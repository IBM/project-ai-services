#!/bin/bash
# Test script for PR title validation (Conventional Commits)
# Usage: ./test-pr-title.sh "your pr title here"

PR_TITLE="$1"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

if [ -z "$PR_TITLE" ]; then
    echo -e "${YELLOW}Usage: ./test-pr-title.sh 'your pr title here'${NC}"
    echo ""
    echo "Examples:"
    echo "  ./test-pr-title.sh 'feat: add new feature'"
    echo "  ./test-pr-title.sh 'fix(api): resolve memory leak'"
    echo "  ./test-pr-title.sh 'feat!: breaking change'"
    echo ""
    exit 1
fi

echo "Testing PR title: $PR_TITLE"
echo ""

# Test against Conventional Commits pattern
if echo "$PR_TITLE" | grep -qE '^(feat|fix|docs|build|chore|test|perf|ci|refactor|style)(\(.+\))?!?:\s+.+'; then
    echo -e "${GREEN}✅ PR title is VALID!${NC}"
    echo ""
    echo "This PR title follows Conventional Commits specification."
    exit 0
else
    echo -e "${RED}❌ PR title is INVALID!${NC}"
    echo ""
    echo "Format: type(scope): description"
    echo "        type(scope)!: breaking change"
    echo ""
    echo "Valid types:"
    echo "  feat, fix, docs, build, chore, test, perf, ci, refactor, style"
    echo ""
    echo "Examples:"
    echo "  feat: add user authentication"
    echo "  fix(api): resolve memory leak"
    echo "  feat(auth)!: redesign authentication"
    echo "  docs: update installation guide"
    echo ""
    exit 1
fi

# Made with Bob
