#!/bin/bash
# Metric: code quality + response compactness
# Higher = better (more tests, fewer source lines, fewer format calls)
cd /Users/arbaz/devmem

if ! go build -o /dev/null ./cmd/devmem 2>/dev/null; then echo "0"; exit 0; fi
if go test ./... -count=1 2>&1 | grep -q "^FAIL"; then echo "0"; exit 0; fi

TESTS=$(go test ./... -v -count=1 2>&1 | grep -c "^--- PASS")
SRC=$(find internal/ cmd/ -name "*.go" ! -name "*_test.go" -exec cat {} + | wc -l | tr -d ' ')

# Count verbose formatting in MCP layer (lower = more compact responses)
FORMAT_CALLS=$(grep -c 'WriteString\|Sprintf\|Fprintf' internal/mcp/tools.go internal/mcp/briefing.go internal/mcp/resources.go 2>/dev/null | tail -1 | cut -d: -f2)

# Metric: (tests * 100 / src_lines) + (1000 / format_calls)
# Higher tests/line + fewer format calls = better
python3 -c "print(round(($TESTS * 100 / $SRC) + (1000 / $FORMAT_CALLS), 2))"
