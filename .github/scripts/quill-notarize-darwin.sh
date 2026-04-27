#!/usr/bin/env bash
set -euo pipefail

artifacts_file="${ARTIFACTS_FILE:-dist/artifacts.json}"
submissions_file="${NOTARY_SUBMISSIONS_FILE:-dist/notary-submissions.json}"
logs_dir="${NOTARY_LOGS_DIR:-dist/notary-logs}"
artifact_name_prefix="${ARTIFACT_NAME_PREFIX:-ghd_}"
timeout_seconds="${NOTARY_TIMEOUT_SECONDS:-3600}"
poll_seconds="${NOTARY_POLL_SECONDS:-60}"

require_command() {
  local command_name="$1"
  if ! command -v "${command_name}" >/dev/null 2>&1; then
    echo "${command_name} is required for notarization" >&2
    exit 1
  fi
}

require_env() {
  local missing=()
  for name in MACOS_NOTARY_KEY MACOS_NOTARY_KEY_ID MACOS_NOTARY_ISSUER_ID; do
    if [[ -z "${!name:-}" ]]; then
      missing+=("${name}")
    fi
  done

  if [[ "${#missing[@]}" -gt 0 ]]; then
    printf 'missing required Apple notarization secrets: %s\n' "${missing[*]}" >&2
    exit 1
  fi
}

write_json() {
  local tmp
  tmp="$(mktemp)"
  jq "$@" "${submissions_file}" >"${tmp}"
  mv "${tmp}" "${submissions_file}"
}

add_submission() {
  local name="$1"
  local target="$2"
  local path="$3"
  local sha256="$4"
  local submission_id="$5"

  write_json \
    --arg name "${name}" \
    --arg target "${target}" \
    --arg path "${path}" \
    --arg sha256 "${sha256}" \
    --arg submission_id "${submission_id}" \
    '. + [{
      name: $name,
      target: $target,
      path: $path,
      sha256: $sha256,
      submission_id: $submission_id,
      status: "Submitted"
    }]'
}

update_submission_status() {
  local submission_id="$1"
  local status="$2"

  write_json \
    --arg submission_id "${submission_id}" \
    --arg status "${status}" \
    'map(if .submission_id == $submission_id then .status = $status else . end)'
}

fetch_failure_log() {
  local submission_id="$1"
  local log_path="${logs_dir}/${submission_id}.json"

  if quill submission logs "${submission_id}" "${notary_args[@]}" >"${log_path}" 2>"${log_path}.stderr"; then
    echo "Fetched notarization log: ${log_path}"
  else
    echo "Failed to fetch notarization log for ${submission_id}; stderr saved to ${log_path}.stderr" >&2
  fi
}

extract_submission_id() {
  { grep -Eo '[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}' || true; } | tail -n 1
}

extract_status() {
  { grep -Eo 'Accepted|Rejected|Invalid|Pending' || true; } | tail -n 1
}

write_summary() {
  if [[ -z "${GITHUB_STEP_SUMMARY:-}" || ! -f "${submissions_file}" ]]; then
    return
  fi

  {
    echo "## Apple notarization"
    echo
    echo "| Artifact | SHA-256 | Submission | Status |"
    echo "| --- | --- | --- | --- |"
    jq -r '
      .[]
      | "| `\(.name)` | `\(.sha256)` | `\(.submission_id)` | \(.status) |"
    ' "${submissions_file}"
  } >>"${GITHUB_STEP_SUMMARY}"
}

require_command jq
require_command quill
require_command shasum
require_env
notary_args=(
  --notary-key
  env:MACOS_NOTARY_KEY
  --notary-key-id
  "${MACOS_NOTARY_KEY_ID}"
  --notary-issuer
  "${MACOS_NOTARY_ISSUER_ID}"
)

if [[ ! -f "${artifacts_file}" ]]; then
  echo "missing GoReleaser artifacts metadata: ${artifacts_file}" >&2
  exit 1
fi

mkdir -p "$(dirname "${submissions_file}")" "${logs_dir}"
printf '[]\n' >"${submissions_file}"
trap write_summary EXIT

