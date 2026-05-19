#!/usr/bin/env python3
"""
Migration script to transfer metadata from JSON files to PostgreSQL database.

This script reads existing job and document metadata from JSON files and
migrates them to PostgreSQL using SQLAlchemy ORM with upsert logic.

IMPORTANT: After the database refactoring, this script expects JSON files in:
    - Jobs: {DIGITIZE_CACHE_DIR}/jobs/*_status.json
    - Documents: {DIGITIZE_CACHE_DIR}/docs/*_metadata.json

If your JSON files are in a different location, set the DIGITIZE_CACHE_DIR
environment variable before running this script.

Usage:
    python -m digitize.db.migrate_json_to_postgres [--cleanup] [--dry-run]

Options:
    --cleanup    Delete JSON files after successful migration
    --dry-run    Show what would be migrated without making changes

Environment Variables:
    DIGITIZE_CACHE_DIR    Base directory containing jobs/ and docs/ subdirectories
                          (default: /var/cache)
    POSTGRES_HOST         PostgreSQL host (required)
    POSTGRES_PORT         PostgreSQL port (default: 5432)
    POSTGRES_DB           PostgreSQL database name (required)
    POSTGRES_USER         PostgreSQL user (required)
    POSTGRES_PASSWORD     PostgreSQL password (required)
"""

import json
import os
import sys
from pathlib import Path
from datetime import datetime, timezone
from typing import Tuple
import argparse

from sqlalchemy.exc import SQLAlchemyError

from digitize.settings import settings
from common.misc_utils import get_logger

logger = get_logger("migrate_json_to_postgres")


def parse_iso_timestamp(timestamp_str: str) -> datetime | None:
    """
    Parse ISO 8601 timestamp string to timezone-aware datetime object in UTC.
    
    Args:
        timestamp_str: ISO 8601 formatted timestamp (e.g., "2024-01-01T12:00:00Z")
        
    Returns:
        Timezone-aware datetime object in UTC or None if timestamp_str is empty
    """
    if not timestamp_str:
        return None
    # Replace 'Z' suffix with '+00:00' for proper ISO parsing
    dt = datetime.fromisoformat(timestamp_str.replace('Z', '+00:00'))
    # Ensure timezone-aware and convert to UTC
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=timezone.utc)
    else:
        dt = dt.astimezone(timezone.utc)
    return dt


def validate_document_data(doc_data: dict, json_file: Path) -> bool:
    """
    Validate document JSON data has required fields.

    Args:
        doc_data: Document data dictionary
        json_file: Path to JSON file (for error messages)

    Returns:
        True if valid, False otherwise
    """
    required_fields = ['id', 'name', 'type', 'status', 'output_format', 'submitted_at']
    missing_fields = [field for field in required_fields if field not in doc_data]

    if missing_fields:
        logger.error(f"Document {json_file} missing required fields: {missing_fields}")
        return False

    # Validate enum values
    valid_statuses = ['accepted', 'in_progress', 'digitized', 'processed', 'chunked', 'completed', 'failed']
    if doc_data['status'] not in valid_statuses:
        logger.error(f"Document {json_file} has invalid status: {doc_data['status']}")
        return False

    valid_types = ['ingestion', 'digitization']
    if doc_data['type'] not in valid_types:
        logger.error(f"Document {json_file} has invalid type: {doc_data['type']}")
        return False

    valid_formats = ['txt', 'md', 'json']
    if doc_data['output_format'] not in valid_formats:
        logger.error(f"Document {json_file} has invalid output_format: {doc_data['output_format']}")
        return False

    return True


