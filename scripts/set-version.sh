#!/usr/bin/env bash
set -euo pipefail

# Sync VERSION into cmd/vantage/wails.json (Windows exe metadata and NSIS installers).
cd "$(dirname "$0")/.."

resolve_version() {
  if [ -n "${VERSION:-}" ]; then
    printf '%s' "${VERSION}"
    return
  fi

  if [ -f VERSION ]; then
    tr -d '[:space:]' < VERSION
    return
  fi

  if git describe --tags --abbrev=0 >/dev/null 2>&1; then
    git describe --tags --abbrev=0 | sed 's/^v//'
    return
  fi

  echo "Set VERSION, add a VERSION file, or create a git tag" >&2
  exit 1
}

update_wails() {
  local file="cmd/vantage/wails.json"
  local version="$1"
  if [ ! -f "${file}" ]; then
    echo "missing ${file}" >&2
    exit 1
  fi

  if command -v jq >/dev/null 2>&1; then
    local tmp
    tmp="$(mktemp)"
    jq --arg v "${version}" '.info.productVersion = $v' "${file}" > "${tmp}"
    mv "${tmp}" "${file}"
    return
  fi

  if command -v node >/dev/null 2>&1; then
    node -e '
      const fs = require("fs");
      const p = process.argv[1];
      const v = process.argv[2];
      const j = JSON.parse(fs.readFileSync(p, "utf8"));
      if (!j.info) j.info = {};
      j.info.productVersion = v;
      fs.writeFileSync(p, JSON.stringify(j, null, 2) + "\n");
    ' "${file}" "${version}"
    return
  fi

  echo "jq or node is required to update wails.json" >&2
  exit 1
}

version="$(resolve_version)"
update_wails "${version}"
echo "==> Version ${version} synced to cmd/vantage/wails.json"
