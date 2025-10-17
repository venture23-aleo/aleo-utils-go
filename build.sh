#!/bin/bash

# Enhanced Aleo Utils Build Script
# Usage: ./build.sh [OPTIONS]
#
# OPTIONS:
#   --optimize, -o     Run WASM optimization after build
#   --debug, -d        Build in debug mode (default is release)
#   --clean, -c        Clean build artifacts before building
#   --test, -t         Run tests after successful build
#   --verbose, -v      Enable verbose output
#   --install-deps, -i Install all required dependencies
#   --lint, -l         Run linting checks (clippy, fmt, go fmt)
#   --docs, -D         Generate documentation
#   --check-env, -e    Check and setup development environment
#   --benchmark, -b    Run performance benchmarks
#   --all, -a          Run all operations (clean, deps, lint, build, optimize, docs, test)
#   --help, -h         Show this help message
#
# Examples:
#   ./build.sh                    # Basic release build
#   ./build.sh --optimize         # Release build with optimization
#   ./build.sh -o -t -v          # Optimized build with tests and verbose output
#   ./build.sh --debug --clean    # Clean debug build
#   ./build.sh --install-deps     # Install all dependencies
#   ./build.sh --all             # Complete development setup and build

set -e  # Exit on any error

# Default values
OPTIMIZE=false
DEBUG=false
CLEAN=false
RUN_TESTS=false
VERBOSE=false
INSTALL_DEPS=false
RUN_LINT=false
GENERATE_DOCS=false
CHECK_ENV=false
RUN_BENCHMARKS=false
RUN_ALL=false
BUILD_TARGET="wasm32-wasi"
BUILD_MODE="release"
WASM_OUTPUT="aleo_utils.wasm"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Helper functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

show_help() {
    echo "Enhanced Aleo Utils Build Script"
    echo ""
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "OPTIONS:"
    echo "  --optimize, -o       Run WASM optimization after build"
    echo "  --debug, -d          Build in debug mode (default is release)"
    echo "  --clean, -c          Clean build artifacts before building"
    echo "  --test, -t           Run tests after successful build"
    echo "  --verbose, -v        Enable verbose output"
    echo "  --install-deps, -i   Install all required dependencies"
    echo "  --lint, -l           Run linting checks (clippy, fmt, go fmt)"
    echo "  --docs, -D           Generate documentation"
    echo "  --check-env, -e      Check and setup development environment"
    echo "  --benchmark, -b      Run performance benchmarks"
    echo "  --all, -a            Run all operations (clean, deps, lint, build, optimize, docs, test)"
    echo "  --help, -h           Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0                    # Basic release build"
    echo "  $0 --optimize         # Release build with optimization"
    echo "  $0 -o -t -v          # Optimized build with tests and verbose output"
    echo "  $0 --debug --clean    # Clean debug build"
    echo "  $0 --install-deps     # Install all dependencies"
    echo "  $0 --all             # Complete development setup and build"
    echo ""
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --optimize|-o)
            OPTIMIZE=true
            shift
            ;;
        --debug|-d)
            DEBUG=true
            BUILD_MODE="debug"
            shift
            ;;
        --clean|-c)
            CLEAN=true
            shift
            ;;
        --test|-t)
            RUN_TESTS=true
            shift
            ;;
        --verbose|-v)
            VERBOSE=true
            shift
            ;;
        --install-deps|-i)
            INSTALL_DEPS=true
            shift
            ;;
        --lint|-l)
            RUN_LINT=true
            shift
            ;;
        --docs|-D)
            GENERATE_DOCS=true
            shift
            ;;
        --check-env|-e)
            CHECK_ENV=true
            shift
            ;;
        --benchmark|-b)
            RUN_BENCHMARKS=true
            shift
            ;;
        --all|-a)
            RUN_ALL=true
            CLEAN=true
            INSTALL_DEPS=true
            RUN_LINT=true
            OPTIMIZE=true
            GENERATE_DOCS=true
            RUN_TESTS=true
            shift
            ;;
        --help|-h)
            show_help
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            show_help
            exit 1
            ;;
    esac
done

# Check and setup development environment
check_environment() {
    if $CHECK_ENV || $RUN_ALL; then
        log_info "Checking development environment..."

        # Check Rust installation
        if ! command -v rustc &> /dev/null; then
            log_error "Rust is not installed. Please install from https://rustup.rs/"
            exit 1
        fi

        # Check Rust 1.76.0 toolchain (as specified in README)
        log_info "Checking Rust 1.76.0 toolchain..."
        current_toolchain=$(rustup show active-toolchain | cut -d' ' -f1)
        log_info "Current toolchain: $current_toolchain"

        # Verify we're using the correct Rust version
        if ! echo "$current_toolchain" | grep -q "1.76.0"; then
            log_warning "Expected Rust 1.76.0, but found: $current_toolchain"
            log_info "The rust-toolchain.toml file should automatically select 1.76.0"

            # Check if 1.76.0 is installed
            if ! rustup toolchain list | grep -q "1.76.0"; then
                log_info "Installing Rust 1.76.0..."
                rustup toolchain install 1.76.0
            fi
        fi

        # Ensure we have the wasm32-wasi target
        if ! rustup target list --installed | grep -q "wasm32-wasi"; then
            log_info "Installing wasm32-wasi target..."
            rustup target add wasm32-wasi
        fi

        # Check Go installation
        if ! command -v go &> /dev/null; then
            log_warning "Go is not installed. Some features may not work."
            log_info "Install Go from https://golang.org/dl/"
        fi

        # Check rustup components
        log_info "Checking Rust components..."
        if ! rustup component list --installed | grep -q "clippy"; then
            log_info "Installing clippy..."
            rustup component add clippy
        fi

        if ! rustup component list --installed | grep -q "rustfmt"; then
            log_info "Installing rustfmt..."
            rustup component add rustfmt
        fi

        # Check wasm32-wasi target
        if ! rustup target list --installed | grep -q "wasm32-wasi"; then
            log_info "Installing wasm32-wasi target..."
            rustup target add wasm32-wasi
        fi

        log_success "Environment check completed"
    fi
}

