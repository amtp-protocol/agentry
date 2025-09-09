#!/bin/bash

# AMTP Gateway Test Runner
# This script runs all tests in the project

set -e

echo "🧪 Running AMTP Gateway Tests"
echo "=============================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Change to project root
cd "$(dirname "$0")/.."

print_status "Running unit tests..."

# Test individual packages
PACKAGES=(
    "./pkg/uuid"
    "./internal/agents"
    "./internal/config"
    "./internal/discovery"
    "./internal/errors"
    "./internal/middleware"
    "./internal/processing"
    "./internal/schema"
    "./internal/server"
    "./internal/storage"
    "./internal/types"
    "./internal/validation"
)

FAILED_PACKAGES=()
PASSED_PACKAGES=()

for package in "${PACKAGES[@]}"; do
    print_status "Testing package: $package"
    
    if go test "$package" -v; then
        print_success "✓ $package tests passed"
        PASSED_PACKAGES+=("$package")
    else
        print_error "✗ $package tests failed"
        FAILED_PACKAGES+=("$package")
    fi
    echo ""
done

# Run integration tests
print_status "Running integration tests..."
if go test ./tests -v; then
    print_success "✓ Integration tests passed"
    PASSED_PACKAGES+=("./tests")
else
    print_error "✗ Integration tests failed"
    FAILED_PACKAGES+=("./tests")
fi

# Run benchmarks
print_status "Running benchmarks..."
if go test ./internal/processing -bench=. -benchmem; then
    print_success "✓ Benchmarks completed"
else
    print_warning "⚠ Benchmarks had issues"
fi

# Summary
echo ""
echo "=============================="
echo "🏁 Test Summary"
echo "=============================="

if [ ${#PASSED_PACKAGES[@]} -gt 0 ]; then
    print_success "Passed packages (${#PASSED_PACKAGES[@]}):"
    for package in "${PASSED_PACKAGES[@]}"; do
        echo "  ✓ $package"
    done
fi

if [ ${#FAILED_PACKAGES[@]} -gt 0 ]; then
    print_error "Failed packages (${#FAILED_PACKAGES[@]}):"
    for package in "${FAILED_PACKAGES[@]}"; do
        echo "  ✗ $package"
    done
    echo ""
    print_error "Some tests failed. Please check the output above."
    exit 1
else
    print_success "All tests passed! 🎉"
fi

# Test coverage
print_status "Generating test coverage report..."
if go test ./... -coverprofile=coverage.out; then
    go tool cover -html=coverage.out -o coverage.html
    print_success "Coverage report generated: coverage.html"
    
    # Show coverage summary
    go tool cover -func=coverage.out | tail -1
else
    print_warning "Could not generate coverage report"
fi

echo ""
print_success "Test run completed successfully!"
