#!/bin/bash
#
# Release Notes Generator (Bash wrapper)
#
# This is a simple wrapper that calls the Python implementation.
# Use this if you prefer bash scripts.
#
# Usage:
#     ./scripts/generate-release-notes.sh <version>
#
# Example:
#     ./scripts/generate-release-notes.sh v0.2.0
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Check if Python 3 is available
if ! command -v python3 &> /dev/null; then
    echo "Error: python3 is required but not found" >&2
    exit 1
fi

# Call the Python script
exec python3 "${SCRIPT_DIR}/generate_release_notes.py" "$@"

# Made with Bob
