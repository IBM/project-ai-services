"""
Exit codes of the training worker and the error that selects the permanent one.

The API retries a worker that exits with EXIT_FAILURE (up to TRAINING_MAX_ATTEMPTS)
and does not retry one that exits with EXIT_PERMANENT, because the same input would
fail the same way.
"""

EXIT_OK = 0
EXIT_FAILURE = 1  # Anything else went wrong: retry.
# 2 is argparse's exit code for a bad command line.
EXIT_PERMANENT = 3  # A failure that would only repeat: do not retry.


class PermanentFailure(Exception):
    """A failure that a retry cannot fix.

    The connector is missing or cannot be decrypted, the table or a column does not
    exist, the table has no rows or too many, or the data cannot be trained on.
    The message is stored in jobs.message, so it must never hold a secret.
    """
