GOTEST    := go test
GOBUILD   := go build
BIN_DIR   := bin
IMAGE     := ghcr.io/keisuke/zfs-db-k8s

.PHONY: all test cover build fmt web-build generate manifests \
        image-build image-push clean help

all: test build

# ── Tests ─────────────────────────────────────────────────────────────────────
test:
	$(GOTEST) ./internal/... ./cmd/... -count=1

cover:
	$(GOTEST) ./internal/... -coverprofile=/tmp/zfsdb-k8s-cover.out -count=1
	go tool cover -func=/tmp/zfsdb-k8s-cover.out | tail -1
	go tool cover -html=/tmp/zfsdb-k8s-cover.out

# ── Build ─────────────────────────────────────────────────────────────────────
build:
	$(GOBUILD) -o $(BIN_DIR)/branchdb    ./cmd/branchdb
	$(GOBUILD) -o $(BIN_DIR)/operator    ./cmd/operator
	$(GOBUILD) -o $(BIN_DIR)/zfsagent   ./cmd/zfsagent

fmt:
	gofmt -w .

# ── Web console ───────────────────────────────────────────────────────────────
web-build:
	cd web && npm install && npm run build

web-dev:
	cd web && npm install && npm run dev

# ── CRD / manifests ───────────────────────────────────────────────────────────
generate:
	go generate ./...

manifests:
	go run sigs.k8s.io/controller-tools/cmd/controller-gen \
		crd \
		paths="./api/..." \
		output:crd:artifacts:config=deploy/k8s/crd

# ── Container image ───────────────────────────────────────────────────────────
image-build:
	docker build -t $(IMAGE):latest .

image-push: image-build
	docker push $(IMAGE):latest

clean:
	rm -rf $(BIN_DIR)

help:
	@echo "Targets:"
	@echo "  test          ユニットテスト実行"
	@echo "  cover         カバレッジレポート表示"
	@echo "  build         バイナリビルド → $(BIN_DIR)/"
	@echo "  web-build     SPA コンソールをビルド → internal/interface/api/k8s-dist/"
	@echo "  web-dev       SPA 開発サーバー起動 (hot reload, :5173)"
	@echo "  generate      コード生成 (deepcopy等)"
	@echo "  manifests     CRD YAML 生成"
	@echo "  image-build   コンテナイメージビルド"
