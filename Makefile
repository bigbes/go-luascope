# Path to the LuaJIT binary of Tarantool; see "Testing" in README.md.
LUAJIT ?=

# Version of golangci-lint and the Go toolchain it must run on: a linter built
# with an older Go cannot type-check a newer one; see CONTRIBUTING.md.
GOLANGCI_LINT_VERSION ?= v2.11.4
GOLANGCI_LINT_GO ?= go1.26.1

# Paths checked for spelling.
SPELL_PATHS ?= .

.PHONY: all
all: test lint spell

.PHONY: test
test:
	@go test ./... -count=1

.PHONY: testrace
testrace:
	@go test ./... -race -count=1

.PHONY: cover
cover:
	@go test ./... -count=1 -coverprofile=coverage.txt \
		-covermode=atomic -coverpkg=./...
	@go tool cover -func=coverage.txt | tail -1

.PHONY: lint
lint:
	@GOTOOLCHAIN=$(GOLANGCI_LINT_GO) golangci-lint run ./...

.PHONY: spell
spell:
	@codespell $(SPELL_PATHS)

.PHONY: fmt
fmt:
	@GOTOOLCHAIN=$(GOLANGCI_LINT_GO) golangci-lint fmt ./...

.PHONY: fuzz
fuzz:
	@go test ./parser -run '^$$' -fuzz=FuzzParse -fuzztime=5m

.PHONY: golden
golden:
	@test -n "$(LUAJIT)" || (echo "set LUAJIT to the path of Tarantool's LuaJIT" && false)
	@go test ./parser -update -luajit=$(LUAJIT) -count=1

.PHONY: vulncheck
vulncheck:
	@govulncheck ./...

.PHONY: tidy
tidy:
	@go mod tidy
