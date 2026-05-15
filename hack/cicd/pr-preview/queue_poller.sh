#!/bin/bash
# Queue Poller Script for Jenkins PR Preview Pipeline
# This script monitors the Jenkins queue and aborts duplicate builds
# to ensure only the latest PR commit is being tested

set -e

# Parse command line arguments
JENKINS_URL="${1%/}"
JENKINS_JOB_PARAM="$2"
JENKINS_JOB_URL="$3"
JENKINS_USERNAME="$4"
JENKINS_API_TOKEN="$5"
POLLER_TIMEOUT_MINS="$6"

# Jenkins job configuration
JOB_NAME="$7"
JENKINS_CREDENTIALS="$JENKINS_USERNAME:$JENKINS_API_TOKEN"

echo "Started polling Jenkins queue to find duplicate $JOB_NAME jobs"
echo "Jenkins job $JENKINS_JOB_URL running with param: $JENKINS_JOB_PARAM"

# Function: Check the status of the current running job
# Returns: "true" if job is in progress, "false" otherwise
get_job_status() {
    local response
    response=$(curl -fsS -u "$JENKINS_CREDENTIALS" "$JENKINS_JOB_URL/api/json")
    local in_progress
    in_progress=$(echo "$response" | jq ".inProgress")
    echo "$in_progress"
}

# Function: Check if a duplicate build with matching parameters is queued
# Returns: "YES" if duplicate found, empty string otherwise
is_duplicate_build_queued() {
    local queue_list
    queue_list=$(curl -u "$JENKINS_CREDENTIALS" -s "$JENKINS_URL/queue/api/json")
    
    echo "$queue_list" | jq -c '.items[]' | while read -r queue_item; do
        local queued_job
        queued_job=$(echo "$queue_item" | jq "select(.task.name==\"$JOB_NAME\")")

        if [ -n "$queued_job" ]; then
            local job_params
            job_params=$(echo "$queued_job" | jq '.params')
            
            # Check if the queued job has the same parameters as current job
            if [[ "$job_params" == *"$JENKINS_JOB_PARAM"* ]]; then
                echo "YES"
                break
            fi
        fi
    done
}

# Main polling loop
# Continuously monitor the pr-preview pipeline job status
# If a new Jenkins job with the same parameters is queued, abort the current job
# This ensures resources are used only for the latest PR commit
for ((iteration=1; iteration<=POLLER_TIMEOUT_MINS; iteration++)); do
    echo "Polling iteration $iteration/$POLLER_TIMEOUT_MINS: Checking status of $JOB_NAME in Jenkins"
    
    job_status=$(get_job_status)
    
    if [[ "$job_status" == "true" ]]; then
        echo "Jenkins $JOB_NAME job is currently in progress"
        
        # Check for duplicate builds in queue
        duplicate_found=$(is_duplicate_build_queued)
        
        if [[ "$duplicate_found" == *"YES"* ]]; then
            echo "Duplicate job detected in queue. Aborting current job: $JENKINS_JOB_URL"
            curl -fsS -u "$JENKINS_CREDENTIALS" -X POST "$JENKINS_JOB_URL/stop"
            exit 0
        fi
        
        # Wait before next poll
        sleep 60s
        continue
    fi
    
    # Job has completed
    echo "Jenkins job $JENKINS_JOB_URL has completed"
    exit 0
done

# Timeout reached
echo "Queue poller for job $JENKINS_JOB_URL timed out after $POLLER_TIMEOUT_MINS minutes"
