#!/bin/bash
# Queue Poller Script for Jenkins PR Preview Pipeline
# This script monitors the Jenkins queue and manages duplicate builds based on PR number and commit SHA
# 
# Logic:
# 1. If both PR# and CommitSHA match a queued job -> Remove the duplicate from queue (exact duplicate)
# 2. If PR# matches but CommitSHA differs -> Stop current job (newer commit for same PR)
# 3. Otherwise -> Continue monitoring
#
# Exit Codes:
#   0 - Normal completion (job finished or stopped intentionally)
#   1 - Error occurred
#   2 - Timeout reached
#
# Changes:
# - Jenkinsfile now continuously monitors this poller's health (PID-based)
# - If this script exits unexpectedly, the Jenkins job will detect it and stop
# - This prevents duplicate builds from queuing when the poller is not running
# - Added more detailed error logging for better diagnostics
#

set -euo pipefail

# Parse command line arguments
readonly JENKINS_URL="${1%/}"
readonly JENKINS_JOB_PARAM="$2"
readonly JENKINS_JOB_URL="$3"
readonly JENKINS_USERNAME="$4"
readonly JENKINS_API_TOKEN="$5"
readonly POLLER_TIMEOUT_MINS="$6"
readonly JOB_NAME="$7"
readonly COMMIT_SHA="$8"
readonly REPO_URL="${9:-}"

# Jenkins credentials
readonly JENKINS_CREDENTIALS="$JENKINS_USERNAME:$JENKINS_API_TOKEN"

# Logging configuration
readonly LOG_PREFIX="[Queue Poller]"

# Validate required parameters
validate_parameters() {
    local errors=0
    
    if [[ -z "$JENKINS_URL" ]]; then
        echo "${LOG_PREFIX} ERROR: JENKINS_URL is required" >&2
        ((errors++))
    fi
    
    if [[ -z "$JENKINS_JOB_PARAM" ]]; then
        echo "${LOG_PREFIX} ERROR: JENKINS_JOB_PARAM is required" >&2
        ((errors++))
    fi
    
    if [[ -z "$JENKINS_JOB_URL" ]]; then
        echo "${LOG_PREFIX} ERROR: JENKINS_JOB_URL is required" >&2
        ((errors++))
    fi
    
    if [[ -z "$JENKINS_USERNAME" ]]; then
        echo "${LOG_PREFIX} ERROR: JENKINS_USERNAME is required" >&2
        ((errors++))
    fi
    
    if [[ -z "$JENKINS_API_TOKEN" ]]; then
        echo "${LOG_PREFIX} ERROR: JENKINS_API_TOKEN is required" >&2
        ((errors++))
    fi
    
    if [[ -z "$POLLER_TIMEOUT_MINS" ]] || ! [[ "$POLLER_TIMEOUT_MINS" =~ ^[0-9]+$ ]]; then
        echo "${LOG_PREFIX} ERROR: POLLER_TIMEOUT_MINS must be a positive integer" >&2
        ((errors++))
    fi
    
    if [[ -z "$JOB_NAME" ]]; then
        echo "${LOG_PREFIX} ERROR: JOB_NAME is required" >&2
        ((errors++))
    fi
    
    if [[ -z "$COMMIT_SHA" ]]; then
        echo "${LOG_PREFIX} ERROR: COMMIT_SHA is required (must be provided by Jenkinsfile)" >&2
        ((errors++))
    fi
    
    if [[ -z "$REPO_URL" ]]; then
        echo "${LOG_PREFIX} ERROR: REPO_URL is required for resolving commit SHAs" >&2
        ((errors++))
    fi
    
    if [[ $errors -gt 0 ]]; then
        echo "${LOG_PREFIX} ERROR: $errors validation error(s) found. Exiting." >&2
        echo "${LOG_PREFIX} NOTE: Jenkins job will detect this exit and stop to prevent duplicate builds." >&2
        exit 1
    fi
}

# Log informational message
log_info() {
    echo "${LOG_PREFIX} INFO: $*" >&2
}

# Log debug message
log_debug() {
    echo "${LOG_PREFIX} DEBUG: $*" >&2
}

# Log warning message
log_warn() {
    echo "${LOG_PREFIX} WARN: $*" >&2
}