def migrate_documents(session, docs_dir: Path, migrated_job_ids: set, dry_run: bool = False) -> Tuple[int, int, int, list]:
    """
    Migrate document metadata from JSON files to PostgreSQL.

    Args:
        session: SQLAlchemy session
        docs_dir: Directory containing document metadata JSON files
        migrated_job_ids: Set of job IDs that were successfully migrated
        dry_run: If True, don't actually write to database

    Returns:
        Tuple of (migrated_count, skipped_count, failed_count, failed_files)
    """
    from digitize.db.models import Document

    migrated = 0
    skipped = 0
    failed = 0
    failed_files = []

    if not docs_dir.exists():
        logger.warning(f"Documents directory does not exist: {docs_dir}")
        return migrated, skipped, failed, failed_files

    json_files = list(docs_dir.glob("*_metadata.json"))
    logger.info(f"Found {len(json_files)} document metadata files to migrate")

    for json_file in json_files:
        try:
            with open(json_file, 'r') as f:
                doc_data = json.load(f)

            # Validate document data
            if not validate_document_data(doc_data, json_file):
                logger.error(f"Skipping invalid document: {json_file}")
                failed += 1
                failed_files.append(str(json_file))
                continue

            # Check if document references a job_id that wasn't migrated
            doc_job_id = doc_data.get('job_id')
            if doc_job_id and doc_job_id not in migrated_job_ids:
                logger.warning(
                    f"Skipping document {doc_data['id']} ({doc_data['name']}): "
                    f"references non-existent job_id {doc_job_id}"
                )
                skipped += 1
                continue

            if dry_run:
                logger.info(f"[DRY-RUN] Would migrate document: {doc_data['id']} ({doc_data['name']})")
                migrated += 1
                continue

            # Create Document ORM object
            document = Document(
                doc_id=doc_data['id'],
                job_id=doc_job_id,
                name=doc_data['name'],
                type=doc_data['type'],
                status=doc_data['status'],
                output_format=doc_data['output_format'],
                submitted_at=parse_iso_timestamp(doc_data['submitted_at']),
                completed_at=parse_iso_timestamp(doc_data.get('completed_at')),
                error=doc_data.get('error'),
                doc_metadata=doc_data.get('metadata', {})
            )

            # Use merge for upsert (insert or update)
            session.merge(document)
            migrated += 1

            if migrated % 100 == 0:
                logger.info(f"Migrated {migrated} documents so far...")

        except json.JSONDecodeError as e:
            logger.error(f"Invalid JSON in {json_file}: {e}")
            failed += 1
            failed_files.append(str(json_file))
        except KeyError as e:
            logger.error(f"Missing required field in {json_file}: {e}")
            failed += 1
            failed_files.append(str(json_file))
        except Exception as e:
            logger.error(f"Failed to migrate {json_file}: {e}", exc_info=True)
            failed += 1
            failed_files.append(str(json_file))

    if not dry_run:
        try:
            session.commit()
            logger.info(f"✅ Document migration complete: {migrated} migrated, {skipped} skipped (orphaned), {failed} failed")
        except SQLAlchemyError as e:
            session.rollback()
            logger.error(f"Failed to commit document migration: {e}", exc_info=True)
            raise
    else:
        logger.info(f"[DRY-RUN] Would migrate {migrated} documents, {skipped} would be skipped, {failed} would fail")

    return migrated, skipped, failed, failed_files


def validate_job_data(job_data: dict, json_file: Path) -> bool:
    """
    Validate job JSON data has required fields.

    Args:
        job_data: Job data dictionary
        json_file: Path to JSON file (for error messages)

    Returns:
        True if valid, False otherwise
    """
    required_fields = ['job_id', 'operation', 'status', 'submitted_at']
    missing_fields = [field for field in required_fields if field not in job_data]

    if missing_fields:
        logger.error(f"Job {json_file} missing required fields: {missing_fields}")
        return False

    # Validate enum values
    valid_statuses = ['accepted', 'in_progress', 'completed', 'failed']
    if job_data['status'] not in valid_statuses:
        logger.error(f"Job {json_file} has invalid status: {job_data['status']}")
        return False

    valid_operations = ['ingestion', 'digitization']
    if job_data['operation'] not in valid_operations:
        logger.error(f"Job {json_file} has invalid operation: {job_data['operation']}")
        return False

    return True


