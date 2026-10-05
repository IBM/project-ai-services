"""
Factory for resolving the configured InvoiceDBLoader backend.
"""

from common.misc_utils import get_logger
from db_load.base import InvoiceDBLoader
from db_load.noop import NoOpLoader

logger = get_logger("db_load.factory")


def get_loader() -> InvoiceDBLoader:
    """Return the loader configured via settings.

    Currently only NoOpLoader is wired in. Add concrete backend imports here
    once a target DB is chosen.
    """
    from settings import settings

    if not settings.invoice.db_load_enabled:
        logger.info("db_load_enabled=False — using NoOpLoader")
        return NoOpLoader()

    # No concrete DB backend configured yet — extend here when ready.
    logger.warning("No DB backend configured — falling back to NoOpLoader")
    return NoOpLoader()
