#!/bin/bash
# relay-store.sh — resolve the shared cross-session relay store directory.
#
# Like the lease store (go/internal/hookhandler/session_lease.go:leaseStore),
# the relay store lives under `git --git-common-dir`'s parent so every worktree
# of the same repo shares ONE relay-signals.jsonl. Without this, each worktree
# would read/write its own file and cross-worktree relay would silently fail.
# Falls back to a project-local path when the directory is not a git repo.
#
# Usage (sourced): relay_sessions_dir <project_path>  → prints <root>/.claude/sessions

relay_sessions_dir() {
  local project="${1:-$PWD}"
  local common_dir repo_root
  common_dir="$(git -C "$project" rev-parse --git-common-dir 2>/dev/null || true)"
  if [ -z "$common_dir" ]; then
    # Not a git repo (or git unavailable): fall back to project-local.
    printf '%s/.claude/sessions' "$project"
    return 0
  fi
  # `git rev-parse --git-common-dir` may return a path relative to <project>;
  # make it absolute so the store is stable across worktrees that resolve to the
  # same physical .git directory.
  case "$common_dir" in
    /*) ;;
    *) common_dir="${project}/${common_dir}" ;;
  esac
  # The .git directory's parent is the (main) repo root, shared by all worktrees.
  repo_root="$(cd "$(dirname "$common_dir")" 2>/dev/null && pwd || true)"
  if [ -z "$repo_root" ]; then
    printf '%s/.claude/sessions' "$project"
    return 0
  fi
  printf '%s/.claude/sessions' "$repo_root"
}
