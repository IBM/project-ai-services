#!/usr/bin/env python3
"""
Database initialization script for summarize service.

This script uses the common database initialization utilities
and provides the summarize-specific schema file and expected tables.
"""

from pathlib import Path
from common.db.scripts.init_db import main as common_main

# Script directory and schema file path
SCRIPT_DIR = Path(__file__).parent
SCHEMA_FILE = SCRIPT_DIR / 'init_schema.sql'

# Expected tables for summarize service
EXPECTED_TABLES = {'summarize_jobs'}


if __name__ == '__main__':
    common_main(SCHEMA_FILE, EXPECTED_TABLES)

# Made with Bob