# Log error message
log_error() {
    echo "${LOG_PREFIX} ERROR: $*" >&2
}

# Function: Check the status of the current running job
# Returns: "true" if job is in progress, "false" otherwise
get_job_status() {
    local response
    local in_progress
    
    if ! response=$(curl -fsS -u "$JENKINS_CREDENTIALS" "$JENKINS_JOB_URL/api/json" 2>&1); then
        log_error "Failed to get job status: $response"
        return 1
    fi
    
    if ! in_progress=$(echo "$response" | jq -r ".inProgress" 2>&1); then
        log_error "Failed to parse job status JSON: $in_progress"
        return 1
    fi
    
    echo "$in_progress"
}

# Function: Extract PR number from job parameters string
# Parameters: $1 - job parameters string (e.g., "CHECKOUT=123")
# Returns: PR number or empty string (only if CHECKOUT is purely numeric, not a SHA)
extract_pr_number() {
    local params="$1"
    local pr_number=""
    
    # Extract CHECKOUT value first
    local checkout_value=""
    if [[ "$params" =~ CHECKOUT=([^[:space:]]+) ]]; then
        checkout_value="${BASH_REMATCH[1]}"
    fi
    
    # Only return it if it's purely numeric (PR number), not a hex SHA
    # PR numbers are digits only, SHAs contain hex characters (a-f)
    if [[ "$checkout_value" =~ ^[0-9]+$ ]]; then
        pr_number="$checkout_value"
    fi
    
    echo "$pr_number"
}

# Function: Extract CHECKOUT value from job parameters string
# Parameters: $1 - job parameters string
# Returns: CHECKOUT value (PR number or commit hash)
extract_checkout_value() {
    local params="$1"
    local checkout_value=""
    
    # Extract CHECKOUT parameter value
    if [[ "$params" =~ CHECKOUT=([^[:space:]]+) ]]; then
        checkout_value="${BASH_REMATCH[1]}"
    fi
    
    echo "$checkout_value"
}

# Function: Resolve PR number from commit SHA
# Parameters: $1 - commit SHA
# Returns: PR number or empty string if not found
resolve_pr_from_sha() {
    local sha="$1"
    local pr_num=""
    
    if [[ -z "$sha" ]]; then
        return 0
    fi
    
    log_debug "Resolving PR number for SHA: $sha"
    
    # List all PR refs and find the one matching this SHA
    local git_output
    local git_exit_code=0
    
    git_output=$(cd /tmp && git ls-remote "$REPO_URL" 'refs/pull/*/head' 2>&1) || git_exit_code=$?
    
    if [[ $git_exit_code -ne 0 ]]; then
        log_error "git ls-remote failed when resolving PR from SHA"
        log_error "Git output: $git_output"
        return 0
    fi
    
    # Find the PR that matches this SHA
    # Format: <sha> refs/pull/<pr_num>/head
    # Use case-insensitive grep and match the full SHA
    pr_num=$(echo "$git_output" | grep -i "^${sha}" | head -n1 | sed -n 's|.*refs/pull/\([0-9]*\)/head|\1|p')
    
    if [[ -n "$pr_num" ]]; then
        log_debug "Resolved SHA to PR #$pr_num"
    else
        log_debug "Could not resolve SHA to any PR (may be a branch commit)"
    fi
    
    echo "$pr_num"
}

