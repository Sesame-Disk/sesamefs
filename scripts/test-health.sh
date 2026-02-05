#!/bin/bash
# =============================================================================
# Health, Readiness & Metrics Integration Tests for SesameFS
# =============================================================================
#
# Tests the monitoring endpoints:
#   1. GET /health    (liveness probe — always 200 if process alive)
#   2. GET /ready     (readiness probe — checks DB + S3)
#   3. GET /metrics   (Prometheus metrics)
#
# Usage:
#   ./scripts/test-health.sh [options]
#
# Options:
#   --verbose     Show response bodies
#   --help        Show this help
#
# Requirements:
#   - Backend running at $API_URL (default: http://localhost:8082)
#   - curl, jq installed
#
# =============================================================================

set -e

# Configuration
API_URL="${API_URL:-http://localhost:8082}"

# Options
VERBOSE=false

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Counters
TOTAL=0
PASSED=0
FAILED=0

# Parse arguments
for arg in "$@"; do
    case "$arg" in
        --verbose|-v) VERBOSE=true ;;
        --help|-h)
            head -25 "$0" | grep "^#" | sed 's/^# //' | sed 's/^#//'
            exit 0
            ;;
    esac
done

# Helper: pass
pass() {
    TOTAL=$((TOTAL + 1))
    PASSED=$((PASSED + 1))
    echo -e "${GREEN}[PASS]${NC} $1"
}

# Helper: fail
fail() {
    TOTAL=$((TOTAL + 1))
    FAILED=$((FAILED + 1))
    echo -e "${RED}[FAIL]${NC} $1"
}

echo "============================================"
echo " Health, Readiness & Metrics Tests"
echo " API_URL: $API_URL"
echo "============================================"
echo ""

# ============================================================
# 1. Liveness Probe (/health)
# ============================================================
echo "--- Test 1: Liveness probe ---"

RESPONSE=$(curl -s -w "\n%{http_code}" "$API_URL/health")
STATUS=$(echo "$RESPONSE" | tail -1)
BODY=$(echo "$RESPONSE" | head -n -1)

if [ "$STATUS" = "200" ]; then
    pass "GET /health returns 200"
else
    fail "GET /health returns 200 (got $STATUS)"
fi

# Check response body has correct fields
if echo "$BODY" | jq -e '.status' > /dev/null 2>&1; then
    HEALTH_STATUS=$(echo "$BODY" | jq -r '.status')
    if [ "$HEALTH_STATUS" = "healthy" ]; then
        pass "GET /health status = 'healthy'"
    else
        fail "GET /health status = 'healthy' (got '$HEALTH_STATUS')"
    fi
else
    fail "GET /health response has 'status' field"
fi

if echo "$BODY" | jq -e '.version' > /dev/null 2>&1; then
    VERSION=$(echo "$BODY" | jq -r '.version')
    if [ -n "$VERSION" ] && [ "$VERSION" != "null" ]; then
        pass "GET /health includes version ('$VERSION')"
    else
        fail "GET /health includes version (got null/empty)"
    fi
else
    fail "GET /health response has 'version' field"
fi

if [ "$VERBOSE" = true ]; then
    echo "  Response: $BODY"
fi

echo ""

# ============================================================
# 2. Liveness Probe - no auth required
# ============================================================
echo "--- Test 2: Liveness probe requires no auth ---"

# /health should work without any Authorization header
STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$API_URL/health")
if [ "$STATUS" = "200" ]; then
    pass "GET /health works without auth"
else
    fail "GET /health works without auth (got $STATUS)"
fi

echo ""

# ============================================================
# 3. Readiness Probe (/ready)
# ============================================================
echo "--- Test 3: Readiness probe ---"

RESPONSE=$(curl -s -w "\n%{http_code}" "$API_URL/ready")
STATUS=$(echo "$RESPONSE" | tail -1)
BODY=$(echo "$RESPONSE" | head -n -1)

if [ "$STATUS" = "200" ]; then
    pass "GET /ready returns 200 (all deps healthy)"
else
    # 503 means a dependency is down, which is also valid behavior
    if [ "$STATUS" = "503" ]; then
        pass "GET /ready returns 503 (dependency down — valid response)"
    else
        fail "GET /ready returns 200 or 503 (got $STATUS)"
    fi
fi

# Check response body structure
if echo "$BODY" | jq -e '.status' > /dev/null 2>&1; then
    READY_STATUS=$(echo "$BODY" | jq -r '.status')
    if [ "$READY_STATUS" = "ready" ] || [ "$READY_STATUS" = "not_ready" ]; then
        pass "GET /ready status = 'ready' or 'not_ready' (got '$READY_STATUS')"
    else
        fail "GET /ready status = 'ready' or 'not_ready' (got '$READY_STATUS')"
    fi
else
    fail "GET /ready response has 'status' field"
fi

# Check that 'checks' field exists
if echo "$BODY" | jq -e '.checks' > /dev/null 2>&1; then
    pass "GET /ready response has 'checks' object"
else
    fail "GET /ready response has 'checks' object"
fi

