GOTEST    := go test
GOBUILD   := go build
BIN_DIR   := bin
IMAGE     := ghcr.io/masucci/branchdb-operator

# E2E 設定
COLIMA_PROFILE  := branchdb-k8s
ZFSAGENT_TOKEN  := e2e-token
ZFSAGENT_PORT   := 9000
SNAPSHOT_NAME   := base

.PHONY: all test cover build fmt web-build generate manifests \
        image-build image-push \
        e2e-k8s-up e2e-k8s-run e2e-k8s-down e2e-k8s-provision \
        clean help

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

# ── E2E テスト（Colima k3s + ZFS/NFS） ───────────────────────────────────────
# 使い方:
#   make e2e-k8s-up    # VM起動 → プロビジョニング → Helm デプロイ
#   make e2e-k8s-run   # E2E テスト実行（up の後に実行）
#   make e2e-k8s-down  # VM 削除

e2e-k8s-up: e2e-k8s-start e2e-k8s-provision e2e-k8s-deploy
	@echo "E2E 環境の準備が完了しました"

e2e-k8s-start:
	colima start --profile $(COLIMA_PROFILE) \
	  --edit=false \
	  --file deploy/k8s/e2e/colima.yaml 2>/dev/null || \
	colima start --profile $(COLIMA_PROFILE)

e2e-k8s-provision:
	$(eval VM_IP := $(shell colima list -j | python3 -c "import sys,json;d=json.load(sys.stdin);[print(x['address']) for x in d if x.get('profile')=='$(COLIMA_PROFILE)']" 2>/dev/null || echo ""))
	@if [ -z "$(VM_IP)" ]; then echo "ERROR: VM IP を取得できませんでした"; exit 1; fi
	@echo "VM IP: $(VM_IP)"
	colima ssh --profile $(COLIMA_PROFILE) -- \
	  sudo REPO_DIR=/Users/$(USER)/sources/branchdb-operator \
	  ZFSAGENT_TOKEN=$(ZFSAGENT_TOKEN) \
	  ZFSAGENT_PORT=$(ZFSAGENT_PORT) \
	  SNAPSHOT_NAME=$(SNAPSHOT_NAME) \
	  bash /Users/$(USER)/sources/branchdb-operator/deploy/k8s/e2e/provision.sh

e2e-k8s-deploy:
	$(eval VM_IP := $(shell colima list -j | python3 -c "import sys,json;d=json.load(sys.stdin);[print(x['address']) for x in d if x.get('profile')=='$(COLIMA_PROFILE)']" 2>/dev/null || echo ""))
	@if [ -z "$(VM_IP)" ]; then echo "ERROR: VM IP を取得できませんでした"; exit 1; fi
	KUBECONFIG="$(HOME)/.colima/$(COLIMA_PROFILE)/kubeconfig" \
	helm upgrade --install branchdb deploy/helm/branchdb \
	  --namespace branchdb-system \
	  --create-namespace \
	  --set installCRDs=true \
	  --set zfsAgent.url=http://$(VM_IP):$(ZFSAGENT_PORT) \
	  --set zfsAgent.token=$(ZFSAGENT_TOKEN) \
	  --set externalHost=$(VM_IP) \
	  --set image.repository=branchdb-operator \
	  --set image.tag=e2e \
	  --set image.pullPolicy=Never \
	  --set apiServer.image.repository=branchdb \
	  --set apiServer.image.tag=e2e \
	  --set apiServer.image.pullPolicy=Never \
	  --set apiServer.service.type=NodePort \
	  --wait --timeout 120s

e2e-k8s-run:
	$(eval VM_IP := $(shell colima list -j | python3 -c "import sys,json;d=json.load(sys.stdin);[print(x['address']) for x in d if x.get('profile')=='$(COLIMA_PROFILE)']" 2>/dev/null || echo ""))
	$(eval API_PORT := $(shell KUBECONFIG="$(HOME)/.colima/$(COLIMA_PROFILE)/kubeconfig" \
	  kubectl get svc -n branchdb-system branchdb-api \
	  -o jsonpath='{.spec.ports[0].nodePort}' 2>/dev/null || echo "8080"))
	@echo "E2E: API=http://$(VM_IP):$(API_PORT)  snapshot=$(SNAPSHOT_NAME)"
	KUBECONFIG="$(HOME)/.colima/$(COLIMA_PROFILE)/kubeconfig" \
	BRANCHDB_API_URL=http://$(VM_IP):$(API_PORT) \
	BRANCHDB_SNAPSHOT=$(SNAPSHOT_NAME) \
	$(GOTEST) ./test/e2e/... -v -timeout 10m -count=1

e2e-k8s-down:
	colima delete --profile $(COLIMA_PROFILE) --force 2>/dev/null || true

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
	@echo ""
	@echo "  E2E (Colima k3s + ZFS/NFS):"
	@echo "  e2e-k8s-up    VM 起動 → プロビジョニング → Helm デプロイ"
	@echo "  e2e-k8s-run   E2E テスト実行"
	@echo "  e2e-k8s-down  VM 削除"
