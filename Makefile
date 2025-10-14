# Aleo Utils Makefile
# Convenient wrapper around build.sh for common development tasks

.PHONY: help build clean test lint docs deps env all optimize debug bench install

# Default target
all: build

# Show help
help:
	@echo "Aleo Utils - Available Make Targets:"
	@echo ""
	@echo "  make build      - Build the project (release mode)"
	@echo "  make debug      - Build in debug mode"
	@echo "  make optimize   - Build with WASM optimization"
	@echo "  make clean      - Clean build artifacts"
	@echo "  make test       - Run all tests"
	@echo "  make lint       - Run linting checks"
	@echo "  make docs       - Generate documentation"
	@echo "  make deps       - Install all dependencies"
	@echo "  make env        - Check development environment"
	@echo "  make bench      - Run performance benchmarks"
	@echo "  make install    - Complete setup (deps + env)"
	@echo "  make all        - Run complete build pipeline"
	@echo ""
	@echo "Combined targets:"
	@echo "  make dev        - Development build (debug + test + lint)"
	@echo "  make ci         - CI pipeline (clean + lint + build + test)"
	@echo "  make release    - Release build (clean + optimize + docs + test)"
	@echo ""

# Basic build targets
build:
	./build.sh

debug:
	./build.sh --debug

optimize:
	./build.sh --optimize

clean:
	./build.sh --clean

# Quality and testing
test:
	./build.sh --test

lint:
	./build.sh --lint

bench:
	./build.sh --benchmark

# Documentation and setup
docs:
	./build.sh --docs

deps:
	./build.sh --install-deps

env:
	./build.sh --check-env

install: deps env

# Complete pipeline
all:
	./build.sh --all

# Combined workflows
dev: debug test lint
	@echo "Development build completed"

ci: clean lint build test
	@echo "CI pipeline completed"

release: clean optimize docs test
	@echo "Release build completed"

# Memory safety specific targets
memory-test:
	@echo "Running memory safety tests..."
	go test -v -run TestMemoryFixValidation
	go test -v -run TestMemoryCapacityMismatch

memory-audit:
	@echo "Running memory audit..."
	go test -v -run TestMemoryCapacityMismatch
	@echo ""
	@echo "Check MEMORY_FIX_PROPOSAL.md for details"

# Development helpers
format:
	cargo fmt
	@if command -v go >/dev/null 2>&1; then go fmt ./...; fi

check:
	cargo check
	@if command -v go >/dev/null 2>&1 && [ -f go.mod ]; then go vet ./...; fi

watch:
	@echo "Watching for changes... (requires cargo-watch)"
	@if command -v cargo-watch >/dev/null 2>&1; then \
		cargo watch -x 'build --target wasm32-wasi'; \
	else \
		echo "Install cargo-watch: cargo install cargo-watch"; \
	fi

# Quick start for new developers
quickstart:
	@echo "Setting up Aleo Utils development environment..."
	./build.sh --check-env --install-deps --all
	@echo ""
	@echo "Setup complete! Try:"
	@echo "  make dev      - for development builds"
	@echo "  make test     - to run tests"
	@echo "  make help     - for more options"