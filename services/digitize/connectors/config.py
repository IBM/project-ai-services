"""
connector/config.py — pydantic configuration models for data-source connectors.

Each connector type has its own typed config class that:
  - reads from environment variables (pydantic-settings convention)
  - validates required fields
  - exposes computed properties (e.g. derived region, provider detection)

Connector types
---------------
S3ConnectorConfig   — IBM COS and AWS S3 (provider auto-detected from endpoint_url)

SFTP connector config is out of scope for this PR.

Environment variables for S3
-----------------------------
CONNECTOR_S3_ENDPOINT_URL        — Full S3/COS endpoint URL.
                                   AWS S3 : https://s3.<region>.amazonaws.com
                                   IBM COS: https://s3.<region>.cloud-object-storage.appdomain.cloud
                                   Omit for AWS and let boto3 auto-resolve.
CONNECTOR_S3_BUCKET_NAME         — Bucket to sync documents from.
CONNECTOR_S3_ACCESS_KEY_ID       — IAM key ID (AWS) or HMAC key ID (IBM COS).
CONNECTOR_S3_SECRET_ACCESS_KEY   — IAM secret (AWS) or HMAC secret (IBM COS).
CONNECTOR_S3_PREFIX              — Key prefix to scope listing (default: "", = bucket root).
CONNECTOR_S3_DELIMITER           — Listing delimiter (default: "", = recursive).
CONNECTOR_S3_DOWNLOAD_CONCURRENCY — Parallel download threads (default: 4).
CONNECTOR_S3_VERIFY_SSL          — Verify TLS certificate (default: true).
"""

from __future__ import annotations

import re
from typing import Optional

from pydantic import Field, field_validator, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------

_AWS_HOSTNAME_RE = re.compile(r"amazonaws\.com", re.IGNORECASE)
_REGION_FROM_URL_RE = re.compile(
    r"s3[.\-](?P<region>[a-z0-9\-]+)\.(?:amazonaws\.com|cloud-object-storage\.appdomain\.cloud)",
    re.IGNORECASE,
)
_DEFAULT_REGION = "us-east-1"

# IBM COS cross-region aliases extracted from the endpoint URL are not valid
# SigV4 region strings — boto3 will sign with the wrong region and IBM COS
# returns 404.  Map each alias to the canonical SigV4 region value that IBM
# COS accepts for that endpoint.
#
# Cross-region aliases use the first location in the group as the canonical
# signing region (IBM COS documentation):
#   us  → us-south  (Dallas — primary US cross-region PoP)
#   eu  → eu-de     (Frankfurt — primary EU cross-region PoP)
#   ap  → jp-tok    (Tokyo — primary AP cross-region PoP)
_COS_CROSSREGION_ALIAS: dict[str, str] = {
    "us": "us-south",
    "eu": "eu-de",
    "ap": "jp-tok",
}


# ---------------------------------------------------------------------------
# S3 connector configuration
# ---------------------------------------------------------------------------