def migrate_jobs(session, jobs_dir: Path, dry_run: bool = False) -> Tuple[int, int, list, set]:
    """
    Migrate job status from JSON files to PostgreSQL.

    Args:
        session: SQLAlchemy session
        jobs_dir: Directory containing job status JSON files
        dry_run: If True, don't actually write to database

    Returns:
        Tuple of (migrated_count, failed_count, failed_files, migrated_job_ids)
    """
    from digitize.db.models import Job

    migrated = 0
    failed = 0
    failed_files = []
    migrated_job_ids = set()

    if not jobs_dir.exists():
        logger.warning(f"Jobs directory does not exist: {jobs_dir}")
        return migrated, failed, failed_files, set()

    json_files = list(jobs_dir.glob("*_status.json"))
    logger.info(f"Found {len(json_files)} job status files to migrate")

    for json_file in json_files:
        try:
            with open(json_file, 'r') as f:
                job_data = json.load(f)

            # Validate job data
            if not validate_job_data(job_data, json_file):
                logger.error(f"Skipping invalid job: {json_file}")
                failed += 1
                failed_files.append(str(json_file))
                continue

            if dry_run:
                logger.info(f"[DRY-RUN] Would migrate job: {job_data['job_id']} ({job_data.get('job_name', 'unnamed')})")
                migrated += 1
                continue

            # Create Job ORM object
            job = Job(
                job_id=job_data['job_id'],
                job_name=job_data.get('job_name'),
                operation=job_data['operation'],
                status=job_data['status'],
                submitted_at=parse_iso_timestamp(job_data['submitted_at']),
                completed_at=parse_iso_timestamp(job_data.get('completed_at')),
                error=job_data.get('error'),
                stats=job_data.get('stats', {
                    'total_documents': 0,
                    'completed': 0,
                    'failed': 0,
                    'in_progress': 0
                })
            )

            # Use merge for upsert (insert or update)
            session.merge(job)
            migrated += 1
            migrated_job_ids.add(job_data['job_id'])

            if migrated % 50 == 0:
                logger.info(f"Migrated {migrated} jobs so far...")

        except json.JSONDecodeError as e:
            logger.error(f"Invalid JSON in {json_file}: {e}")
            failed += 1
            failed_files.append(str(json_file))
        except KeyError as e:
            logger.error(f"Missing required field in {json_file}: {e}")
            failed += 1
            failed_files.append(str(json_file))
        except Exception as e:
            logger.error(f"Failed to migrate {json_file}: {e}", exc_info=True)
            failed += 1
            failed_files.append(str(json_file))

    if not dry_run:
        try:
            session.commit()
            logger.info(f"✅ Job migration complete: {migrated} migrated, {failed} failed")
        except SQLAlchemyError as e:
            session.rollback()
            logger.error(f"Failed to commit job migration: {e}", exc_info=True)
            raise
    else:
        logger.info(f"[DRY-RUN] Would migrate {migrated} jobs, {failed} would fail")

    return migrated, failed, failed_files, migrated_job_ids


def cleanup_json_files(jobs_dir: Path, docs_dir: Path, dry_run: bool = False) -> Tuple[int, int]:
    """
    Remove JSON files after successful migration.

    Args:
        jobs_dir: Directory containing job status files
        docs_dir: Directory containing document metadata files
        dry_run: If True, don't actually delete files

    Returns:
        Tuple of (jobs_deleted, docs_deleted)
    """
    jobs_deleted = 0
    docs_deleted = 0

    logger.info("Cleaning up JSON files...")

    # Remove job status files
    if jobs_dir.exists():
        for json_file in jobs_dir.glob("*_status.json"):
            if dry_run:
                logger.info(f"[DRY-RUN] Would delete: {json_file}")
            else:
                json_file.unlink()
                logger.debug(f"Deleted: {json_file}")
            jobs_deleted += 1

    # Remove document metadata files
    if docs_dir.exists():
        for json_file in docs_dir.glob("*_metadata.json"):
            if dry_run:
                logger.info(f"[DRY-RUN] Would delete: {json_file}")
            else:
                json_file.unlink()
                logger.debug(f"Deleted: {json_file}")
            docs_deleted += 1

    if dry_run:
        logger.info(f"[DRY-RUN] Would delete {jobs_deleted} job files and {docs_deleted} document files")
    else:
        logger.info(f"✅ Cleanup complete: {jobs_deleted} job files and {docs_deleted} document files deleted")

    return jobs_deleted, docs_deleted


