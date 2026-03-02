#!/bin/bash
set -e

jenkins_url="${1%/}"
current_job_param="$2"
build_number="$3"
user_name="$4"
api_token="$5"

job_name="pr-preview-pipeline"
creds="$user_name:$api_token"
job_url="$jenkins_url/job/adarsh/job/$job_name/$build_number"

echo "Started polling jenkins queue to find duplicate $job_name job"
echo "Jenkins job $job_url running with param: $current_job_param"

# Check the status of current job which is running
get_job_status() {
    resp=$(curl -fsS -u "$creds" "$job_url/api/json")
    progress=$(echo $resp | jq ".inProgress" )
    echo $progress
}

# Check if build is in queue, with matching parameter with current build.
is_duplicate_build_queued() {
    queue_list=$(curl -u "$creds" -s "$jenkins_url/queue/api/json")
    echo $queue_list | jq -c '.items[]' | while read i; do
        queued_job=$(echo "$i" | jq "select(.task.name==\"$job_name\")")

        if [ -n "$queued_job" ]; then
            job_param=$(echo $queued_job | jq '.params')
            if [[ "$job_param" == *"$current_job_param"* ]]; then
                echo "YES"
                break
            fi
        fi
    done
}

# Continuously monitor the pr-preview pipeline job status
# If a new Jenkins job with the same parameters is queued, abort the current job.
# This ensures resources are used only for the latest PR commit.
for i in {1..300}; do
    echo "Polling status of $job_name in jenkins."
    status=$(get_job_status)
    if [[ "$status" == "true" ]]; then
        echo "Jenkins $job_name job is in progress."
        is_duplicate_job=$(is_duplicate_build_queued)
        if [[ "$is_duplicate_job" == *"YES"* ]]; then
            echo "Aborting duplicate $job_url job."
            curl -fsS -u "$creds" -X POST "$job_url/stop"
            exit 0
        fi
        sleep "30s"
        continue
    fi
    echo "Jenkins job $job_url is completed."
    exit 0
done
