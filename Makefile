APP := lactssl
GO := go
BUILD_ENV := CGO_ENABLED=0
LDFLAGS := -s -w
BIN := bin

.PHONY: all build bundle bundle-native gen manifest plugins test fmt vet check check-native clean index

all: build

build: bundle

# 合并插件包：DNS 只发布 all-native 一个包，部署与通知各一个包，共享各自进程内的 Go 运行时。
bundle:
	mkdir -p $(BIN)
	$(BUILD_ENV) $(GO) build -trimpath -tags "bundle_dns_all_native,nethttpomithttp2" -ldflags "$(LDFLAGS)" -o $(BIN)/$(APP)-dns-all-native ./cmd/dns-helper
	$(BUILD_ENV) $(GO) build -trimpath -tags nethttpomithttp2 -ldflags "$(LDFLAGS)" -o $(BIN)/$(APP)-dns-all ./cmd/dns-helper
	$(BUILD_ENV) $(GO) build -trimpath -tags nethttpomithttp2 -ldflags "$(LDFLAGS)" -o $(BIN)/$(APP)-deploy-all ./cmd/deploy-helper
	$(BUILD_ENV) $(GO) build -trimpath -tags nethttpomithttp2 -ldflags "$(LDFLAGS)" -o $(BIN)/$(APP)-notify-all ./cmd/notify-helper

# all-native 单独构建（用于体积/依赖检查）。
bundle-native:
	mkdir -p $(BIN)
	$(BUILD_ENV) $(GO) build -trimpath -tags "bundle_dns_all_native,nethttpomithttp2" -ldflags "$(LDFLAGS)" -o $(BIN)/$(APP)-dns-all-native ./cmd/dns-helper

# 生成 plugins.json；bundle 模式不再生成单服务商 selector。
gen:
	$(GO) run scripts/pluginctl/main.go manifest
	$(GO) run scripts/pluginctl/main.go gen

manifest:
	$(GO) run scripts/pluginctl/main.go manifest

# 构建单个合并包：make plugin KIND=dns ID=all-native GOOS=linux GOARCH=amd64
# DNS 的 ID 为 all-native；部署和通知的 ID 为 all。
ID ?= all-native
plugin:
	$(GO) run scripts/pluginctl/main.go build -kind "$(KIND)" -id "$(ID)" -goos "$(GOOS)" -goarch "$(GOARCH)" -out dist

# 从 dist/*.meta.json 生成索引。
BUCKET ?=
index:
	$(GO) run scripts/pluginctl/main.go index -assets dist -base-url "$(BASE_URL)" $(if $(BUCKET),-bucket "$(BUCKET)") -out index.json

test:
	$(GO) test ./...

fmt:
	gofmt -w .

vet:
	$(GO) vet ./...

check:
	test -z "$$(gofmt -l .)" || (echo "gofmt required" >&2; exit 1)
	$(GO) vet ./...
	$(GO) test ./...
	./scripts/check_native.sh

check-native:
	./scripts/check_native.sh

clean:
	rm -rf $(BIN) dist out index.json index.json.sig
