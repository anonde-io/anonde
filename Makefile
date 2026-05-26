# anonde top-level dev Makefile.
#
# Day-to-day developer workflow targets: codegen, build, test, local
# run, Docker. The benchmark matrix lives under bench/Makefile (run via
# `make -C bench help`) and is intentionally separate; corpus downloads
# are heavy and not part of the inner loop.
#
# Run `make help` to see every target with its description.

.DEFAULT_GOAL := help

# protoc-gen-go, protoc-gen-connect-go, protoc-gen-go-grpc, and
# protoc-gen-grpc-gateway all live under $(go env GOPATH)/bin, which
# isn't normally on PATH. Prepending it here lets `buf generate` find
# every plugin without the operator setting PATH themselves.
GOBIN ?= $(shell go env GOPATH)/bin
export PATH := $(GOBIN):$(PATH)

# Single-source values for Docker smoke runs.
IMAGE      := anonde-smoke
CONTAINER  := anonde-smoke
PORT       := 8081

# ──────────────────────────────────────────────────────────────────────
# help
# ──────────────────────────────────────────────────────────────────────

.PHONY: help
help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"; printf "Targets:\n"} \
	    /^[a-zA-Z0-9_-]+:.*## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2} \
	    /^##@/ {printf "\n\033[1m%s\033[0m\n", substr($$0, 5)}' $(MAKEFILE_LIST)

##@ Proto / codegen

.PHONY: tools
tools: ## Install/refresh protoc-gen-* plugins used by buf generate
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install connectrpc.com/connect/cmd/protoc-gen-connect-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@latest
	go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@latest
	@echo "plugins installed under $(GOBIN)"

.PHONY: proto
proto: ## Regenerate proto Go code (run after editing proto/)
	buf generate
	@echo "regenerated gen/; don't forget go test ./internal/..."

.PHONY: proto-lint
proto-lint: ## buf lint
	buf lint

.PHONY: proto-clean
proto-clean: ## Wipe gen/ so the next `make proto` is fully fresh
	rm -rf gen

.PHONY: proto-rebuild
proto-rebuild: proto-clean proto ## Wipe + regenerate (use after a rename)

##@ Build / test

.PHONY: build
build: ## go build ./...
	go build ./...

.PHONY: build-ner
build-ner: ## Build with -tags hugot (GLiNER + libonnxruntime required)
	go build -tags hugot ./...

.PHONY: test
test: ## Run the whole test suite
	go test ./...

.PHONY: test-api
test-api: ## Run only the api package tests (verbose)
	go test -v ./internal/api/...

.PHONY: e2e
e2e: ## Boot the real binary, drive every REST endpoint, assert /metrics ticked (~5s)
	go test ./e2e/... -count=1 -v

.PHONY: stress
stress: ## testcontainers-driven load + edge-case tier across all Docker variants (~10-15 min cold). Docker required. See stress/README.md.
	go test -tags stress -count=1 -timeout 30m -v ./stress/...

.PHONY: stress-fast
stress-fast: ## Same as `stress` but patterns variant only — fast smoke for harness changes (~2 min cold, ~30s warm)
	go test -tags stress -count=1 -timeout 10m -v -run 'TestStress.*/patterns' ./stress/...

.PHONY: vet
vet: ## go vet ./...
	go vet ./...

.PHONY: tidy
tidy: ## go mod tidy
	go mod tidy

##@ Local run (no Docker)

.PHONY: run
run: ## Run the anonde server on :$(PORT) (patterns backend, no NER)
	ANALYZER_BACKEND=patterns ANONDE_ADDR=:$(PORT) go run ./cmd/anonde/

.PHONY: run-ner
run-ner: ## Run the anonde server on :$(PORT) with GLiNER NER (needs libonnxruntime)
	ANALYZER_BACKEND=gliner ANONDE_ADDR=:$(PORT) go run -tags hugot ./cmd/anonde/

.PHONY: run-ner-pdf
run-ner-pdf: ## Run NER server on :$(PORT) with PDF redaction + Prometheus on :9090 (host needs pdftoppm + tesseract)
	ANALYZER_BACKEND=gliner ANONDE_ADDR=:$(PORT) \
		ANONDE_PDF_ENABLED=1 \
		METRICS_BIND=127.0.0.1:9090 \
		go run -tags hugot ./cmd/anonde/

##@ Docker

.PHONY: docker-build
docker-build: ## Build the patterns-only image ($(IMAGE):patterns)
	docker build -f Dockerfile.anonde -t $(IMAGE):patterns .

.PHONY: docker-build-ner
docker-build-ner: ## Build the NER image (~1.13 GB, $(IMAGE):ner, GLiNER base + YOLOS sig + tesseract + poppler)
	docker build -f Dockerfile.anonde-ner -t $(IMAGE):ner .

.PHONY: docker-build-ner-stack
docker-build-ner-stack: ## Build the lowest-leak image (~2.65 GB, GLiNER base+LARGE + YOLOS sig, $(IMAGE):ner-stack)
	docker build -f Dockerfile.anonde-ner-stack -t $(IMAGE):ner-stack .

.PHONY: docker-run
docker-run: docker-build ## Build + start the patterns container on :$(PORT) (text/JSON only, no PDF, no metrics)
	-docker rm -f $(CONTAINER) >/dev/null 2>&1
	docker run -d --name $(CONTAINER) -p $(PORT):8080 \
		-e ANALYZER_BACKEND=patterns $(IMAGE):patterns
	@sleep 1 && docker ps --filter name=$(CONTAINER) --format "  {{.Names}}\t{{.Status}}\t{{.Ports}}"
	@echo "→ http://localhost:$(PORT)"

.PHONY: docker-run-ner
docker-run-ner: docker-build-ner ## Build + start the NER container on :$(PORT), PDF endpoint + metrics on :9090
	-docker rm -f $(CONTAINER) >/dev/null 2>&1
	docker run -d --name $(CONTAINER) -p $(PORT):8080 -p 9090:9090 \
		-e ANALYZER_BACKEND=gliner \
		-e ANONDE_PDF_ENABLED=1 \
		-e METRICS_BIND=0.0.0.0:9090 \
		$(IMAGE):ner
	@sleep 1 && docker ps --filter name=$(CONTAINER) --format "  {{.Names}}\t{{.Status}}\t{{.Ports}}"
	@echo "→ http://localhost:$(PORT)  (metrics: http://localhost:9090/metrics)"

.PHONY: docker-logs
docker-logs: ## Tail logs from the running smoke container
	docker logs -f $(CONTAINER)

.PHONY: docker-stop
docker-stop: ## Stop + remove the smoke container
	-docker rm -f $(CONTAINER)

##@ Smoke test (against a running container on :$(PORT))

.PHONY: smoke
smoke: ## Hit ingest → reveal → delete against http://localhost:$(PORT)
	@echo "T1: POST /v1/anonymizations (server mints id)"
	@MINT=$$(curl -s -X POST http://localhost:$(PORT)/v1/anonymizations \
		-H "Content-Type: application/json" \
		-d '{"tenantId":"demo","contentFormat":"text","content":"Email alice@example.com"}'); \
	echo "  $$MINT"; \
	ID=$$(echo "$$MINT" | sed -E 's/.*"id":"([^"]+)".*/\1/'); \
	echo ""; \
	echo "T2: POST /v1/anonymizations/$$ID/reveal"; \
	BODY=$$(echo "$$MINT" | sed -E 's/.*"anonymizedContent":"([^"]+)".*/\1/'); \
	curl -s -X POST http://localhost:$(PORT)/v1/anonymizations/$$ID/reveal \
		-H "Content-Type: application/json" \
		-d "{\"tenantId\":\"demo\",\"actor\":\"test\",\"purpose\":\"smoke\",\"contentFormat\":\"text\",\"content\":\"$$BODY\"}"; \
	echo ""; \
	echo "T3: DELETE /v1/anonymizations/$$ID?tenantId=demo"; \
	curl -s -X DELETE "http://localhost:$(PORT)/v1/anonymizations/$$ID?tenantId=demo"; \
	echo ""

##@ Composite

.PHONY: regen
regen: proto-rebuild build test ## Full regen: wipe gen/, regenerate, build, test

.PHONY: ci
ci: vet test ## What CI should run: vet + full tests
