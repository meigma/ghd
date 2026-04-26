#!/usr/bin/env bash

set -euo pipefail

repo="meigma/ghd"
package="ghd-example"
version="1.1.1"
tag="example-v1.1.1"
target="${repo}/${package}"
versioned_target="${target}@${version}"
signer_workflow="meigma/ghd/.github/workflows/release.yml"

if [[ "${GITHUB_ACTIONS:-}" != "true" && "${GHD_FUNCTIONAL_SMOKE_ALLOW_LOCAL:-}" != "1" ]]; then
  echo "functional smoke test is destructive to default ghd paths; run in GitHub Actions or set GHD_FUNCTIONAL_SMOKE_ALLOW_LOCAL=1 with an isolated HOME" >&2
  exit 1
fi

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "required command not found: $1" >&2
    exit 1
  fi
}

assert_file() {
  if [[ ! -f "$1" ]]; then
    echo "expected file to exist: $1" >&2
    exit 1
  fi
}

assert_no_file() {
  if [[ -e "$1" ]]; then
    echo "expected path to be absent: $1" >&2
    exit 1
  fi
}

assert_output_contains() {
  local file="$1"
  local expected="$2"
  if ! grep -Fq "$expected" "$file"; then
    echo "expected $file to contain: $expected" >&2
    echo "--- $file ---" >&2
    cat "$file" >&2
    exit 1
  fi
}

assert_json() {
  local file="$1"
  shift
  jq -e "$@" "$file" >/dev/null
}

run_expect_failure() {
  local name="$1"
  shift

  set +e
  "$@" >"${log_dir}/${name}.out" 2>"${log_dir}/${name}.err"
  local status=$?
  set -e

  if [[ "$status" -eq 0 ]]; then
    echo "expected command to fail: $*" >&2
    echo "--- stdout ---" >&2
    cat "${log_dir}/${name}.out" >&2
    echo "--- stderr ---" >&2
    cat "${log_dir}/${name}.err" >&2
    exit 1
  fi
}

require_command gh
require_command go
require_command jq
require_command grep

candidate_dir="${HOME}/.local/share/ghd-functional-smoke/bin"
log_dir="${HOME}/.local/share/ghd-functional-smoke/logs"
download_dir="${HOME}/Downloads/ghd-functional-smoke"
attestation_dir="${HOME}/.local/share/ghd-functional-smoke/attestation"
managed_share="${HOME}/.local/share/ghd"
managed_state="${HOME}/.local/state/ghd"
managed_bin="${HOME}/.local/bin"
managed_binary="${managed_bin}/${package}"

rm -rf "${managed_share}" "${managed_state}" "${download_dir}" "${candidate_dir}" "${log_dir}" "${attestation_dir}"
rm -f "${managed_binary}"
mkdir -p "${candidate_dir}" "${log_dir}" "${download_dir}" "${attestation_dir}" "${managed_bin}"

GOBIN="${candidate_dir}" go install ./cmd/ghd
export PATH="${candidate_dir}:${managed_bin}:${PATH}"

if [[ "$(command -v ghd)" != "${candidate_dir}/ghd" ]]; then
  echo "candidate ghd is not first on PATH" >&2
  command -v ghd >&2 || true
  exit 1
fi

ghd --help >"${log_dir}/ghd-help.out"
for command in download install list info repo installed check verify update uninstall doctor; do
  assert_output_contains "${log_dir}/ghd-help.out" "$command"
done

host_os="$(go env GOHOSTOS)"
host_arch="$(go env GOHOSTARCH)"
asset="${package}_${version}_${host_os}_${host_arch}"

gh release view "${tag}" \
  -R "${repo}" \
  --json tagName,isDraft,isImmutable,assets,publishedAt,targetCommitish,url \
  >"${log_dir}/release-view.json"
assert_json "${log_dir}/release-view.json" \
  --arg tag "${tag}" --arg asset "${asset}" \
  '.tagName == $tag and .isDraft == false and .isImmutable == true and any(.assets[]; .name == $asset)'

gh release verify "${tag}" -R "${repo}" >"${log_dir}/release-verify.out"
gh release download "${tag}" -R "${repo}" -p "${asset}" -D "${attestation_dir}" --clobber
gh attestation verify "${attestation_dir}/${asset}" \
  --repo "${repo}" \
  --signer-workflow "${signer_workflow}" \
  --source-ref "refs/tags/${tag}" \
  --deny-self-hosted-runners \
  >"${log_dir}/attestation-verify.out"

ghd --non-interactive repo add "${repo}" \
  >"${log_dir}/repo-add.out" \
  2>"${log_dir}/repo-add.err"
ghd --non-interactive repo list --json \
  >"${log_dir}/repo-list.json" \
  2>"${log_dir}/repo-list.err"
