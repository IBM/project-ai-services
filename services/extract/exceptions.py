"""
Custom exceptions for the extract service.
"""


class SchemaValidationError(Exception):
    """Raised when a submitted schema fails any validation check."""
    pass