# If backend is fully up, check individual dependency statuses
if [ "$STATUS" = "200" ]; then
    DB_CHECK=$(echo "$BODY" | jq -r '.checks.database // empty')
    if [ "$DB_CHECK" = "ok" ]; then
        pass "GET /ready database check = 'ok'"
    elif [ -n "$DB_CHECK" ]; then
        pass "GET /ready database check present (value: '$DB_CHECK')"
    else
        pass "GET /ready no database check (may not be configured)"
    fi

    STORAGE_CHECK=$(echo "$BODY" | jq -r '.checks.storage // empty')
    if [ "$STORAGE_CHECK" = "ok" ]; then
        pass "GET /ready storage check = 'ok'"
    elif [ -n "$STORAGE_CHECK" ]; then
        pass "GET /ready storage check present (value: '$STORAGE_CHECK')"
    else
        pass "GET /ready no storage check (may not be configured)"
    fi
fi

if [ "$VERBOSE" = true ]; then
    echo "  Response: $BODY"
fi

echo ""

# ============================================================
# 4. Readiness Probe - no auth required
# ============================================================
echo "--- Test 4: Readiness probe requires no auth ---"

STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$API_URL/ready")
if [ "$STATUS" = "200" ] || [ "$STATUS" = "503" ]; then
    pass "GET /ready works without auth (got $STATUS)"
else
    fail "GET /ready works without auth (got $STATUS)"
fi

echo ""

# ============================================================
# 5. Prometheus Metrics (/metrics)
# ============================================================
echo "--- Test 5: Prometheus metrics endpoint ---"

RESPONSE=$(curl -s -w "\n%{http_code}" "$API_URL/metrics")
STATUS=$(echo "$RESPONSE" | tail -1)
BODY=$(echo "$RESPONSE" | head -n -1)

if [ "$STATUS" = "200" ]; then
    pass "GET /metrics returns 200"
else
    # Metrics may be disabled in config
    if [ "$STATUS" = "404" ]; then
        pass "GET /metrics returns 404 (metrics disabled in config — acceptable)"
        echo ""
        echo "============================================"
        echo " Results: $PASSED/$TOTAL passed, $FAILED failed"
        echo "============================================"
        if [ "$FAILED" -gt 0 ]; then
            exit 1
        fi
        exit 0
    else
        fail "GET /metrics returns 200 or 404 (got $STATUS)"
    fi
fi

# Check Prometheus text format
if echo "$BODY" | grep -q "# HELP"; then
    pass "GET /metrics contains Prometheus HELP lines"
else
    fail "GET /metrics contains Prometheus HELP lines"
fi

if echo "$BODY" | grep -q "# TYPE"; then
    pass "GET /metrics contains Prometheus TYPE lines"
else
    fail "GET /metrics contains Prometheus TYPE lines"
fi

# Check our custom metrics are registered
if echo "$BODY" | grep -q "http_requests_total"; then
    pass "GET /metrics has http_requests_total counter"
else
    fail "GET /metrics has http_requests_total counter"
fi

if echo "$BODY" | grep -q "http_request_duration_seconds"; then
    pass "GET /metrics has http_request_duration_seconds histogram"
else
    fail "GET /metrics has http_request_duration_seconds histogram"
fi

# Check Go runtime metrics are present (from promhttp default)
if echo "$BODY" | grep -q "go_goroutines"; then
    pass "GET /metrics has go_goroutines gauge"
else
    fail "GET /metrics has go_goroutines gauge"
fi

if echo "$BODY" | grep -q "go_memstats"; then
    pass "GET /metrics has go_memstats metrics"
else
    fail "GET /metrics has go_memstats metrics"
fi

echo ""

# ============================================================
# 6. Metrics - no auth required
# ============================================================
echo "--- Test 6: Metrics endpoint requires no auth ---"

STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$API_URL/metrics")
if [ "$STATUS" = "200" ]; then
    pass "GET /metrics works without auth"
else
    fail "GET /metrics works without auth (got $STATUS)"
fi

echo ""

# ============================================================
# 7. Content-Type headers
# ============================================================
echo "--- Test 7: Correct Content-Types ---"

# /health should return JSON
CT=$(curl -s -o /dev/null -w "%{content_type}" "$API_URL/health")
if echo "$CT" | grep -qi "application/json"; then
    pass "GET /health Content-Type is application/json"
else
    fail "GET /health Content-Type is application/json (got '$CT')"
fi

# /ready should return JSON
CT=$(curl -s -o /dev/null -w "%{content_type}" "$API_URL/ready")
if echo "$CT" | grep -qi "application/json"; then
    pass "GET /ready Content-Type is application/json"
else
    fail "GET /ready Content-Type is application/json (got '$CT')"
fi

# /metrics should return text/plain (Prometheus format)
CT=$(curl -s -o /dev/null -w "%{content_type}" "$API_URL/metrics")
if echo "$CT" | grep -qi "text/plain"; then
    pass "GET /metrics Content-Type contains text/plain"
else
    # prometheus client_golang may return text/plain or application/openmetrics-text
    if echo "$CT" | grep -qi "openmetrics"; then
        pass "GET /metrics Content-Type is openmetrics (acceptable)"
    else
        fail "GET /metrics Content-Type is text/plain (got '$CT')"
    fi
fi

echo ""

# ============================================================
# Summary
# ============================================================
echo "============================================"
echo -e " Results: ${GREEN}$PASSED${NC}/$TOTAL passed, ${RED}$FAILED${NC} failed"
echo "============================================"

if [ "$FAILED" -gt 0 ]; then
    exit 1
fi