assert_json "${log_dir}/repo-list.json" \
  --arg repo "${repo}" --arg package "${package}" \
  'any(.repositories[]; .repository == $repo and any(.packages[]; .name == $package and (.binaries | index($package))))'

ghd --non-interactive list --json \
  >"${log_dir}/list-index.json" \
  2>"${log_dir}/list-index.err"
assert_json "${log_dir}/list-index.json" \
  --arg repo "${repo}" --arg package "${package}" \
  'any(.packages[]; .repository == $repo and .package == $package and .target == ($repo + "/" + $package))'

ghd --non-interactive list "${repo}" --json \
  >"${log_dir}/list-live.json" \
  2>"${log_dir}/list-live.err"
assert_json "${log_dir}/list-live.json" \
  --arg repo "${repo}" --arg package "${package}" \
  'any(.packages[]; .repository == $repo and .package == $package and .target == ($repo + "/" + $package))'

ghd --non-interactive info "${package}" --json \
  >"${log_dir}/info-index.json" \
  2>"${log_dir}/info-index.err"
assert_json "${log_dir}/info-index.json" \
  --arg repo "${repo}" --arg package "${package}" --arg signer "$signer_workflow" \
  '.package.repository == $repo and .package.package == $package and .package.signer_workflow == $signer'

ghd --non-interactive info "${target}" --json \
  >"${log_dir}/info-qualified.json" \
  2>"${log_dir}/info-qualified.err"
assert_json "${log_dir}/info-qualified.json" \
  --arg repo "${repo}" --arg package "${package}" --arg host_os "${host_os}" --arg host_arch "${host_arch}" --arg pattern "${package}"'_${version}_'"${host_os}_${host_arch}" \
  '.package.repository == $repo and .package.package == $package and any(.package.assets[]; .os == $host_os and .arch == $host_arch and .pattern == $pattern)'

run_expect_failure info-version-target ghd --non-interactive info "${versioned_target}"
assert_output_contains "${log_dir}/info-version-target.err" "info target must be name, owner/repo, or owner/repo/package"
assert_no_file "${managed_state}/installed.json"
assert_no_file "${managed_binary}"

mkdir -p "${download_dir}/direct"
ghd --non-interactive download "${versioned_target}" \
  --output "${download_dir}/direct" \
  >"${log_dir}/download.out" \
  2>"${log_dir}/download.err"
assert_output_contains "${log_dir}/download.out" "artifact ${download_dir}/direct/${asset}"
assert_output_contains "${log_dir}/download.out" "verification ${download_dir}/direct/verification.json"
assert_file "${download_dir}/direct/${asset}"
assert_file "${download_dir}/direct/verification.json"
assert_json "${download_dir}/direct/verification.json" \
  --arg repo "${repo}" --arg package "${package}" --arg version "${version}" --arg tag "${tag}" --arg asset "${asset}" --arg signer "${signer_workflow}" \
  '.schema_version == 1
    and .repository == $repo
    and .package == $package
    and .version == $version
    and .tag == $tag
    and .asset == $asset
    and .evidence.AssetDigest.Algorithm == "sha256"
    and (.evidence.AssetDigest.Hex | test("^[0-9a-f]{64}$"))
    and (.evidence.ReleaseAttestation.PredicateType == "https://in-toto.io/attestation/release/v0.1" or .evidence.ReleaseAttestation.PredicateType == "https://in-toto.io/attestation/release/v0.2")
    and .evidence.ProvenanceAttestation.PredicateType == "https://slsa.dev/provenance/v1"
    and (.evidence.ProvenanceAttestation.SignerWorkflow | startswith($signer + "@refs/tags/"))'
assert_no_file "${managed_state}/installed.json"
assert_no_file "${managed_binary}"

run_expect_failure install-no-yes ghd --non-interactive install "${versioned_target}"
assert_output_contains "${log_dir}/install-no-yes.err" "install requires approval after verification; rerun with --yes"
assert_no_file "${managed_state}/installed.json"
assert_no_file "${managed_binary}"

ghd --yes --non-interactive install "${package}@${version}" \
  >"${log_dir}/install.out" \
  2>"${log_dir}/install.err"
assert_output_contains "${log_dir}/install.out" "binary ${managed_binary}"
assert_output_contains "${log_dir}/install.err" "installed ${target}@${version}"

if [[ ! -L "${managed_binary}" ]]; then
  echo "expected managed binary to be a symlink: ${managed_binary}" >&2
  exit 1
fi
if [[ "$(readlink "${managed_binary}")" != "${managed_share}/store/github.com/${repo}/${package}/${version}/"* ]]; then
  echo "managed binary does not point into the expected store path" >&2
  readlink "${managed_binary}" >&2
  exit 1
fi

"${managed_binary}" version >"${log_dir}/example-version.out"
"${managed_binary}" >"${log_dir}/example-hello.out"
assert_output_contains "${log_dir}/example-version.out" "${package} ${version}"
assert_output_contains "${log_dir}/example-hello.out" "hello from ghd-example"

