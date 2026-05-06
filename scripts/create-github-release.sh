#!/bin/sh
set -eu

tag="${1:-}"

if [ -z "$tag" ]; then
  printf '%s\n' "Missing version tag." >&2
  exit 1
fi

if ! command -v gh >/dev/null 2>&1; then
  printf '%s\n' "Missing GitHub CLI: gh" >&2
  exit 1
fi

notes_file="$(mktemp)"
trap 'rm -f "$notes_file"' EXIT

cog changelog --at "$tag" -t remote > "$notes_file"

if gh release view "$tag" >/dev/null 2>&1; then
  gh release edit "$tag" --title "$tag" --notes-file "$notes_file" --latest
else
  gh release create "$tag" --title "$tag" --notes-file "$notes_file" --latest --verify-tag
fi
