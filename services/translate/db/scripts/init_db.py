#!/usr/bin/env python3
"""
Database initialization script for translate service.

Delegates to the common init_db utility, providing the translate-specific
schema file and the set of expected tables.
"""

from pathlib import Path
from common.db.scripts.init_db import main as common_main

SCRIPT_DIR = Path(__file__).parent
SCHEMA_FILE = SCRIPT_DIR / "init_schema.sql"

EXPECTED_TABLES = {"translate_jobs"}


if __name__ == "__main__":
    common_main(SCHEMA_FILE, EXPECTED_TABLES)

# Made with Bob
