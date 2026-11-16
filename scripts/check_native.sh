#!/usr/bin/env bash
# 校验 native DNS 插件包：体积预算、依赖白名单、manifest 兼容性。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

build() {
  local tags="$1" out="$2"
  CGO_ENABLED=0 go build -trimpath -tags "$tags" -ldflags "-s -w" -o "$out" ./cmd/dns-helper
}

size_of() {
  stat -c%s "$1"
}

check_limit() {
  local name="$1" size="$2" limit="$3"
  if [ "$size" -gt "$limit" ]; then
    echo "$name 体积超限：${size} bytes > ${limit} bytes" >&2
    exit 1
  fi
  printf '%s size: %s bytes (%.2f MiB)\n' "$name" "$size" "$(awk -v s="$size" 'BEGIN { print s / 1048576 }')"
}

# heavy-native：完全不依赖 lego / 云 SDK。
HEAVY_TAGS="bundle_dns_heavy_native,nethttpomithttp2"
build "$HEAVY_TAGS" "$TMP/heavy-native"
build "bundle_dns_heavy,nethttpomithttp2" "$TMP/heavy-original"
check_limit "heavy-native" "$(size_of "$TMP/heavy-native")" $((10 * 1024 * 1024))

deps="$(go list -deps -tags "$HEAVY_TAGS" -f '{{with .Module}}{{if not .Main}}{{.Path}}{{end}}{{end}}' ./cmd/dns-helper | sort -u)"
forbidden='github.com/go-acme/lego|github.com/aws/aws-sdk-go|github.com/Azure/|cloud.google.com/go|google.golang.org/|github.com/yandex-cloud|github.com/huaweicloud|github.com/sacloud|github.com/akamai|github.com/iij/|github.com/aliyun|github.com/tencentcloud|github.com/volcengine|github.com/baidubce'
if printf '%s\n' "$deps" | grep -Eq "$forbidden"; then
  echo "heavy-native 仍包含上游 SDK：" >&2
  printf '%s\n' "$deps" | grep -E "$forbidden" >&2
  exit 1
fi

"$TMP/heavy-original" manifest > "$TMP/heavy-original.json"
"$TMP/heavy-native" manifest > "$TMP/heavy-native.json"

# core-native：增量变体，允许未迁移 provider 继续使用 lego，但已迁移 SDK 不得回归。
CORE_TAGS="bundle_dns_core_native,nethttpomithttp2"
build "$CORE_TAGS" "$TMP/core-native"
build "bundle_dns_core,nethttpomithttp2" "$TMP/core-original"
check_limit "core-native" "$(size_of "$TMP/core-native")" $((19 * 1024 * 1024))

migrated='github.com/alibabacloud-go|github.com/go-acme/alidns|github.com/go-acme/esa|github.com/aliyun/credentials-go|github.com/aziontech/azionapi-go-sdk|github.com/dnsimple/dnsimple-go|github.com/exoscale/egoscale|github.com/linode/linodego|github.com/ovh/go-ovh|github.com/scaleway/scaleway-sdk-go|github.com/volcengine/volc-sdk-golang|github.com/vultr/govultr|gopkg.in/ns1/ns1-go'
core_deps="$(go list -deps -tags "$CORE_TAGS" -f '{{with .Module}}{{if not .Main}}{{.Path}}{{end}}{{end}}' ./cmd/dns-helper | sort -u)"
if printf '%s\n' "$core_deps" | grep -Eq "$migrated"; then
  echo "core-native 仍包含已迁移 provider 的 SDK：" >&2
  printf '%s\n' "$core_deps" | grep -E "$migrated" >&2
  exit 1
fi

"$TMP/core-original" manifest > "$TMP/core-original.json"
"$TMP/core-native" manifest > "$TMP/core-native.json"

python3 - "$TMP/heavy-original.json" "$TMP/heavy-native.json" "$TMP/core-original.json" "$TMP/core-native.json" <<'PY'
import json
import sys

def load(path):
    with open(path, encoding="utf-8") as fh:
        item = json.load(fh)
    item.pop("version", None)
    item.pop("name", None)
    return item

pairs = [
    ("heavy", load(sys.argv[1]), load(sys.argv[2])),
    ("core", load(sys.argv[3]), load(sys.argv[4])),
]
for name, original, native in pairs:
    if original != native:
        raise SystemExit(f"{name}-native manifest 与原 manifest 不一致")
    print(f"{name} manifest parity: ok")
PY

printf 'heavy-native dependency closure:\n%s\n' "$deps"
printf 'core-native third-party module count: %s\n' "$(printf '%s\n' "$core_deps" | grep -c .)"
