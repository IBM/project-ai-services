"""
connectors — long-running data-source connector subsystem.

Packages
--------
config.py           — pydantic config models for every connector type
base_scanner.py     — BaseScanner ABC (transport-agnostic interface + verify_integrity)
s3_scanner.py       — S3Scanner (boto3, _HashingWriter inline MD5, S3 ETag integrity check)
scanner_factory.py  — build_scanner() factory function

Design contract
---------------
* Scanners only list + download — they never write to the checksum registry.
* download_to() returns the local hex digest so the caller can verify integrity
  via verify_integrity() without a second file read.
* verify_integrity() lives on BaseScanner (direct equality); S3Scanner overrides
  it to skip the check for multi-part ETags (<hex>-N).
* Dedup classification (skip / cross-connector dup / brand-new) lives in the
  worker's _classify() method (sync_worker.py — not yet implemented in this PR).
* Registry writes (connector_document_checksum) happen in the worker after all
  per-file ingest jobs complete, under the process-wide ingest_lock.
"""

from digitize.connectors.config import S3ConnectorConfig
from digitize.connectors.base_scanner import BaseScanner
from digitize.connectors.s3_scanner import S3Scanner
from digitize.connectors.scanner_factory import build_scanner

__all__ = [
    "S3ConnectorConfig",
    "BaseScanner",
    "S3Scanner",
    "build_scanner",
]
