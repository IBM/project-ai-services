"""
Invoice Processing exceptions.
"""


class InvoiceProcessingError(Exception):
    """Base exception for invoice processing errors."""

    def __init__(self, message: str):
        super().__init__(message)
        self.message = message


class InvoiceDBLoadError(InvoiceProcessingError):
    """Raised when target database load fails."""
    pass
