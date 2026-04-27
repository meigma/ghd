#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 <target> <binary-path>" >&2
}

if [[ "$#" -ne 2 ]]; then
  usage
  exit 2
fi

target="$1"
binary_path="$2"

if [[ "${target}" != darwin_* ]]; then
  exit 0
fi

sign_secret_count=0
for name in MACOS_SIGN_P12 MACOS_SIGN_PASSWORD; do
  if [[ -n "${!name:-}" ]]; then
    sign_secret_count=$((sign_secret_count + 1))
  fi
done

if [[ "${sign_secret_count}" -eq 0 ]]; then
  echo "Skipping Quill signing for ${target}: Apple signing secrets are not configured."
  exit 0
fi

if [[ "${sign_secret_count}" -ne 2 ]]; then
  echo "partial Apple signing secret configuration for ${target}; MACOS_SIGN_P12 and MACOS_SIGN_PASSWORD must both be set" >&2
  exit 1
fi

if ! command -v quill >/dev/null 2>&1; then
  echo "quill is required to sign ${target}, but it is not installed or not on PATH" >&2
  exit 1
fi

if [[ ! -f "${binary_path}" ]]; then
  echo "cannot sign ${target}: binary path does not exist: ${binary_path}" >&2
  exit 1
fi

export QUILL_SIGN_PASSWORD="${MACOS_SIGN_PASSWORD}"

echo "Signing ${target} binary with Quill: ${binary_path}"
quill sign --p12 env:MACOS_SIGN_P12 "${binary_path}"
