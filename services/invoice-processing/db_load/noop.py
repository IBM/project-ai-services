"""
No-operation database loader implementation.
"""

from typing import Any
from common.misc_utils import get_logger
from db_load.base import InvoiceDBLoader

logger = get_logger("db_load.noop")


class NoOpLoader(InvoiceDBLoader):
    """No-op loader used when db loading is disabled or unconfigured."""

    def load(
        self,
        job_id: str,
        header: dict[str, Any],
        lines: list[dict[str, Any]],
    ) -> str:
        logger.info(f"NoOpLoader called for job {job_id} — skipping database load")
        return "NOOP"

    def is_configured(self) -> bool:
        return True