mapfile -t darwin_artifacts < <(
  jq -r --arg artifact_name_prefix "${artifact_name_prefix}" '
    .[]
    | select(.type == "Binary")
    | select(.name | startswith($artifact_name_prefix))
    | select(
        (.name | test("_darwin_")) or
        (.goos? == "darwin") or
        (.path | test("darwin"))
      )
    | [.name, (.target // ((.goos // "darwin") + "_" + (.goarch // "unknown"))), .path]
    | @tsv
  ' "${artifacts_file}"
)

if [[ "${#darwin_artifacts[@]}" -eq 0 ]]; then
  echo "no Darwin binary artifacts found in ${artifacts_file} for prefix ${artifact_name_prefix}" >&2
  exit 1
fi

for artifact in "${darwin_artifacts[@]}"; do
  IFS=$'\t' read -r name target path <<<"${artifact}"

  if [[ ! -f "${path}" ]]; then
    echo "Darwin binary artifact is missing: ${path}" >&2
    exit 1
  fi

  sha256="$(shasum -a 256 "${path}" | awk '{print $1}')"
  safe_name="${name//[^A-Za-z0-9_.-]/_}"
  submit_log="${logs_dir}/${safe_name}.submit.log"

  echo "Submitting ${name} for Apple notarization."
  if ! quill notarize "${notary_args[@]}" --wait=false -v "${path}" 2>&1 | tee "${submit_log}"; then
    echo "failed to submit ${name} for Apple notarization" >&2
    exit 1
  fi

  submission_id="$(extract_submission_id <"${submit_log}")"
  if [[ -z "${submission_id}" ]]; then
    echo "could not determine Apple notarization submission ID for ${name}" >&2
    cat "${submit_log}" >&2
    exit 1
  fi

  add_submission "${name}" "${target}" "${path}" "${sha256}" "${submission_id}"
done

deadline=$((SECONDS + timeout_seconds))

while true; do
  pending_count="$(jq '[.[] | select(.status != "Accepted")] | length' "${submissions_file}")"
  if [[ "${pending_count}" -eq 0 ]]; then
    break
  fi

  while IFS=$'\t' read -r submission_id name; do
    safe_name="${name//[^A-Za-z0-9_.-]/_}"
    status_log="${logs_dir}/${safe_name}.${submission_id}.status.log"

    echo "Checking Apple notarization status for ${name}: ${submission_id}"
    if ! quill submission status "${submission_id}" "${notary_args[@]}" 2>&1 | tee "${status_log}"; then
      echo "failed to query Apple notarization status for ${submission_id}" >&2
      exit 1
    fi

    status="$(extract_status <"${status_log}")"
    case "${status}" in
      Accepted)
        update_submission_status "${submission_id}" "${status}"
        echo "Apple notarization accepted for ${name}: ${submission_id}"
        ;;
      Rejected|Invalid)
        update_submission_status "${submission_id}" "${status}"
        fetch_failure_log "${submission_id}"
        echo "Apple notarization ${status} for ${name}: ${submission_id}" >&2
        exit 1
        ;;
      Pending)
        update_submission_status "${submission_id}" "${status}"
        ;;
      *)
        echo "unrecognized Apple notarization status for ${submission_id}" >&2
        cat "${status_log}" >&2
        exit 1
        ;;
    esac
  done < <(jq -r '.[] | select(.status != "Accepted") | [.submission_id, .name] | @tsv' "${submissions_file}")

  pending_count="$(jq '[.[] | select(.status != "Accepted")] | length' "${submissions_file}")"
  if [[ "${pending_count}" -eq 0 ]]; then
    break
  fi

  if [[ "${SECONDS}" -ge "${deadline}" ]]; then
    write_json 'map(if .status != "Accepted" then .status = "Timeout" else . end)'
    echo "timed out waiting for Apple notarization" >&2
    exit 1
  fi

  sleep_for="${poll_seconds}"
  if [[ $((SECONDS + sleep_for)) -gt "${deadline}" ]]; then
    sleep_for=$((deadline - SECONDS))
  fi
  if [[ "${sleep_for}" -gt 0 ]]; then
    sleep "${sleep_for}"
  fi
done