# Function: Resolve commit SHA from CHECKOUT value (PR number or commit hash)
# Parameters: $1 - checkout value (PR number or commit hash)
# Returns: Full commit SHA or empty string on error
resolve_commit_sha_from_checkout() {
    local checkout_value="$1"
    local sha=""
    
    if [[ -z "$checkout_value" ]]; then
        return 0
    fi
    
    log_debug "Resolving commit SHA for CHECKOUT=$checkout_value"
    
    # Case 1: PR number (digits only)
    if [[ "$checkout_value" =~ ^[0-9]+$ ]]; then
        local pr_num="$checkout_value"
        local git_output
        local git_exit_code=0
        
        # Change to a stable directory before running git (workspace may be deleted)
        # Capture both stdout and stderr, and the exit code
        git_output=$(cd /tmp && git ls-remote "$REPO_URL" "refs/pull/${pr_num}/head" 2>&1) || git_exit_code=$?
        
        if [[ $git_exit_code -ne 0 ]]; then
            log_error "git ls-remote failed with exit code $git_exit_code for PR #$pr_num"
            log_error "Git command: git ls-remote $REPO_URL refs/pull/${pr_num}/head"
            log_error "Git output: $git_output"
            # Return empty string but don't exit - let caller handle it
            echo ""
            return 0
        fi
        
        sha=$(echo "$git_output" | awk '{print $1}')
        
        if [[ -n "$sha" ]]; then
            log_debug "Resolved PR #$pr_num to SHA: $sha"
        else
            log_warn "Could not resolve PR #$pr_num to commit SHA"
            log_warn "Git output was: $git_output"
        fi
    # Case 2: Looks like a SHA (short or full)
    elif [[ "$checkout_value" =~ ^[0-9a-fA-F]{7,40}$ ]]; then
        local git_output
        local git_exit_code=0
        
        # Change to a stable directory before running git (workspace may be deleted)
        # Try to expand to full SHA
        git_output=$(cd /tmp && git ls-remote "$REPO_URL" 2>&1) || git_exit_code=$?
        
        if [[ $git_exit_code -ne 0 ]]; then
            log_error "git ls-remote failed with exit code $git_exit_code"
            log_error "Git command: git ls-remote $REPO_URL"
            log_error "Git output: $git_output"
            # Use the provided value as-is
            sha="$checkout_value"
            log_debug "Using provided SHA as-is due to git error: $sha"
        else
            sha=$(echo "$git_output" | awk '{print $1}' | grep -i "^${checkout_value}" | head -n1 || echo "")
            
            if [[ -n "$sha" ]]; then
                log_debug "Expanded SHA $checkout_value to: $sha"
            else
                # Use as-is if we can't expand (might still be valid)
                sha="$checkout_value"
                log_debug "Using provided SHA as-is: $sha"
            fi
        fi
    else
        log_warn "CHECKOUT value '$checkout_value' is neither a PR number nor a commit SHA"
    fi
    
    echo "$sha"
}

# Function: Remove a queued job from Jenkins queue
# Parameters: $1 - queue item ID
# Returns: 0 on success, 1 on failure
remove_queued_job() {
    local queue_id="$1"
    local cancel_url="${JENKINS_URL}/queue/cancelItem?id=${queue_id}"
    
    log_info "Attempting to remove queued job with ID: $queue_id"
    
    if curl -fsS -u "$JENKINS_CREDENTIALS" -X POST "$cancel_url" >/dev/null 2>&1; then
        log_info "Successfully removed duplicate queued job (ID: $queue_id)"
        return 0
    else
        log_error "Failed to remove queued job (ID: $queue_id)"
        return 1
    fi
}

# Function: Stop the current running job
# Returns: 0 on success, 1 on failure
stop_current_job() {
    log_info "Stopping current job: $JENKINS_JOB_URL"
    
    if curl -fsS -u "$JENKINS_CREDENTIALS" -X POST "$JENKINS_JOB_URL/stop" >/dev/null 2>&1; then
        log_info "Successfully stopped current job"
        return 0
    else
        log_error "Failed to stop current job"
        return 1
    fi
}

