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
