gh_source_repo := "https://github.com/cli/cli.git"
gh_source_branch := "trunk"

# Show available recipes.
default:
    @just --list

# Clone or update the upstream GitHub CLI source reference.
prime-gh-source target=".upstream/gh":
    #!/usr/bin/env bash
    set -euo pipefail

    repo="{{gh_source_repo}}"
    branch="{{gh_source_branch}}"
    target="{{target}}"

    if [ -d "$target/.git" ]; then
        git -C "$target" remote set-url origin "$repo"
        git -C "$target" fetch --depth=1 --prune origin "$branch"

        if git -C "$target" show-ref --verify --quiet "refs/heads/$branch"; then
            git -C "$target" checkout "$branch"
        else
            git -C "$target" checkout --track "origin/$branch"
        fi

        git -C "$target" pull --ff-only --depth=1 origin "$branch"
    elif [ -e "$target" ]; then
        printf 'error: %s exists but is not a git checkout\n' "$target" >&2
        exit 1
    else
        mkdir -p "$(dirname "$target")"
        git clone --depth=1 --branch "$branch" "$repo" "$target"
    fi
