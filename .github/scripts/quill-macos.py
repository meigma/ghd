#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.12"
# ///
"""Sign and notarize macOS release binaries with Quill."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys
import time
from pathlib import Path
from typing import Any


SUBMISSION_ID_RE = re.compile(
    r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}"
)
STATUS_RE = re.compile(r"\b(Accepted|Rejected|Invalid|Pending)\b")
NOTARY_ENV = ("MACOS_NOTARY_KEY", "MACOS_NOTARY_KEY_ID", "MACOS_NOTARY_ISSUER_ID")
SIGN_ENV = ("MACOS_SIGN_P12", "MACOS_SIGN_PASSWORD")


class ReleaseError(RuntimeError):
    """Expected operational failure with a message safe to print."""


def log(message: str) -> None:
    print(message, flush=True)


def fail(message: str) -> None:
    raise ReleaseError(message)


def require_command(name: str) -> None:
    if shutil.which(name) is None:
        fail(f"{name} is required, but it is not installed or not on PATH")


def require_env(names: tuple[str, ...]) -> None:
    missing = [name for name in names if not os.environ.get(name)]
    if missing:
        fail(f"missing required Apple notarization secrets: {' '.join(missing)}")


def quill_notary_args() -> list[str]:
    return [
        "--notary-key",
        "env:MACOS_NOTARY_KEY",
        "--notary-key-id",
        os.environ["MACOS_NOTARY_KEY_ID"],
        "--notary-issuer",
        os.environ["MACOS_NOTARY_ISSUER_ID"],
    ]


def run_stream(command: list[str], *, env: dict[str, str] | None = None) -> None:
    try:
        subprocess.run(command, check=True, env=env)
    except FileNotFoundError:
        fail(f"{command[0]} is required, but it is not installed or not on PATH")
    except subprocess.CalledProcessError as exc:
        fail(f"command failed with exit code {exc.returncode}: {' '.join(command)}")


def run_capture(command: list[str]) -> tuple[int, str]:
    try:
        completed = subprocess.run(
            command,
            check=False,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
        )
    except FileNotFoundError:
        fail(f"{command[0]} is required, but it is not installed or not on PATH")

    output = completed.stdout
    if output:
        print(output, end="" if output.endswith("\n") else "\n")
    return completed.returncode, output


def safe_name(name: str) -> str:
    return re.sub(r"[^A-Za-z0-9_.-]", "_", name)


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as file:
        for chunk in iter(lambda: file.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def write_json(path: Path, data: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")


def parse_submission_id(output: str) -> str | None:
    matches = SUBMISSION_ID_RE.findall(output)
    return matches[-1] if matches else None


def parse_status(output: str) -> str | None:
    matches = STATUS_RE.findall(output)
    return matches[-1] if matches else None


def is_darwin_artifact(artifact: dict[str, Any], prefix: str) -> bool:
    name = str(artifact.get("name", ""))
    path = str(artifact.get("path", ""))
    return (
        artifact.get("type") == "Binary"
        and name.startswith(prefix)
        and (
            "_darwin_" in name
            or artifact.get("goos") == "darwin"
            or "darwin" in path
        )
    )


def artifact_target(artifact: dict[str, Any]) -> str:
    if artifact.get("target"):
        return str(artifact["target"])
    goos = str(artifact.get("goos") or "darwin")
    goarch = str(artifact.get("goarch") or "unknown")
    return f"{goos}_{goarch}"


def load_darwin_artifacts(artifacts_file: Path, prefix: str) -> list[dict[str, str]]:
    if not artifacts_file.is_file():
        fail(f"missing GoReleaser artifacts metadata: {artifacts_file}")

    with artifacts_file.open(encoding="utf-8") as file:
        artifacts = json.load(file)

    if not isinstance(artifacts, list):
        fail(f"expected {artifacts_file} to contain a JSON array")

    selected: list[dict[str, str]] = []
    for artifact in artifacts:
        if not isinstance(artifact, dict) or not is_darwin_artifact(artifact, prefix):
            continue

        name = str(artifact.get("name") or "")
        path = str(artifact.get("path") or "")
        if not name or not path:
            fail(f"Darwin artifact entry is missing name or path: {artifact!r}")

        selected.append(
            {
                "name": name,
                "target": artifact_target(artifact),
                "path": path,
            }
        )

    if not selected:
        fail(f"no Darwin binary artifacts found in {artifacts_file} for prefix {prefix}")
    return selected


def sign_build(args: argparse.Namespace) -> int:
    target = args.target
    binary_path = Path(args.path)

    if not target.startswith("darwin_"):
        return 0

    configured = [name for name in SIGN_ENV if os.environ.get(name)]
    if not configured:
        log(f"Skipping Quill signing for {target}: Apple signing secrets are not configured.")
        return 0

    if len(configured) != len(SIGN_ENV):
        fail(
            f"partial Apple signing secret configuration for {target}; "
            "MACOS_SIGN_P12 and MACOS_SIGN_PASSWORD must both be set"
        )

    require_command("quill")
    if not binary_path.is_file():
        fail(f"cannot sign {target}: binary path does not exist: {binary_path}")

    env = os.environ.copy()
    env["QUILL_SIGN_PASSWORD"] = os.environ["MACOS_SIGN_PASSWORD"]
    log(f"Signing {target} binary with Quill: {binary_path}")
    run_stream(["quill", "sign", "--p12", "env:MACOS_SIGN_P12", str(binary_path)], env=env)
    return 0


def submit_artifact(artifact: dict[str, str], logs_dir: Path) -> dict[str, str]:
    path = Path(artifact["path"])
    if not path.is_file():
        fail(f"Darwin binary artifact is missing: {path}")

    name = artifact["name"]
    submit_log = logs_dir / f"{safe_name(name)}.submit.log"
    command = [
        "quill",
        "notarize",
        *quill_notary_args(),
        "--wait=false",
        "-v",
        str(path),
    ]

    log(f"Submitting {name} for Apple notarization.")
    returncode, output = run_capture(command)
    submit_log.write_text(output, encoding="utf-8")
    if returncode != 0:
        fail(f"failed to submit {name} for Apple notarization")

    submission_id = parse_submission_id(output)
    if submission_id is None:
        fail(f"could not determine Apple notarization submission ID for {name}")

    return {
        "name": name,
        "target": artifact["target"],
        "path": artifact["path"],
        "sha256": sha256_file(path),
        "submission_id": submission_id,
        "status": "Submitted",
    }


def fetch_failure_log(submission_id: str, logs_dir: Path) -> None:
    log_path = logs_dir / f"{submission_id}.json"
    stderr_path = logs_dir / f"{submission_id}.json.stderr"
    command = ["quill", "submission", "logs", submission_id, *quill_notary_args()]

    try:
        completed = subprocess.run(
            command,
            check=False,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
    except FileNotFoundError:
        log(f"Failed to fetch notarization log for {submission_id}: quill is not on PATH")
        return

    log_path.write_text(completed.stdout, encoding="utf-8")
    stderr_path.write_text(completed.stderr, encoding="utf-8")
    if completed.returncode == 0:
        log(f"Fetched notarization log: {log_path}")
    else:
        log(f"Failed to fetch notarization log for {submission_id}; stderr saved to {stderr_path}")


def write_summary(submissions: list[dict[str, str]]) -> None:
    summary_path = os.environ.get("GITHUB_STEP_SUMMARY")
    if not summary_path:
        return

    lines = [
        "## Apple notarization",
        "",
        "| Artifact | SHA-256 | Submission | Status |",
        "| --- | --- | --- | --- |",
    ]
    for item in submissions:
        lines.append(
            f"| `{item['name']}` | `{item['sha256']}` | "
            f"`{item['submission_id']}` | {item['status']} |"
        )
    Path(summary_path).open("a", encoding="utf-8").write("\n".join(lines) + "\n")


def poll_submissions(
    submissions: list[dict[str, str]],
    submissions_file: Path,
    logs_dir: Path,
    timeout_seconds: int,
    poll_seconds: int,
) -> None:
    deadline = time.monotonic() + timeout_seconds

    while True:
        pending = [item for item in submissions if item["status"] != "Accepted"]
        if not pending:
            return

        for item in pending:
            submission_id = item["submission_id"]
            name = item["name"]
            status_log = logs_dir / f"{safe_name(name)}.{submission_id}.status.log"
            command = ["quill", "submission", "status", submission_id, *quill_notary_args()]

            log(f"Checking Apple notarization status for {name}: {submission_id}")
            returncode, output = run_capture(command)
            status_log.write_text(output, encoding="utf-8")
            if returncode != 0:
                fail(f"failed to query Apple notarization status for {submission_id}")

            status = parse_status(output)
            if status is None:
                fail(f"unrecognized Apple notarization status for {submission_id}")

            item["status"] = status
            write_json(submissions_file, submissions)

            if status == "Accepted":
                log(f"Apple notarization accepted for {name}: {submission_id}")
                continue
            if status in {"Rejected", "Invalid"}:
                fetch_failure_log(submission_id, logs_dir)
                fail(f"Apple notarization {status} for {name}: {submission_id}")
            if status != "Pending":
                fail(f"unrecognized Apple notarization status for {submission_id}: {status}")

        if all(item["status"] == "Accepted" for item in submissions):
            return

        remaining = deadline - time.monotonic()
        if remaining <= 0:
            for item in submissions:
                if item["status"] != "Accepted":
                    item["status"] = "Timeout"
            write_json(submissions_file, submissions)
            fail("timed out waiting for Apple notarization")

        time.sleep(min(poll_seconds, remaining))


def notarize_dist(args: argparse.Namespace) -> int:
    require_command("quill")
    require_env(NOTARY_ENV)

    artifacts_file = Path(args.artifacts_file)
    submissions_file = Path(args.submissions_file)
    logs_dir = Path(args.logs_dir)
    logs_dir.mkdir(parents=True, exist_ok=True)

    submissions: list[dict[str, str]] = []
    write_json(submissions_file, submissions)

    try:
        for artifact in load_darwin_artifacts(artifacts_file, args.artifact_name_prefix):
            submissions.append(submit_artifact(artifact, logs_dir))
            write_json(submissions_file, submissions)

        poll_submissions(
            submissions,
            submissions_file,
            logs_dir,
            args.timeout_seconds,
            args.poll_seconds,
        )
        return 0
    finally:
        write_summary(submissions)


def positive_int(value: str) -> int:
    parsed = int(value)
    if parsed <= 0:
        raise argparse.ArgumentTypeError("must be greater than 0")
    return parsed


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)

    sign = subparsers.add_parser("sign-build", help="sign a GoReleaser build artifact")
    sign.add_argument("--target", required=True)
    sign.add_argument("--path", required=True)
    sign.set_defaults(func=sign_build)

    notarize = subparsers.add_parser("notarize-dist", help="notarize Darwin artifacts from dist")
    notarize.add_argument("--artifacts-file", default=os.environ.get("ARTIFACTS_FILE", "dist/artifacts.json"))
    notarize.add_argument(
        "--submissions-file",
        default=os.environ.get("NOTARY_SUBMISSIONS_FILE", "dist/notary-submissions.json"),
    )
    notarize.add_argument("--logs-dir", default=os.environ.get("NOTARY_LOGS_DIR", "dist/notary-logs"))
    notarize.add_argument(
        "--artifact-name-prefix",
        default=os.environ.get("ARTIFACT_NAME_PREFIX", "ghd_"),
    )
    notarize.add_argument(
        "--timeout-seconds",
        type=positive_int,
        default=positive_int(os.environ.get("NOTARY_TIMEOUT_SECONDS", "3600")),
    )
    notarize.add_argument(
        "--poll-seconds",
        type=positive_int,
        default=positive_int(os.environ.get("NOTARY_POLL_SECONDS", "60")),
    )
    notarize.set_defaults(func=notarize_dist)
    return parser


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()
    try:
        return int(args.func(args))
    except ReleaseError as exc:
        print(str(exc), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
