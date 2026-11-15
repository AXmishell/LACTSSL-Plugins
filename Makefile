APP := lactssl
GO := go
BUILD_ENV := CGO_ENABLED=0
LDFLAGS := -s -w
BIN := bin

.PHONY: all build dns-helper deploy-helper notify-helper test fmt vet gen check clean

all: build

build: dns-helper deploy-helper notify-helper

dns-helper:
	mkdir -p $(BIN)
	$(BUILD_ENV) $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/$(APP)-dns-helper ./cmd/dns-helper

deploy-helper:
	mkdir -p $(BIN)
	$(BUILD_ENV) $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/$(APP)-deploy-helper ./cmd/deploy-helper

notify-helper:
	mkdir -p $(BIN)
	$(BUILD_ENV) $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/$(APP)-notify-helper ./cmd/notify-helper

test:
	$(GO) test ./...

fmt:
	gofmt -w .

vet:
	$(GO) vet ./...

gen:
	$(GO) run scripts/gen_dns_plugins/main.go

check:
	test -z "$$(gofmt -l .)" || (echo "gofmt required" >&2; exit 1)
	$(GO) vet ./...
	$(GO) test ./...

clean:
	rm -rf $(BIN) out dist
