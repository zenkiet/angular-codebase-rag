#!/bin/sh
set -eu

version="${1:-}"

if [ -z "$version" ]; then
  printf '%s\n' "Missing version argument." >&2
  exit 1
fi

version="${version#v}"

if ! printf '%s\n' "$version" | grep -E -q '^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?(\+[0-9A-Za-z][0-9A-Za-z.-]*)?$'; then
  printf 'Invalid SemVer version: %s\n' "$version" >&2
  exit 1
fi

tmp_file="VERSION.tmp"
printf '%s\n' "$version" > "$tmp_file"
mv "$tmp_file" VERSION

printf 'Updated VERSION to %s\n' "$version"
