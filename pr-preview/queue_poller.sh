#!/bin/bash
# Queue Poller Script for Jenkins PR Preview Pipeline
# This script monitors the Jenkins queue and aborts duplicate builds
# to ensure only the latest PR commit is being tested

set -e

# Parse command line arguments
JENKINS_URL="${1%/}"
CURRENT_JOB_PARAM="$2"
BUILD_NUMBER="$3"
JENKINS_USERNAME="$4"
JENKINS_API_TOKEN="$5"
POLLER_TIMEOUT_MINS="$6"

# Jenkins job configuration
JOB_NAME="pr-preview-pipeline"
JENKINS_CREDENTIALS="$JENKINS_USERNAME:$JENKINS_API_TOKEN"
CURRENT_JOB_URL="$JENKINS_URL/job/adarsh/job/$JOB_NAME/$BUILD_NUMBER"

echo "Started polling Jenkins queue to find duplicate $JOB_NAME jobs"
echo "Jenkins job $CURRENT_JOB_URL running with param: $CURRENT_JOB_PARAM"

# Function: Check the status of the current running job
# Returns: "true" if job is in progress, "false" otherwise
get_job_status() {
    local response
    response=$(curl -fsS -u "$JENKINS_CREDENTIALS" "$CURRENT_JOB_URL/api/json")
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
            if [[ "$job_params" == *"$CURRENT_JOB_PARAM"* ]]; then
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
            echo "Duplicate job detected in queue. Aborting current job: $CURRENT_JOB_URL"
            curl -fsS -u "$JENKINS_CREDENTIALS" -X POST "$CURRENT_JOB_URL/stop"
            exit 0
        fi
        
        # Wait before next poll
        sleep 60s
        continue
    fi
    
    # Job has completed
    echo "Jenkins job $CURRENT_JOB_URL has completed"
    exit 0
done

# Timeout reached
echo "Queue poller for job $CURRENT_JOB_URL timed out after $POLLER_TIMEOUT_MINS minutes"