def main():
    """Main migration entry point."""
    parser = argparse.ArgumentParser(
        description='Migrate metadata from JSON files to PostgreSQL',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Dry run to see what would be migrated
  python -m digitize.db.migrate_json_to_postgres --dry-run

  # Migrate data
  python -m digitize.db.migrate_json_to_postgres

  # Migrate and cleanup JSON files
  python -m digitize.db.migrate_json_to_postgres --cleanup
        """
    )
    parser.add_argument(
        '--cleanup',
        action='store_true',
        help='Delete JSON files after successful migration'
    )
    parser.add_argument(
        '--dry-run',
        action='store_true',
        help='Show what would be migrated without making changes'
    )

    args = parser.parse_args()

    # Check if database connection is configured
    db_host = os.getenv('POSTGRES_HOST')
    if not db_host:
        logger.error("POSTGRES_HOST environment variable not set")
        logger.error("Please configure database connection environment variables:")
        logger.error("  - POSTGRES_HOST")
        logger.error("  - POSTGRES_PORT")
        logger.error("  - POSTGRES_DB")
        logger.error("  - POSTGRES_USER")
        logger.error("  - POSTGRES_PASSWORD")
        sys.exit(1)

    logger.info("=" * 70)
    logger.info("PostgreSQL Migration Utility")
    logger.info("=" * 70)

    if args.dry_run:
        logger.info("🔍 DRY RUN MODE - No changes will be made")

    # Import database components
    try:
        from digitize.db.connection import SessionLocal
    except ImportError as e:
        logger.error(f"Failed to import database components: {e}")
        logger.error("Make sure SQLAlchemy and psycopg2 are installed")
        sys.exit(1)

    # Note: Database schema should already be initialized by the init container
    # before this migration script runs. If schema doesn't exist, the script will fail.
    if not args.dry_run:
        logger.info("Assuming database schema is already initialized by init container...")

    # Get directories from environment variables or use defaults
    # The refactoring removed file-based status management, so we need to get paths from env
    cache_dir = Path(os.getenv('DIGITIZE_CACHE_DIR', settings.digitize.cache_dir))
    jobs_dir = cache_dir / 'jobs'
    docs_dir = cache_dir / 'docs'

    logger.info(f"Cache directory: {cache_dir}")
    logger.info(f"Jobs directory: {jobs_dir}")
    logger.info(f"Docs directory: {docs_dir}")

    # Warn if directories don't exist
    if not jobs_dir.exists():
        logger.warning(f"Jobs directory does not exist: {jobs_dir}")
        logger.warning("If you have JSON files in a different location, set DIGITIZE_CACHE_DIR environment variable")
    if not docs_dir.exists():
        logger.warning(f"Docs directory does not exist: {docs_dir}")
        logger.warning("If you have JSON files in a different location, set DIGITIZE_CACHE_DIR environment variable")

    # Create session
    if not SessionLocal:
        logger.error("SessionLocal not initialized. Database configuration error.")
        sys.exit(1)

    session = SessionLocal()

    try:
        # Migrate jobs first (documents reference jobs via foreign key)
        logger.info("\n" + "=" * 70)
        logger.info("Migrating Jobs")
        logger.info("=" * 70)
        jobs_migrated, jobs_failed, jobs_failed_files, migrated_job_ids = migrate_jobs(
            session, jobs_dir, dry_run=args.dry_run
        )

        # Migrate documents
        logger.info("\n" + "=" * 70)
        logger.info("Migrating Documents")
        logger.info("=" * 70)
        docs_migrated, docs_skipped, docs_failed, docs_failed_files = migrate_documents(
            session, docs_dir, migrated_job_ids, dry_run=args.dry_run
        )

        # Summary
        logger.info("\n" + "=" * 70)
        logger.info("Migration Summary")
        logger.info("=" * 70)
        logger.info(f"Jobs:      {jobs_migrated} migrated, {jobs_failed} failed")
        logger.info(f"Documents: {docs_migrated} migrated, {docs_skipped} skipped (orphaned), {docs_failed} failed")

        if docs_skipped > 0:
            logger.warning(f"\n⚠️  {docs_skipped} document(s) were skipped because they reference non-existent jobs")
            logger.warning("These documents have job_id values that don't have corresponding job JSON files")
            logger.warning("Consider cleaning up these orphaned document JSON files manually")

        if jobs_failed_files or docs_failed_files:
            logger.warning("\nFailed files:")
            for f in jobs_failed_files + docs_failed_files:
                logger.warning(f"  - {f}")

        # Cleanup if requested and migration was successful
        if args.cleanup and not args.dry_run:
            if jobs_failed == 0 and docs_failed == 0:
                logger.info("\n" + "=" * 70)
                logger.info("Cleanup")
                logger.info("=" * 70)
                cleanup_json_files(jobs_dir, docs_dir, dry_run=args.dry_run)
            else:
                logger.warning("\n⚠️  Skipping cleanup due to migration failures")
                logger.warning("Fix the errors and rerun with --cleanup")
        elif args.cleanup and args.dry_run:
            logger.info("\n" + "=" * 70)
            logger.info("Cleanup (Dry Run)")
            logger.info("=" * 70)
            cleanup_json_files(jobs_dir, docs_dir, dry_run=True)
        else:
            logger.info("\n💡 JSON files retained. Use --cleanup flag to remove them after migration.")

        logger.info("\n" + "=" * 70)
        if args.dry_run:
            logger.info("✅ Dry run complete!")
        else:
            logger.info("✅ Migration complete!")
        logger.info("=" * 70)

    except Exception as e:
        logger.error(f"\n❌ Migration failed: {e}", exc_info=True)
        sys.exit(1)
    finally:
        session.close()


if __name__ == "__main__":
    main()

# Made with Bob
