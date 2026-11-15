APP := lactssl
GO := go
BUILD_ENV := CGO_ENABLED=0
LDFLAGS := -s -w
BIN := bin

.PHONY: all build bundle gen manifest plugins test fmt vet check clean index

all: build

build: bundle

# 兼容旧版主程序的 bundle helper；新主程序不再使用。
bundle:
	mkdir -p $(BIN)
	$(BUILD_ENV) $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/$(APP)-dns-helper ./cmd/dns-helper
	$(BUILD_ENV) $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/$(APP)-deploy-helper ./cmd/deploy-helper
	$(BUILD_ENV) $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/$(APP)-notify-helper ./cmd/notify-helper

# 生成 plugins.json 与 select_*.go。
gen:
	$(GO) run scripts/pluginctl/main.go manifest
	$(GO) run scripts/pluginctl/main.go gen

manifest:
	$(GO) run scripts/pluginctl/main.go manifest

# 构建单个插件：make plugin KIND=dns ID=cloudflare GOOS=linux GOARCH=amd64
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

clean:
	rm -rf $(BIN) dist out index.json index.json.sig
