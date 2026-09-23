APP_NAME := fan-video-ct
VERSION := $(shell sed -n 's/^var Version = "\([0-9.]*\)"/\1/p' internal/version/version.go | head -1)
GOFLAGS := -ldflags "-s -w -X github.com/meimolihan/fan-video-ct/internal/version.Version=$(VERSION)"

.PHONY: all build vet fmt test run release install docker clean

all: vet build

build:
	go build -trimpath $(GOFLAGS) -o bin/$(APP_NAME) .

vet:
	go vet ./...

fmt:
	gofmt -w .

test:
	go test ./...

run: build
	./bin/$(APP_NAME) -data ./data -port 8788

# 本地发布构建：dist/ 下生成 amd64 + arm64 二进制与 sha256
release:
	bash scripts/build-release.sh

# 安装为 systemd 服务（使用本地刚构建的二进制）
install: build
	bash scripts/install.sh -b bin/$(APP_NAME)

docker:
	docker build --build-arg FCT_VERSION=$(VERSION) -t mobufan/$(APP_NAME):$(VERSION) .

clean:
	rm -rf bin dist data