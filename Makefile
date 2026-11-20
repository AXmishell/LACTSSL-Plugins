APP := lactssl
GO := go
BUILD_ENV := CGO_ENABLED=0
LDFLAGS := -s -w
BIN := bin

.PHONY: all build bundle bundle-lego bundle-lego-v5 bundle-native gen gen-v5 manifest plugins test fmt vet check check-native clean index

all: build

build: bundle

# 合并插件包：DNS 发布 all-lego、all-lego-v5 与 all-native 三个变体，部署与通知各一个包，
# 共享各自进程内的 Go 运行时。
bundle:
	mkdir -p $(BIN)
	$(BUILD_ENV) $(GO) build -trimpath -tags "bundle_dns_all_lego,nethttpomithttp2,http2legacy" -ldflags "$(LDFLAGS)" -o $(BIN)/$(APP)-dns-all-lego ./cmd/dns-helper
	$(BUILD_ENV) $(GO) build -trimpath -tags "nethttpomithttp2,http2legacy,urfave_cli_no_template,ozzo_validation_no_template,grpcnotrace,validator_novalidatefn" -ldflags "$(LDFLAGS)" -o $(BIN)/$(APP)-dns-all-lego-v5 ./cmd/dns-helper-v5
	$(BUILD_ENV) $(GO) build -trimpath -tags "bundle_dns_all_native,nethttpomithttp2,http2legacy" -ldflags "$(LDFLAGS)" -o $(BIN)/$(APP)-dns-all-native ./cmd/dns-helper
	$(BUILD_ENV) $(GO) build -trimpath -tags nethttpomithttp2,http2legacy -ldflags "$(LDFLAGS)" -o $(BIN)/$(APP)-deploy-all ./cmd/deploy-helper
	$(BUILD_ENV) $(GO) build -trimpath -tags nethttpomithttp2,http2legacy -ldflags "$(LDFLAGS)" -o $(BIN)/$(APP)-notify-all ./cmd/notify-helper

# all-lego 单独构建：全部服务商使用 lego 实现。
bundle-lego:
	mkdir -p $(BIN)
	$(BUILD_ENV) $(GO) build -trimpath -tags "bundle_dns_all_lego,nethttpomithttp2,http2legacy" -ldflags "$(LDFLAGS)" -o $(BIN)/$(APP)-dns-all-lego ./cmd/dns-helper

# all-lego-v5 单独构建：全部服务商使用 lego v5 实现。
# DCE 标签与上游 lego v5 release 保持一致，避免 validator/ozzo 模板代码进入二进制。
bundle-lego-v5:
	mkdir -p $(BIN)
	$(BUILD_ENV) $(GO) build -trimpath -tags "nethttpomithttp2,http2legacy,urfave_cli_no_template,ozzo_validation_no_template,grpcnotrace,validator_novalidatefn" -ldflags "$(LDFLAGS)" -o $(BIN)/$(APP)-dns-all-lego-v5 ./cmd/dns-helper-v5

# all-native 单独构建（用于体积/依赖检查）。
bundle-native:
	mkdir -p $(BIN)
	$(BUILD_ENV) $(GO) build -trimpath -tags "bundle_dns_all_native,nethttpomithttp2,http2legacy" -ldflags "$(LDFLAGS)" -o $(BIN)/$(APP)-dns-all-native ./cmd/dns-helper

# 生成 plugins.json 与各合并包的分组注册文件（manifest + gen）。
# 改动会被链接进产物的代码后运行本目标，并把变更一起提交；CI 会校验是否遗漏。
# gen 为历史名称，与 manifest 等价。
manifest gen: gen-v5
	$(GO) run scripts/pluginctl/main.go manifest
	$(GO) run scripts/pluginctl/main.go gen

# 生成 lego v5 的包装包、分组与补充元数据。
gen-v5:
	$(GO) run scripts/gen_dns_v5_plugins/main.go

# 构建单个合并包：make plugin KIND=dns ID=all-lego GOOS=linux GOARCH=amd64
# DNS 的 ID 为 all-lego、all-lego-v5 或 all-native；部署和通知的 ID 为 all。
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
