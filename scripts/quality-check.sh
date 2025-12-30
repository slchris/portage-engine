#!/bin/bash
# Test script to run various Go code quality checks locally
# Similar to what Go Report Card runs

set -e

echo "=== Running gofmt ==="
gofmt -l . | grep -v vendor || echo "✓ All files are formatted"

echo ""
echo "=== Running go vet ==="
go vet ./... && echo "✓ No vet issues"

echo ""
echo "=== Running golangci-lint ==="
golangci-lint run --timeout=5m && echo "✓ No golangci-lint issues"

echo ""
echo "=== Running ineffassign ==="
if command -v ineffassign &> /dev/null; then
    ~/go/bin/ineffassign ./... && echo "✓ No ineffassign issues"
else
    echo "Installing ineffassign..."
    go install github.com/gordonklaus/ineffassign@latest
    ~/go/bin/ineffassign ./... && echo "✓ No ineffassign issues"
fi

echo ""
echo "=== Running gocyclo (complexity check) ==="
if command -v gocyclo &> /dev/null; then
    gocyclo -over 15 . | grep -v vendor || echo "✓ No high complexity functions"
else
    echo "Installing gocyclo..."
    go install github.com/fzipp/gocyclo/cmd/gocyclo@latest
    ~/go/bin/gocyclo -over 15 . | grep -v vendor || echo "✓ No high complexity functions"
fi

echo ""
echo "=== Running misspell ==="
if command -v misspell &> /dev/null; then
    misspell -error . && echo "✓ No spelling errors"
else
    echo "Installing misspell..."
    go install github.com/client9/misspell/cmd/misspell@latest
    ~/go/bin/misspell -error . && echo "✓ No spelling errors"
fi

echo ""
echo "=== Running gosec (security check) ==="
if command -v gosec &> /dev/null; then
    gosec -quiet -exclude=G204,G304,G107,G302 ./... && echo "✓ No critical security issues"
else
    echo "Installing gosec..."
    go install github.com/securego/gosec/v2/cmd/gosec@latest
    ~/go/bin/gosec -quiet -exclude=G204,G304,G107,G302 ./... && echo "✓ No critical security issues"
fi

echo ""
echo "=== Running govulncheck (vulnerability check) ==="
if command -v govulncheck &> /dev/null; then
    govulncheck ./... && echo "✓ No known vulnerabilities"
else
    echo "Installing govulncheck..."
    go install golang.org/x/vuln/cmd/govulncheck@latest
    ~/go/bin/govulncheck ./... && echo "✓ No known vulnerabilities"
fi

echo ""
echo "=== All checks completed! ==="
