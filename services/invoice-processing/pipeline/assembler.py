"""
Invoice assembler: normalizes raw extraction into canonical staging format.
"""

from typing import Any


def assemble_invoice(*args: Any, **kwargs: Any) -> Any:
    """Normalize raw extraction output into AP_INVOICES_ALL and AP_INVOICE_LINES_ALL structures."""
    raise NotImplementedError("assemble_invoice is not implemented in boilerplate scaffold.")
