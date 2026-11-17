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

# all-native：覆盖全部 194 个 provider，已迁移 provider 使用仓库实现。
NATIVE_TAGS="bundle_dns_all_native,nethttpomithttp2,http2legacy"
build "$NATIVE_TAGS" "$TMP/all-native"
build "nethttpomithttp2,http2legacy" "$TMP/all-original"
check_limit "all-native" "$(size_of "$TMP/all-native")" $((20 * 1024 * 1024))

deps="$(go list -deps -tags "$NATIVE_TAGS" -f '{{with .Module}}{{if not .Main}}{{.Path}}{{end}}{{end}}' ./cmd/dns-helper | sort -u)"

# 已重构的 provider 不得再链接对应的 lego 包装，否则 native 实现没有被使用。
for dir in dns/native/providers/*; do
  [ -d "$dir" ] || continue
  provider="$(basename "$dir")"
  if printf '%s\n' "$deps" | grep -qx "github.com/go-acme/lego/v4/providers/dns/${provider}"; then
    echo "all-native 仍链接 lego 的 ${provider} 包装" >&2
    exit 1
  fi
done

# 已迁移 provider 的上游 SDK 不得回归（未迁移的常用服务商仍允许 lego）。
forbidden='github.com/alibabacloud-go|github.com/go-acme/alidns|github.com/go-acme/esa|github.com/aliyun/credentials-go|github.com/aziontech/azionapi-go-sdk|github.com/dnsimple/dnsimple-go|github.com/exoscale/egoscale|github.com/linode/linodego|github.com/ovh/go-ovh|github.com/scaleway/scaleway-sdk-go|github.com/volcengine/volc-sdk-golang|github.com/vultr/govultr|gopkg.in/ns1/ns1-go|github.com/yandex-cloud|github.com/sacloud|github.com/akamai|github.com/iij/|github.com/huaweicloud|cloud.google.com/go|google.golang.org/api'
if printf '%s\n' "$deps" | grep -Eq "$forbidden"; then
  echo "all-native 仍包含已迁移 provider 的上游 SDK：" >&2
  printf '%s\n' "$deps" | grep -E "$forbidden" >&2
  exit 1
fi

"$TMP/all-original" manifest > "$TMP/all-original.json"
"$TMP/all-native" manifest > "$TMP/all-native.json"

python3 - "$TMP/all-original.json" "$TMP/all-native.json" <<'PY'
import json
import sys

def load(path):
    with open(path, encoding="utf-8") as fh:
        item = json.load(fh)
    item.pop("version", None)
    item.pop("name", None)
    return item

original = load(sys.argv[1])
native = load(sys.argv[2])
if original != native:
    raise SystemExit("all-native manifest 与原 manifest 不一致")
print("all manifest parity: ok")
PY

printf 'all-native third-party module count: %s\n' "$(printf '%s\n' "$deps" | grep -c .)"
