#!/usr/bin/env bash
set -euo pipefail

# Sync VERSION into the desktop Wails project metadata.
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

update_wails_yaml() {
  local file="wails.yaml"
  local version="$1"
  if [ ! -f "${file}" ]; then
    echo "missing ${file}" >&2
    exit 1
  fi
  python3 - "${file}" "${version}" <<'PY'
import pathlib, sys
path, version = sys.argv[1:]
file = pathlib.Path(path)
lines = file.read_text().splitlines()
out = []
seen_version = False
for line in lines:
    if line.startswith("  version:") and not seen_version:
        out.append(f'  version: "{version}"')
        seen_version = True
        continue
    if "appcore.Version=" in line:
        out.append(f"    - github.com/jdbnet/vantage/internal/appcore.Version={version}")
        continue
    out.append(line)
if not seen_version:
    raise SystemExit("project.version not found in wails.yaml")
file.write_text("\n".join(out) + "\n")
PY
}

version="$(resolve_version)"
update_wails "${version}"
update_wails_yaml "${version}"
echo "==> Version ${version} synced to cmd/vantage/wails.json and wails.yaml"
