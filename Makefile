.DEFAULT_GOAL := help

OS   		:= $(shell uname | awk '{print tolower($$0)}')
ARCH 		:= $(shell case $$(uname -m) in (x86_64) echo amd64 ;; (aarch64) echo arm64 ;; (*) echo $$(uname -m) ;; esac)
BIN_DIR		:= ./bin

##### BINARY #####

BUF_VERSION	:= 1.57.0
BUF			:= $(abspath $(BIN_DIR)/buf)

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
	@git submodule update --init --recursive
	@uv sync
	@go mod tidy

##### LINT #####

.PHONY: lint
lint/: $(BUF)
lint: ## Run all lint ## make lint
	@echo "Running all lint..."
	go fmt ./...
	go tool strictgoimports -w -exclude "*.pb.go" -local "github.com/BIwashi/candecode" .
	$(BUF) lint

##### BUILD #####

# build targets
# opendbc build target with dependency tracking
OPENDBC_DIR 			:= third_party/opendbc
OPENDBC_GENERATOR_DIR 	:= $(OPENDBC_DIR)/opendbc/dbc/generator
OPENDBC_DBC_DIR 		:= $(OPENDBC_DIR)/opendbc/dbc
OPENDBC_SOURCES 		:= $(shell find $(OPENDBC_GENERATOR_DIR) -name "*.py" -o \( -name "*.dbc" -not -name "*_generated.dbc" \) 2>/dev/null)
OPENDBC_STAMP 			:= $(BIN_DIR)/.opendbc_stamp
# candecode build target with dependency tracking
CANDECODE_MAIN			:= ./cmd/candecode/main.go
CANDECODE_SOURCES		:= $(shell find . -name "*.go" -not -path "$(OPENDBC_DIR)/*" 2>/dev/null) go.mod go.sum
CANDECODE_BINARY		:= $(BIN_DIR)/candecode
# buf build target with dependency tracking
BUF_PROTO_DIR			:= pkg/proto
BUF_PROTO_SOURCES		:= $(shell find . -name "*.proto" 2>/dev/null) buf.gen.yaml buf.yaml
BUF_TARGETS				:= $(BUF_PROTO_DIR)/*.pb.go

$(BUF_TARGETS): $(BUF_PROTO_SOURCES) $(BUF)
	@echo "Generating protobuf files..."
	@$(BUF) generate

$(OPENDBC_STAMP): $(OPENDBC_SOURCES)
	@echo "Building opendbc files..."
	uv run scons -C $(OPENDBC_DIR) -j8
	@touch $(OPENDBC_STAMP)

$(CANDECODE_BINARY): $(CANDECODE_SOURCES) $(BUF_TARGETS)
	@echo "Building candecode binary..."
	@mkdir -p $(BIN_DIR)
	@go mod tidy
	@$(BUILD_ENV) go build $(BUILD_OPTS) -o $(CANDECODE_BINARY) $(CANDECODE_MAIN)
	@echo "Binary built: $(CANDECODE_BINARY)"

.PHONY: build/opendbc
build/opendbc: setup $(OPENDBC_STAMP) ## Build opendbc files ## make build/opendbc

.PHONY: build/buf
build/buf: $(BUF_TARGETS) ## Build buf ## make build/buf

.PHONY: build/cmd
build/cmd: ## Build cmd ## make build/cmd
build/cmd: $(BUF_TARGETS)
build/cmd: CGO_ENABLED ?= 0
build/cmd: BUILD_OS ?= $(OS)
build/cmd: BUILD_ARCH ?= $(ARCH)
build/cmd: BUILD_ENV ?= GOOS=$(BUILD_OS) GOARCH=$(BUILD_ARCH) CGO_ENABLED=$(CGO_ENABLED)
build/cmd: BUILD_OPTS ?= -trimpath -ldflags "-s -w -extldflags -static"
build/cmd: $(CANDECODE_BINARY)

.PHONY: build
build: build/opendbc build/buf build/cmd ## Build all components ## make build

##### RUN #####

.PHONY: run/convert
run/convert: ## Convert PCAPNG to MCAP ## make run/convert PCAPNG=input.pcapng DBC=reference.dbc
run/convert: $(OPENDBC_TARGETS) $(CANDECODE_BINARY) $(OPENDBC_STAMP)
run/convert: PCAPNG ?=
run/convert: DBC ?=
run/convert:
	@echo "Converting PCAPNG to MCAP..."
	@$(CANDECODE_BINARY) convert --pcapng-file $(PCAPNG) --dbc-file $(DBC)

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

.PHONY: clean/bin
clean/bin: ## Clean binary files ## make clean/bin
	@echo "Cleaning binary files..."
	@rm -rf $(BIN_DIR)/*
	@rm -f $(OPENDBC_STAMP)

.PHONY: clean
clean: clean/opendbc clean/bin ## Clean all files ## make clean

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
