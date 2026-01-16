#!/bin/bash
# Easy wrapper to run protocol comparison tests

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${GREEN}[INFO]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }
log_section() { echo -e "\n${BLUE}========== $1 ==========${NC}\n"; }

# Check if local server is running
check_local_server() {
    log_info "Checking if local SesameFS server is running..."

    if curl -s http://localhost:8080/api2/server-info/ > /dev/null 2>&1; then
        log_info "Local server is running ✓"
        return 0
    else
        log_error "Local server is NOT running at http://localhost:8080"
        log_info "Start it with: go run cmd/sesamefs/main.go"
        return 1
    fi
}

# Build container
build_container() {
    log_section "Building Container"
    docker-compose build seafile-compare
}

# Run comparison tests
run_comparison() {
    log_section "Running Protocol Comparison"

    # Ensure captures directory exists and is writable
    mkdir -p captures
    chmod 777 captures

    # Run tests
    docker-compose run --rm seafile-compare python3 /usr/local/bin/compare_protocol.py --test all

    # Show results
    if [ -d captures ]; then
        latest_session=$(ls -td captures/comparison_* 2>/dev/null | head -1)
        if [ -n "$latest_session" ]; then
            log_section "Results"
            log_info "Capture directory: $latest_session"

            if [ -f "$latest_session/COMPARISON_REPORT.md" ]; then
                echo ""
                echo "========================================="
                echo "COMPARISON REPORT PREVIEW"
                echo "========================================="
                head -100 "$latest_session/COMPARISON_REPORT.md"
                echo ""
                echo "..."
                echo ""
                log_info "Full report: $latest_session/COMPARISON_REPORT.md"
            fi

            if [ -f "$latest_session/diffs.json" ]; then
                diff_count=$(jq '. | length' "$latest_session/diffs.json")
                if [ "$diff_count" -eq 0 ]; then
                    log_info "✓ No differences found! Protocols match perfectly."
                else
                    log_error "✗ Found $diff_count differences"
                    log_info "Review: $latest_session/COMPARISON_REPORT.md"
                fi
            fi
        fi
    fi
}

# Capture single operation
capture_operation() {
    operation=$1
    log_section "Capturing: $operation"

    docker-compose run --rm seafile-compare seaf-debug.sh "$operation"
}

# Interactive shell in container
run_shell() {
    log_section "Starting Interactive Shell"
    docker-compose run --rm seafile-compare /bin/bash
}

# Show help
show_help() {
    cat <<EOF
Seafile Protocol Comparison Tool

Usage: $0 <command>

Commands:
    compare         Run full protocol comparison (default)
    build           Build the Docker container
    shell           Start interactive shell in container
    capture <op>    Capture single operation (e.g., capture-commits)
    clean           Clean up old captures

Examples:
    # Run full comparison (builds if needed)
    $0 compare

    # Build container only
    $0 build

    # Interactive debugging
    $0 shell

    # Capture specific operation
    $0 capture capture-pack-fs

    # Clean old captures
    $0 clean

Environment Variables:
    REMOTE_SERVER   Remote server URL (default: https://app.nihaoconsult.com)
    LOCAL_SERVER    Local server URL (default: http://localhost:8080)
    REMOTE_USER     Remote username
    REMOTE_PASS     Remote password
    LOCAL_USER      Local username
    LOCAL_PASS      Local password

Prerequisites:
    - Docker and docker-compose installed
    - Local SesameFS server running on http://localhost:8080
    - Remote server credentials configured

EOF
}

# Clean old captures
clean_captures() {
    log_section "Cleaning Old Captures"

    if [ -d captures ]; then
        count=$(find captures -maxdepth 1 -type d -name "comparison_*" -o -name "session_*" | wc -l)
        if [ "$count" -gt 0 ]; then
            log_info "Found $count capture sessions"
            read -p "Delete all? (y/N) " -n 1 -r
            echo
            if [[ $REPLY =~ ^[Yy]$ ]]; then
                find captures -maxdepth 1 -type d \( -name "comparison_*" -o -name "session_*" \) -exec rm -rf {} +
                log_info "Cleaned up old captures"
            fi
        else
            log_info "No old captures found"
        fi
    fi
}

# Main
case "${1:-compare}" in
    compare)
        check_local_server || exit 1
        build_container
        run_comparison
        ;;

    build)
        build_container
        ;;

    shell)
        build_container
        run_shell
        ;;

    capture)
        if [ -z "$2" ]; then
            log_error "Specify operation to capture (e.g., capture-commits)"
            exit 1
        fi
        build_container
        capture_operation "$2"
        ;;

    clean)
        clean_captures
        ;;

    help|--help|-h)
        show_help
        ;;

    *)
        log_error "Unknown command: $1"
        show_help
        exit 1
        ;;
esac
