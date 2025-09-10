.DEFAULT_GOAL := help

OS   		:= $(shell uname | awk '{print tolower($$0)}')
ARCH 		:= $(shell case $$(uname -m) in (x86_64) echo amd64 ;; (aarch64) echo arm64 ;; (*) echo $$(uname -m) ;; esac)
BIN_DIR		:= ./bin
BUF_VERSION	:= 1.32.2
BUF			:= $(abspath $(BIN_DIR)/buf)
PROTOLINT_VERSION := 0.55.0

##### BINARY #####

buf: $(BUF)
$(BUF):
	@curl -sSL "https://github.com/bufbuild/buf/releases/download/v${BUF_VERSION}/buf-$(shell uname -s)-$(shell uname -m)" -o $(BUF) && chmod +x $(BUF)

##### SYNC #####

.PHONY: sync
sync: ## Sync submodules to remote main branch ## make sync
	@echo "Syncing submodules..."
	git submodule update --remote --init --recursive

##### SETUP #####

.PHONY: setup
setup: ## Setup environment ## make setup
	@echo "Setting up uv environment..."
	git submodule update --init --recursive
	uv sync
	go mod tidy

##### LINT #####

.PHONY: lint
lint/: ## Run all lint ## make lint/all
	@echo "Running all lint..."
	go fmt ./...
	go tool strictgoimports -w -local "github.com/BIwashi/candecode" .
	buf lint

##### BUILD #####

.PHONY: build/opendbc
build/opendbc: setup
build/opendbc: ## Build opendbc files ## make build/opendbc
	@echo "Building opendbc files..."
	uv run scons -C third_party/opendbc -j8

.PHONY: build/buf
build/buf: $(BUF)
build/buf: ## Build buf ## make build/buf
	@echo "Building buf..."
	@$(BUF) generate

.PHONY: build/cmd
build/cmd: ## Build cmd ## make build/cmd
build/cmd: CGO_ENABLED ?= 0
build/cmd: BUILD_OS ?= $(OS)
build/cmd: BUILD_ARCH ?= $(ARCH)
build/cmd: BUILD_ENV ?= GOOS=$(BUILD_OS) GOARCH=$(BUILD_ARCH) CGO_ENABLED=$(CGO_ENABLED)
build/cmd: BUILD_OPTS ?= -trimpath -ldflags "-s -w -extldflags -static"
build/cmd:
	@echo "Building candecode binary..."
	@$(BUILD_ENV) go build $(BUILD_OPTS) -o $(BIN_DIR)/candecode ./cmd/main.go
	@echo "Binary built: $(BIN_DIR)/candecode"

.PHONY: build
build: build/opendbc build/buf build/cmd ## Build all components ## make build

##### RUN #####

.PHONY: run/convert
run/convert: build ## Convert PCAPNG to MCAP ## make run/convert PCAPNG=input.pcapng DBC=toyota.dbc
run/convert: PCAPNG ?= pcapng/can_00001_20250908185300.pcapng
run/convert: DBC ?= third_party/opendbc/opendbc/dbc/toyota_new_mc_pt_generated.dbc
run/convert:
	@echo "Converting PCAPNG to MCAP..."
	@$(BIN_DIR)/candecode convert --pcapng-file $(PCAPNG) --dbc-file $(DBC)

##### TEST #####

.PHONY: test
test: ## Run tests ## make test
	@echo "Running tests..."
	@go test -v ./...

.PHONY: test/coverage
test/coverage: ## Run tests with coverage ## make test/coverage
	@echo "Running tests with coverage..."
	@go test -v -coverprofile=coverage.out ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

##### CLEAN #####

.PHONY: clean/opendbc
clean/opendbc: ## Clean opendbc build files ## make clean/opendbc
	@echo "Cleaning opendbc build files..."
	uv run scons -C third_party/opendbc -c

##### HELP #####

.PHONY: help
help: ## Display this help screen ## make or make help
	@echo ""
	@echo "Usage: make SUB_COMMAND argument_name=argument_value"
	@echo ""
	@echo "Command list:"
	@echo ""
	@printf "\033[36m%-30s\033[0m %-50s %s\n" "[Sub command]" "[Description]" "[Example]"
	@grep -E '^[/a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | perl -pe 's%^([/a-zA-Z_-]+):.*?(##)%$$1 $$2%' | awk -F " *?## *?" '{printf "\033[36m%-30s\033[0m %-50s %s\n", $$1, $$2, $$3}'
