"""
Custom exceptions for the digitize service.
"""


class JobCancelledError(Exception):
    """
    Raised inside a pipeline function when the job has been marked as CANCELLED
    in the database. Caught by the background-task wrapper (_run_ingest /
    _run_digitize) to perform clean shutdown without treating the cancellation
    as an error.
    """
    pass


class SyncNotFound(Exception):
    """Raised by dispatch_sync when the connector does not exist."""
    pass


class SyncLocked(Exception):
    """Raised by dispatch_sync when the connector cannot accept a new sync
    (DELETE_PENDING or a cancellation already in progress)."""
    pass


class DeadlineExceededError(Exception):
    """Raised by ``_poll_until`` when the conversion deadline is exceeded."""
    pass
