"""
connector/scanner_factory.py — factory that maps connector type to scanner.

Usage
-----
    from digitize.connectors.scanners.scanner_factory import build_scanner

    scanner = build_scanner(connector_row)   # connector_row from DB
    scanner.connect()
    try:
        all_files = scanner.scan()
    finally:
        scanner.close()

Adding a new scanner type
-------------------------
1. Implement a subclass of BaseScanner in a new module (e.g. ssh_scanner.py).
2. Add the connector type string to the ``_REGISTRY`` dict below.
3. No other code needs to change — the worker calls build_scanner() and
   receives the correct instance.
"""

from __future__ import annotations

from typing import Any

from common.misc_utils import get_logger
from digitize.connectors.scanners.base_scanner import BaseScanner
from digitize.connectors.scanners.config import S3ConnectorConfig, SSHConnectorConfig
from digitize.connectors.scanners.s3_scanner import S3Scanner
from digitize.connectors.scanners.ssh_scanner import SSHScanner

logger = get_logger("scanner_factory")

# ---------------------------------------------------------------------------
# Registry — maps connector type → (scanner class, config class)
# ---------------------------------------------------------------------------
_REGISTRY: dict[str, tuple[type[BaseScanner], type]] = {
    "s3": (S3Scanner, S3ConnectorConfig),
    "ssh": (SSHScanner, SSHConnectorConfig),
}


def build_scanner(connector_row: Any) -> BaseScanner:
    """
    Construct and return the correct scanner for ``connector_row``.

    Parameters
    ----------
    connector_row:
        Any object (ORM model, dataclass, or dict) that exposes:
          - ``.type``               → str  (e.g. ``"s3"``, ``"ssh"``)
          - ``.connection_details`` → dict  (decrypted; type-specific fields)
          - ``.allowed_extensions`` → list[str]  (e.g. ``[".pdf", ".docx"]``)

        A plain dict with those keys is also accepted.

    Returns
    -------
    BaseScanner
        Fully configured scanner instance ready for connect().

    Raises
    ------
    ValueError
        If the connector type is not registered.
    """
    if isinstance(connector_row, dict):
        connector_type: str = connector_row["type"]
        connection_details: dict = connector_row.get("connection_details", {})
        allowed_extensions: list[str] = connector_row.get(
            "allowed_extensions", [".pdf", ".docx"]
        )
    else:
        connector_type = connector_row.type
        connection_details = connector_row.connection_details or {}
        allowed_extensions = connector_row.allowed_extensions or [".pdf", ".docx"]

    if connector_type not in _REGISTRY:
        supported = sorted(_REGISTRY.keys())
        raise ValueError(
            f"Unknown connector type '{connector_type}'. "
            f"Supported types: {supported}"
        )

    scanner_cls, config_cls = _REGISTRY[connector_type]

    if config_cls is S3ConnectorConfig:
        config = S3ConnectorConfig.from_connection_details(
            connection_details,
            allowed_extensions=allowed_extensions,
        )
    elif config_cls is SSHConnectorConfig:
        config = SSHConnectorConfig.from_connection_details(
            connection_details,
            allowed_extensions=allowed_extensions,
        )
    else:
        # Generic fallback: pass connection_details as kwargs.
        config = config_cls(**connection_details)

    logger.debug(
        f"[scanner_factory] Built {scanner_cls.__name__} "
        f"for connector type '{connector_type}'"
    )
    return scanner_cls(config)