# Install dependencies
install_dependencies() {
    if $INSTALL_DEPS || $RUN_ALL; then
        log_info "Installing dependencies..."

        # Install Rust tools
        log_info "Installing Rust development tools..."

        # WASM optimization tools
        if ! command -v wasm-snip &> /dev/null; then
            log_info "Installing wasm-snip..."
            cargo install wasm-snip
        fi

        if ! command -v wasm-pack &> /dev/null; then
            log_info "Installing wasm-pack..."
            cargo install wasm-pack
        fi

        # Install binaryen (wasm-opt) based on platform
        if ! command -v wasm-opt &> /dev/null; then
            log_info "Installing binaryen tools..."
            case "$(uname -s)" in
                Darwin)
                    if command -v brew &> /dev/null; then
                        brew install binaryen
                    else
                        log_warning "Homebrew not found. Please install binaryen manually."
                    fi
                    ;;
                Linux)
                    if command -v apt-get &> /dev/null; then
                        sudo apt-get update && sudo apt-get install -y binaryen
                    elif command -v yum &> /dev/null; then
                        sudo yum install -y binaryen
                    else
                        log_warning "Package manager not found. Please install binaryen manually."
                    fi
                    ;;
                *)
                    log_warning "Unsupported platform. Please install binaryen manually."
                    ;;
            esac
        fi

        # Install Go dependencies if go.mod exists
        if [ -f "go.mod" ] && command -v go &> /dev/null; then
            log_info "Installing Go dependencies..."
            go mod download
            go mod tidy
        fi

        log_success "Dependencies installation completed"
    fi
}

# Check required tools
check_tools() {
    log_info "Checking required tools..."

    if ! command -v cargo &> /dev/null; then
        log_error "cargo is not installed. Please install Rust."
        exit 1
    fi

    if $OPTIMIZE; then
        if ! command -v wasm-snip &> /dev/null; then
            log_warning "wasm-snip not found. Install with: ./build.sh --install-deps"
        fi

        if ! command -v wasm-opt &> /dev/null; then
            log_warning "wasm-opt not found. Install with: ./build.sh --install-deps"
        fi
    fi

    # Check if wasm32-wasi target is installed
    if ! rustup target list --installed | grep -q "wasm32-wasi"; then
        log_info "Installing wasm32-wasi target..."
        rustup target add wasm32-wasi
    fi
}

# Clean build artifacts
clean_build() {
    if $CLEAN; then
        log_info "Cleaning build artifacts..."
        cargo clean
        if [ -f "$WASM_OUTPUT" ]; then
            rm "$WASM_OUTPUT"
            log_info "Removed existing $WASM_OUTPUT"
        fi
    fi
}

# Build the project
build_project() {
    log_info "Building project in $BUILD_MODE mode..."

    local build_flags="--target $BUILD_TARGET"

    if [ "$BUILD_MODE" = "release" ]; then
        build_flags="$build_flags --release"
    fi

    if $VERBOSE; then
        build_flags="$build_flags --verbose"
    fi

    # Execute cargo build
    if $VERBOSE; then
        log_info "Running: cargo build $build_flags"
    fi

    cargo build $build_flags

    # Copy the built WASM file
    local source_path="target/$BUILD_TARGET/$BUILD_MODE/aleo_utils.wasm"

    if [ ! -f "$source_path" ]; then
        log_error "Build failed: $source_path not found"
        exit 1
    fi

    cp "$source_path" "$WASM_OUTPUT"
    log_success "Built $WASM_OUTPUT ($(du -h "$WASM_OUTPUT" | cut -f1))"
}