# Function: Check Jenkins queue for duplicate or newer builds
# Returns:
#   "REMOVE_DUPLICATE:<queue_id>" - if exact duplicate found in queue
#   "STOP_CURRENT" - if newer commit for same PR found in queue
#   "CONTINUE" - if no action needed
check_queue_for_duplicates() {
    local queue_list
    local current_checkout
    local current_pr
    
    # Extract CHECKOUT value from current job parameter (could be PR or SHA)
    current_checkout=$(extract_checkout_value "$JENKINS_JOB_PARAM")
    
    if [[ -z "$current_checkout" ]]; then
        log_warn "Could not extract CHECKOUT value from job parameter: $JENKINS_JOB_PARAM"
        echo "CONTINUE"
        return 0
    fi
    
    # Also extract PR number if available (for logging and matching)
    current_pr=$(extract_pr_number "$JENKINS_JOB_PARAM")
    
    log_debug "Current job CHECKOUT: $current_checkout, PR: ${current_pr:-N/A}"
    
    # Fetch Jenkins queue
    if ! queue_list=$(curl -fsS -u "$JENKINS_CREDENTIALS" "$JENKINS_URL/queue/api/json" 2>&1); then
        log_error "Failed to fetch Jenkins queue: $queue_list"
        echo "CONTINUE"
        return 0
    fi
    
    log_debug "Successfully fetched Jenkins queue"
    
    # Process each queued item
    while IFS= read -r queue_item; do
        [[ -z "$queue_item" ]] && continue
        
        # Check if this is our job. If so, decide if it is a duplicate of the current running job
        local queued_job
        queued_job=$(echo "$queue_item" | jq -c "select(.task.name==\"$JOB_NAME\")" 2>/dev/null || echo "")
        
        if [[ -z "$queued_job" ]]; then
            continue
        fi
        
        log_debug "Found queued job for $JOB_NAME"
        
        # Extract job parameters and queue ID
        local job_params
        local queue_id
        job_params=$(echo "$queued_job" | jq -r '.params // ""' 2>/dev/null || echo "")
        queue_id=$(echo "$queued_job" | jq -r '.id // ""' 2>/dev/null || echo "")
        
        log_debug "Queue ID: $queue_id"
        log_debug "Raw job params: '$job_params'"
        
        if [[ -z "$job_params" ]] || [[ -z "$queue_id" ]]; then
            log_debug "Skipping - missing params or queue_id"
            continue
        fi
        
        # Extract CHECKOUT value and PR number from queued job
        local queued_checkout
        local queued_pr
        local queued_sha
        
        queued_checkout=$(extract_checkout_value "$job_params")
        queued_pr=$(extract_pr_number "$job_params")
        
        log_debug "Extracted from queue - CHECKOUT: '$queued_checkout', PR: '${queued_pr:-N/A}'"
        
        # Skip if CHECKOUT value is empty
        if [[ -z "$queued_checkout" ]]; then
            log_debug "Skipping - empty CHECKOUT value"
            continue
        fi
        
        # Resolve commit SHA from the queued job's CHECKOUT parameter
        if [[ -n "$queued_checkout" ]]; then
            queued_sha=$(resolve_commit_sha_from_checkout "$queued_checkout")
        fi
        
        # Determine the actual PR numbers for both jobs (resolve from SHA if needed)
        local current_pr_resolved="$current_pr"
        local queued_pr_resolved="$queued_pr"
        
        # If current job is a SHA, try to resolve it to a PR
        if [[ -z "$current_pr_resolved" ]] && [[ -n "$COMMIT_SHA" ]]; then
            current_pr_resolved=$(resolve_pr_from_sha "$COMMIT_SHA")
            if [[ -n "$current_pr_resolved" ]]; then
                log_debug "Resolved current SHA to PR #$current_pr_resolved"
            fi
        fi
        
        # If queued job is a SHA, try to resolve it to a PR
        if [[ -z "$queued_pr_resolved" ]] && [[ -n "$queued_sha" ]]; then
            queued_pr_resolved=$(resolve_pr_from_sha "$queued_sha")
            if [[ -n "$queued_pr_resolved" ]]; then
                log_debug "Resolved queued SHA to PR #$queued_pr_resolved"
            fi
        fi
        
        log_info "Found queued job (Queue ID: $queue_id)"
        log_info "Current job - CHECKOUT: $current_checkout, SHA: $COMMIT_SHA, PR: ${current_pr_resolved:-N/A}"
        log_info "Queued job  - CHECKOUT: $queued_checkout, SHA: $queued_sha, PR: ${queued_pr_resolved:-N/A}"
        
        # Determine if we should compare these jobs
        local should_compare=false
        
        if [[ -n "$current_pr_resolved" ]] && [[ -n "$queued_pr_resolved" ]]; then
            # Both resolved to PRs - only compare if same PR
            if [[ "$current_pr_resolved" == "$queued_pr_resolved" ]]; then
                should_compare=true
                log_debug "Both jobs are for PR #$current_pr_resolved - will compare SHAs"
            else
                log_debug "Different PRs (current: #$current_pr_resolved, queued: #$queued_pr_resolved) - skipping"
                continue
            fi
        elif [[ -z "$current_pr_resolved" ]] && [[ -z "$queued_pr_resolved" ]]; then
            # Neither resolved to a PR - these are direct branch commits (e.g., main branch)
            # Compare them to detect duplicates (same SHA) or different commits
            should_compare=true
            log_debug "Both jobs are branch commits (not PR-based) - will compare SHAs"
        else
            # One is PR-based, one is not - skip comparison
            log_debug "One job is PR-based, other is not - skipping comparison"
            continue
        fi
        
        if [[ "$should_compare" == "false" ]]; then
            continue
        fi
        
        # Case 1: Both commit SHAs match - exact duplicate
        if [[ -n "$queued_sha" ]] && [[ "$queued_sha" == "$COMMIT_SHA" ]]; then
            log_info "Exact duplicate detected (same commit SHA for PR #$current_pr_resolved)"
            echo "REMOVE_DUPLICATE:$queue_id"
            return 0
        fi
        
        # Case 2: Same PR but different SHAs - newer commit for same PR
        if [[ -n "$queued_sha" ]] && [[ "$queued_sha" != "$COMMIT_SHA" ]]; then
            log_info "Newer commit detected for PR #$current_pr_resolved (different SHA)"
            echo "STOP_CURRENT"
            return 0
        fi
        
        # Case 3: Couldn't resolve SHA - treat as potential newer commit
        if [[ -z "$queued_sha" ]]; then
            log_warn "Could not resolve commit SHA for queued job"
            log_warn "Treating as potential newer commit to be safe"
            echo "STOP_CURRENT"
            return 0
        fi
    done < <(echo "$queue_list" | jq -c '.items[]?' 2>/dev/null || echo "")
    
    echo "CONTINUE"
}