class S3ConnectorConfig(BaseSettings):
    """
    Configuration for an S3-compatible data-source connector.

    Works for both AWS S3 and IBM COS — provider is auto-detected from
    ``endpoint_url``

    Instances are normally constructed by the connector CRUD API from the
    ``connection_details`` JSON stored in the ``connectors`` DB table.  They
    can also be created from environment variables for local testing (env
    prefix: ``CONNECTOR_S3_``).
    """

    model_config = SettingsConfigDict(
        env_prefix="CONNECTOR_S3_",
        # Accept extra fields from DB-sourced dicts without raising an error.
        extra="ignore",
    )

    bucket_name: str = Field(
        description="S3 / COS bucket to sync documents from.",
    )
    access_key_id: str = Field(
        default="",
        description=(
            "IAM key ID (AWS) or HMAC key ID (IBM COS)."
        ),
    )
    secret_access_key: str = Field(
        default="",
        description=(
            "IAM secret (AWS) or HMAC secret (IBM COS)."
        ),
    )
    endpoint_url: str = Field(
        default="",
        description=(
            "Full S3 endpoint URL pointing to IBM COS or AWS S3 source."
        ),
    )

    # Optional fields
    prefix: str = Field(
        default="",
        description="Key prefix to scope listing.  Empty = bucket root.",
    )
    delimiter: str = Field(
        default="",
        description="Listing delimiter.  Set '/' for non-recursive (immediate children only).",
    )
    download_concurrency: int = Field(
        default=4,
        ge=1,
        description="Number of parallel download threads per sync tick.",
    )
    verify_ssl: bool = Field(
        default=True,
        description="Verify TLS certificates when connecting to the S3 endpoint.",
    )
    allowed_extensions: list[str] = Field(
        default_factory=lambda: [".pdf", ".docx"],
        description="File extensions to include.  Others are silently skipped.",
    )

    # ------------------------------------------------------------------ #
    # Computed properties (not stored — derived at runtime)               #
    # ------------------------------------------------------------------ #

    @property
    def provider(self) -> str:
        """Return ``'aws'`` or ``'cos'`` based on endpoint_url."""
        if not self.endpoint_url or _AWS_HOSTNAME_RE.search(self.endpoint_url):
            return "aws"
        return "cos"

    @property
    def is_aws(self) -> bool:
        return self.provider == "aws"

    @property
    def effective_region(self) -> str:
        """
        Derive the SigV4-valid region from endpoint_url.

        Handles three cases:

        AWS S3 — explicit regional endpoint:
          s3.eu-west-1.amazonaws.com  →  ``eu-west-1``

        IBM COS — direct regional endpoint (already a valid SigV4 region):
          s3.us-south.cloud-object-storage.appdomain.cloud  →  ``us-south``
          s3.eu-de.cloud-object-storage.appdomain.cloud     →  ``eu-de``

        IBM COS — cross-region alias endpoint:
          s3.us.cloud-object-storage.appdomain.cloud  →  ``us-south``
          s3.eu.cloud-object-storage.appdomain.cloud  →  ``eu-de``
          s3.ap.cloud-object-storage.appdomain.cloud  →  ``jp-tok``

          Cross-region aliases are not valid SigV4 region strings; boto3
          would sign with ``us`` and IBM COS would return 404.  The alias
          is resolved to the canonical primary region for that geography.

        Falls back to ``us-east-1`` when no region segment can be extracted.
        """
        if not self.endpoint_url:
            return _DEFAULT_REGION
        m = _REGION_FROM_URL_RE.search(self.endpoint_url)
        if not m:
            return _DEFAULT_REGION
        region = m.group("region")
        # Resolve IBM COS cross-region alias → canonical SigV4 region.
        return _COS_CROSSREGION_ALIAS.get(region, region)

    @field_validator("bucket_name")
    @classmethod
    def _check_bucket(cls, v: str) -> str:
        if not v.strip():
            raise ValueError("bucket_name must not be empty.")
        return v

    @field_validator("endpoint_url")
    @classmethod
    def _check_endpoint_url(cls, v: str) -> str:
        """Reject endpoint_url values that are missing the URL scheme.

        A bare hostname like ``s3.us-east-1.amazonaws.com`` is a common typo
        that passes silently but causes boto3 to produce malformed requests
        (301 from AWS, connection errors from COS).  Require ``https://`` or
        ``http://`` when a value is supplied.
        """
        if v and not v.startswith(("https://", "http://")):
            raise ValueError(
                f"endpoint_url must start with 'https://' or 'http://', got: {v!r}"
            )
        return v

    @field_validator("allowed_extensions")
    @classmethod
    def _check_extensions(cls, v: list[str]) -> list[str]:
        """Reject extension values that are missing the leading dot.

        ``os.path.splitext`` always returns extensions with a leading dot
        (e.g. ``'.pdf'``).  An entry like ``'pdf'`` would silently match
        nothing during listing — fail early with a clear message instead.
        """
        bad = [e for e in v if not e.startswith(".")]
        if bad:
            raise ValueError(
                f"Each allowed_extension must start with '.', got: {bad!r}. "
                f"Use '.pdf' not 'pdf'."
            )
        return [e.lower() for e in v]

    @model_validator(mode="after")
    def _check_credentials(self) -> "S3ConnectorConfig":
        """Require explicit credentials when the provider is IBM COS.

        AWS S3 can resolve credentials from the environment (instance profile,
        ECS task role, ~/.aws/credentials, etc.) so empty strings are valid.
        IBM COS has no ambient credential chain — access_key_id and
        secret_access_key are always required when endpoint_url is a COS host.
        """
        if not self.is_aws:
            if not self.access_key_id.strip():
                raise ValueError(
                    "access_key_id is required for IBM COS connectors."
                )
            if not self.secret_access_key.strip():
                raise ValueError(
                    "secret_access_key is required for IBM COS connectors."
                )
        return self

    @classmethod
    def from_connection_details(
        cls,
        details: dict,
        allowed_extensions: Optional[list[str]] = None,
    ) -> "S3ConnectorConfig":
        """
        Construct from the ``connection_details`` JSONB stored in the DB.

        The DB field uses ``bucket_name``; ``allowed_extensions`` comes from
        the connector row's top-level column.
        """
        payload = dict(details)
        if allowed_extensions is not None:
            payload["allowed_extensions"] = allowed_extensions
        return cls.model_validate(payload)
