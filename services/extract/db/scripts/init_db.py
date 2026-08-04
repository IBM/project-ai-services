#!/usr/bin/env python3
"""
Database initialization script for the extract service.

Delegates to the common init_db main, providing the extract-specific
schema file path and expected tables.
"""

from pathlib import Path
from common.db.scripts.init_db import main as common_main

SCRIPT_DIR = Path(__file__).parent
SCHEMA_FILE = SCRIPT_DIR / "init_schema.sql"

EXPECTED_TABLES = {"schemas", "extract_jobs"}


if __name__ == "__main__":
    common_main(SCHEMA_FILE, EXPECTED_TABLES)

# Made with Bob