# Main execution
main() {
    log_info "Starting queue poller"
    log_info "Job: $JOB_NAME"
    log_info "Job URL: $JENKINS_JOB_URL"
    log_info "Job Parameter: $JENKINS_JOB_PARAM"
    log_info "Commit SHA: $COMMIT_SHA (provided by Jenkinsfile)"
    log_info "Repository URL: $REPO_URL"
    log_info "Timeout: $POLLER_TIMEOUT_MINS minutes"
    log_info "Health Monitoring: Jenkins job will monitor this process (PID: $$)"
    
    # Validate all parameters
    validate_parameters
    
    log_info "Parameter validation successful - starting queue monitoring"
    
    # Main polling loop
    for ((iteration=1; iteration<=POLLER_TIMEOUT_MINS; iteration++)); do
        log_info "Polling iteration $iteration/$POLLER_TIMEOUT_MINS"
        
        # Check if current job is still running
        local job_status
        if ! job_status=$(get_job_status); then
            log_error "Failed to get job status, continuing..."
            sleep 60
            continue
        fi
        
        if [[ "$job_status" != "true" ]]; then
            log_info "Job has completed. Exiting poller."
            exit 0
        fi
        
        log_info "Job is currently in progress"
        
        # Check queue for duplicates or newer builds
        local queue_action
        queue_action=$(check_queue_for_duplicates)
        
        case "$queue_action" in
            REMOVE_DUPLICATE:*)
                # Extract queue ID and remove duplicate
                local queue_id="${queue_action#REMOVE_DUPLICATE:}"
                if remove_queued_job "$queue_id"; then
                    log_info "Duplicate removed from queue. Current job continues."
                else
                    log_warn "Failed to remove duplicate, but continuing current job."
                fi
                ;;
                
            STOP_CURRENT)
                # Stop current job to allow newer commit to run
                log_info "Newer commit detected. Stopping current job."
                if stop_current_job; then
                    log_info "Current job stopped successfully. Exiting poller."
                    exit 0
                else
                    log_error "Failed to stop current job. Exiting with error."
                    exit 1
                fi
                ;;
                
            CONTINUE)
                log_info "No duplicate or newer builds found. Continuing monitoring."
                ;;
                
            *)
                log_warn "Unknown queue action: $queue_action. Continuing monitoring."
                ;;
        esac
        
        # Wait before next poll
        sleep 60
    done
    
    # Timeout reached
    log_warn "Queue poller timed out after $POLLER_TIMEOUT_MINS minutes"
    exit 2
}

# Execute main function
main