# Optimize WASM
optimize_wasm() {
    if $OPTIMIZE; then
        log_info "Optimizing WASM binary..."

        local original_size=$(stat -f%z "$WASM_OUTPUT" 2>/dev/null || stat -c%s "$WASM_OUTPUT" 2>/dev/null)

        # Remove panic handling code
        if command -v wasm-snip &> /dev/null; then
            log_info "Removing panic handling code..."
            wasm-snip --snip-rust-panicking-code -o "$WASM_OUTPUT" "$WASM_OUTPUT"
        else
            log_warning "Skipping wasm-snip optimization (not installed)"
        fi

        # Optimize with wasm-opt
        if command -v wasm-opt &> /dev/null; then
            log_info "Running wasm-opt optimizations..."
            wasm-opt "$WASM_OUTPUT" -all -o "$WASM_OUTPUT" -Os --strip-debug --strip-dwarf --dce
        else
            log_warning "Skipping wasm-opt optimization (not installed)"
        fi

        local optimized_size=$(stat -f%z "$WASM_OUTPUT" 2>/dev/null || stat -c%s "$WASM_OUTPUT" 2>/dev/null)
        local reduction=$((original_size - optimized_size))
        local percentage=$((reduction * 100 / original_size))

        log_success "Optimization complete: reduced by ${reduction} bytes (${percentage}%)"
        log_success "Final size: $(du -h "$WASM_OUTPUT" | cut -f1)"
    fi
}

# Run linting checks
run_linting() {
    if $RUN_LINT || $RUN_ALL; then
        log_info "Running linting checks..."

        # Rust linting
        log_info "Running Rust clippy..."
        cargo clippy --all-targets --all-features -- -D warnings

        log_info "Checking Rust formatting..."
        cargo fmt -- --check

        # Go linting if available
        if command -v go &> /dev/null && [ -f "go.mod" ]; then
            log_info "Running Go formatting check..."
            if ! go fmt ./...; then
                log_warning "Go code needs formatting. Run: go fmt ./..."
            fi

            # Run go vet if available
            log_info "Running go vet..."
            go vet ./...

            # Run golint if available
            if command -v golint &> /dev/null; then
                log_info "Running golint..."
                golint ./...
            fi
        fi

        log_success "Linting checks completed"
    fi
}

# Generate documentation
generate_docs() {
    if $GENERATE_DOCS || $RUN_ALL; then
        log_info "Generating documentation..."

        # Rust documentation
        log_info "Generating Rust documentation..."
        cargo doc --no-deps --document-private-items

        # Go documentation if available
        if command -v go &> /dev/null && [ -f "go.mod" ]; then
            log_info "Generating Go documentation..."
            go doc -all ./... > docs/go-docs.txt 2>/dev/null || true
        fi

        log_success "Documentation generation completed"
        log_info "Rust docs available at: target/doc/"
    fi
}

# Run performance benchmarks
run_benchmarks() {
    if $RUN_BENCHMARKS; then
        log_info "Running performance benchmarks..."

        # Rust benchmarks
        if cargo test --benches &> /dev/null; then
            log_info "Running Rust benchmarks..."
            cargo bench
        else
            log_info "No Rust benchmarks found"
        fi

        # Go benchmarks
        if command -v go &> /dev/null && [ -f "go.mod" ]; then
            log_info "Running Go benchmarks..."
            go test -bench=. -benchmem ./...
        fi

        log_success "Benchmarks completed"
    fi
}

# Run tests
run_tests() {
    if $RUN_TESTS || $RUN_ALL; then
        log_info "Running tests..."

        # Run Rust tests
        log_info "Running Rust tests..."
        if $VERBOSE; then
            cargo test -- --nocapture
        else
            cargo test
        fi

        # Run Go tests
        if command -v go &> /dev/null && [ -f "go.mod" ]; then
            log_info "Running Go tests..."
            if $VERBOSE; then
                go test -v ./...
            else
                go test ./...
            fi
        else
            log_warning "Go not found or no go.mod, skipping Go tests"
        fi

        # Run memory safety tests specifically
        if [ -f "memory_fix_validation_test.go" ]; then
            log_info "Running memory safety validation tests..."
            go test -v -run TestMemoryFixValidation
        fi

        log_success "All tests completed"
    fi
}

# Main execution
main() {
    log_info "Starting Aleo Utils build process..."

    if $RUN_ALL; then
        log_info "Running complete build pipeline..."
    fi

    log_info "Configuration: mode=$BUILD_MODE, optimize=$OPTIMIZE, clean=$CLEAN, test=$RUN_TESTS"
    log_info "Additional: deps=$INSTALL_DEPS, lint=$RUN_LINT, docs=$GENERATE_DOCS, env=$CHECK_ENV"

    # Environment and dependency setup
    check_environment
    install_dependencies

    # Code quality checks
    run_linting

    # Build process
    check_tools
    clean_build
    build_project
    optimize_wasm

    # Documentation and testing
    generate_docs
    run_tests
    run_benchmarks

    log_success "Build process completed successfully!"

    # Summary
    echo ""
    log_success "=== BUILD SUMMARY ==="
    if [ -f "$WASM_OUTPUT" ]; then
        log_info "WASM Output: $WASM_OUTPUT ($(du -h "$WASM_OUTPUT" | cut -f1))"
    fi

    if $GENERATE_DOCS || $RUN_ALL; then
        log_info "Documentation: target/doc/index.html"
    fi

    if $RUN_TESTS || $RUN_ALL; then
        log_info "Tests: All tests passed ✓"
    fi

    if $RUN_LINT || $RUN_ALL; then
        log_info "Linting: All checks passed ✓"
    fi

    echo ""
}

# Run main function
main