ghd --non-interactive installed --json \
  >"${log_dir}/installed-after-install.json" \
  2>"${log_dir}/installed-after-install.err"
assert_json "${log_dir}/installed-after-install.json" \
  --arg repo "${repo}" --arg package "${package}" --arg version "${version}" --arg tag "${tag}" --arg asset "${asset}" \
  '(.installed | length) == 1
    and .installed[0].repository == $repo
    and .installed[0].package == $package
    and .installed[0].version == $version
    and .installed[0].tag == $tag
    and .installed[0].asset == $asset
    and (.installed[0].asset_digest | test("^sha256:[0-9a-f]{64}$"))
    and (.installed[0].binaries | length) == 1
    and .installed[0].binaries[0].name == $package
    and .installed[0].binaries[0].link_path == "'"${managed_binary}"'"'

ghd --non-interactive check "${package}" --json \
  >"${log_dir}/check-target.json" \
  2>"${log_dir}/check-target.err"
assert_json "${log_dir}/check-target.json" \
  --arg repo "${repo}" --arg package "${package}" --arg version "${version}" \
  '(.checks | length) == 1
    and .checks[0].repository == $repo
    and .checks[0].package == $package
    and .checks[0].version == $version
    and .checks[0].status == "up-to-date"'

ghd --non-interactive verify "${package}" --json \
  >"${log_dir}/verify-target.json" \
  2>"${log_dir}/verify-target.err"
assert_json "${log_dir}/verify-target.json" \
  --arg repo "${repo}" --arg package "${package}" --arg version "${version}" \
  '(.verifications | length) == 1
    and .verifications[0].repository == $repo
    and .verifications[0].package == $package
    and .verifications[0].version == $version
    and .verifications[0].status == "verified"'

ghd --non-interactive verify --all \
  >"${log_dir}/verify-all.out" \
  2>"${log_dir}/verify-all.err"
assert_output_contains "${log_dir}/verify-all.out" "${target} ${version} verified"

ghd --non-interactive doctor --json \
  >"${log_dir}/doctor.json" \
  2>"${log_dir}/doctor.err"
assert_json "${log_dir}/doctor.json" \
  '(["bin-dir-on-path","index-dir","store-dir","state-dir","bin-dir","trusted-root","github-api"] - [.checks[].id]) == []'
assert_json "${log_dir}/doctor.json" \
  'all(.checks[]; .status != "fail")'

run_expect_failure verify-no-target ghd --non-interactive verify
assert_output_contains "${log_dir}/verify-no-target.err" "verify target must be set"
run_expect_failure update-no-target ghd --yes --non-interactive update
assert_output_contains "${log_dir}/update-no-target.err" "update target must be set"

ghd --yes --non-interactive update "${package}" \
  >"${log_dir}/update-current.out" \
  2>"${log_dir}/update-current.err"
assert_output_contains "${log_dir}/update-current.out" "${target} ${version} ${version} already-up-to-date"

ghd --non-interactive repo refresh "${repo}" \
  >"${log_dir}/repo-refresh.out" \
  2>"${log_dir}/repo-refresh.err"
ghd --non-interactive repo refresh --all \
  >"${log_dir}/repo-refresh-all.out" \
  2>"${log_dir}/repo-refresh-all.err"
ghd --non-interactive repo remove "${repo}" \
  >"${log_dir}/repo-remove.out" \
  2>"${log_dir}/repo-remove.err"
ghd --non-interactive repo list --json \
  >"${log_dir}/repo-list-after-remove.json" \
  2>"${log_dir}/repo-list-after-remove.err"
assert_json "${log_dir}/repo-list-after-remove.json" \
  '(.repositories | length) == 0'
ghd --non-interactive installed --json \
  >"${log_dir}/installed-after-repo-remove.json" \
  2>"${log_dir}/installed-after-repo-remove.err"
assert_json "${log_dir}/installed-after-repo-remove.json" \
  --arg repo "${repo}" --arg package "${package}" \
  '(.installed | length) == 1 and .installed[0].repository == $repo and .installed[0].package == $package'

ghd --non-interactive uninstall "${package}" \
  >"${log_dir}/uninstall.out" \
  2>"${log_dir}/uninstall.err"
assert_output_contains "${log_dir}/uninstall.err" "uninstalled ${target}@${version}"
assert_no_file "${managed_binary}"
ghd --non-interactive installed --json \
  >"${log_dir}/installed-after-uninstall.json" \
  2>"${log_dir}/installed-after-uninstall.err"
assert_json "${log_dir}/installed-after-uninstall.json" \
  '(.installed | length) == 0'

rm -rf "${managed_share}" "${managed_state}" "${download_dir}"
rm -f "${managed_binary}"

echo "functional smoke test passed for ${versioned_target}"
