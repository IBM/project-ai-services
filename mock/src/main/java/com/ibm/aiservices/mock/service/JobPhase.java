package com.ibm.aiservices.mock.service;

/**
 * Coarse lifecycle phase of a simulated asynchronous job.
 * <p>
 * Jobs are not executed; their phase is derived from elapsed time, so clients polling the mock see the same accepted, in progress, finished sequence as with the real services.
 */
public enum JobPhase {

    /** Submitted but not yet started; the first 15 percent of the simulated duration. */
    QUEUED,

    /** Running. */
    RUNNING,

    /** Finished, successfully or not. */
    DONE;

    /** Fraction of the job duration spent in {@link #QUEUED}. */
    private static final double QUEUED_FRACTION = 0.15;

    /**
     * Computes the phase of a job.
     *
     * @param _submittedAt the submission time in epoch milliseconds
     * @param _durationMillis the simulated job duration
     * @param _now the current time in epoch milliseconds
     * @return the phase at {@code _now}
     */
    public static JobPhase of(final long _submittedAt, final long _durationMillis, final long _now) {
        final long elapsed = _now - _submittedAt;
        if (elapsed >= _durationMillis) {
            return DONE;
        }
        if (elapsed < _durationMillis * QUEUED_FRACTION) {
            return QUEUED;
        }
        return RUNNING;
    }

    /**
     * Computes how far a job has progressed.
     *
     * @param _submittedAt the submission time in epoch milliseconds
     * @param _durationMillis the simulated job duration
     * @param _now the current time in epoch milliseconds
     * @return a value between {@code 0.0} and {@code 1.0}
     */
    public static double progress(final long _submittedAt, final long _durationMillis, final long _now) {
        if (_durationMillis <= 0) {
            return 1.0;
        }
        final double fraction = (double) (_now - _submittedAt) / _durationMillis;
        return Math.max(0.0, Math.min(1.0, fraction));
    }
}
