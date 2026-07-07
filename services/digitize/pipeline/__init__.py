"""
Processing pipeline package.

Entry-point functions called by background tasks; each owns a complete job lifecycle:
  - digitize.py — single-document digitization pipeline
  - ingest.py   — multi-document ingestion + vector-indexing pipeline
  - cleanup.py  — full digitize service reset (VDB + PostgreSQL + filesystem)
"""
