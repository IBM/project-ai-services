package common

import (
	"context"
	"fmt"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// JobStatus is the lifecycle state of an async job.
type JobStatus string

const (
	JobStatusAccepted            JobStatus = "accepted"
	JobStatusInProgress          JobStatus = "in_progress"
	JobStatusCompleted           JobStatus = "completed"
	JobStatusCompletedWithErrors JobStatus = "completed_with_errors"
	JobStatusFailed              JobStatus = "failed"
)

// JobDocStatus is the lifecycle state of a single document inside a job.
type JobDocStatus string

const (
	JobDocStatusPending    JobDocStatus = "pending"
	JobDocStatusInProgress JobDocStatus = "in_progress"
	JobDocStatusCompleted  JobDocStatus = "completed"
	JobDocStatusFailed     JobDocStatus = "failed"
)

// JobPollConfig controls how WaitForJob polls a job.
type JobPollConfig struct {
	Timeout   time.Duration
	Interval  time.Duration
	LogPrefix string
}

// JobState is the part of a job response that WaitForJob needs to decide whether to keep polling.
type JobState struct {
	Status JobStatus
	Error  *string
}

// classifyJobState reports whether polling is finished and the error, if any, for the given state.
func classifyJobState(state JobState) (bool, error) {
	switch state.Status {
	case JobStatusCompleted, JobStatusCompletedWithErrors:
		return true, nil
	case JobStatusFailed:
		errMsg := "unknown error"
		if state.Error != nil {
			errMsg = *state.Error
		}

		return true, fmt.Errorf("job failed: %s", errMsg)
	case JobStatusAccepted, JobStatusInProgress:
		return false, nil
	default:
		return true, fmt.Errorf("unexpected job status: %s", state.Status)
	}
}

// WaitForJob polls fetch until the job reaches a terminal state, ctx is cancelled or the timeout elapses.
// stateOf extracts the status and error message from the fetched response.
func WaitForJob[T any](
	ctx context.Context,
	jobID string,
	cfg JobPollConfig,
	fetch func(context.Context) (*T, error),
	stateOf func(*T) JobState,
) (*T, error) {
	deadline := time.Now().Add(cfg.Timeout)
	ticker := time.NewTicker(cfg.Interval)

	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("timed out waiting for job %s", jobID)
			}

			job, err := fetch(ctx)
			if err != nil {
				logger.Warningf("[%s] get job detail failed: %v", cfg.LogPrefix, err)

				continue
			}

			state := stateOf(job)
			logger.Infof("[%s] job %s status: %s", cfg.LogPrefix, jobID, state.Status)

			if done, jobErr := classifyJobState(state); done {
				return job, jobErr
			}
		}
	}
}
