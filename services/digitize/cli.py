import argparse
import logging
import sys
from pathlib import Path

from common.misc_utils import get_logger, set_log_level, validate_document_file
from common.settings import settings as common_settings

# Setting log level, 1st priority is to the flag received via cli, 2nd priority to the LOG_LEVEL env var.
log_level = logging.INFO

if "debug" in common_settings.app.log_level.lower():
    log_level = logging.DEBUG

# Parse args early to check for debug flag
common_parser = argparse.ArgumentParser(add_help=False)
common_parser.add_argument("--debug", action="store_true", help="Enable debug logging")

parser = argparse.ArgumentParser(
    description="Data Ingestion CLI",
    formatter_class=argparse.RawTextHelpFormatter,
    parents=[common_parser],
)
command_parser = parser.add_subparsers(dest="command", required=True)

ingest_parser = command_parser.add_parser(
    "ingest",
    help="Ingest the DOCs",
    description="Ingest the DOCs into the vector database after processing\n",
    formatter_class=argparse.RawTextHelpFormatter,
    parents=[common_parser],
)
ingest_parser.add_argument("--path", type=str, default="/var/docs", help="Path to the documents that needs to be ingested into the RAG")

command_parser.add_parser(
    "clean-db",
    help="Clean the DB",
    description="Clean the vector database and PostgreSQL metadata\n",
    formatter_class=argparse.RawTextHelpFormatter,
    parents=[common_parser],
)

command_args = parser.parse_args()
if command_args.debug:
    log_level = logging.DEBUG

set_log_level(log_level)

from digitize.cleanup import reset_db
from digitize.db.connection import check_db_connection
from digitize.digitize_utils import generate_uuid, has_active_jobs, initialize_job_state
from digitize.ingest import ingest
from digitize.models import OperationType, OutputFormat

logger = get_logger("Ingest")


def validate_input_directory(base_path: Path) -> list[Path]:
    """Validate the input directory and return all files recursively."""
    if not base_path.exists():
        raise FileNotFoundError(f"Path does not exist: {base_path}")

    if not base_path.is_dir():
        raise NotADirectoryError(f"Path is not a directory: {base_path}")

    all_files = [path for path in base_path.rglob("*") if path.is_file()]
    if not all_files:
        raise ValueError(f"No files provided. Please ensure at least one file exists in: {base_path}")

    return all_files


def validate_files(all_files: list[Path]) -> list[str]:
    """Validate supported document files and return their filenames."""
    filenames: list[str] = []

    for file_path in all_files:
        try:
            with open(file_path, "rb") as file_handle:
                content = file_handle.read(4)
        except Exception as exc:
            raise RuntimeError(f"Failed to read file: {file_path.name}: {exc}") from exc

        try:
            validate_document_file(file_path.name, content)
            filenames.append(file_path.name)
        except ValueError as exc:
            raise ValueError(f"File validation failed: {exc}") from exc

    return filenames


def print_ingestion_stats(converted_pdf_stats: dict) -> None:
    """Print ingestion statistics for processed documents."""
    total_pages = sum(stats["page_count"] for stats in converted_pdf_stats.values())
    if not total_pages:
        # No pages were processed, ingestion must have completed using cached data.
        return

    print("Stats of processed PDFs:")
    max_file_len = max(len(filename) for filename in converted_pdf_stats.keys())
    total_tables = sum(stats["table_count"] for stats in converted_pdf_stats.values())

    header_format = f'| {"PDF":<{max_file_len}} | {"Total Pages":^15} | {"Total Tables":^15} |'
    if logger.isEnabledFor(logging.DEBUG):
        header_format += f' {"Digitizing":^15} | {"Processing":^15} | {"Chunking":^15} |'
    header_format += f' {"Total Time (s)":>15} |'

    print("-" * len(header_format))
    print(header_format)
    print("-" * len(header_format))

    for file_name, file_stats in converted_pdf_stats.items():
        timings = file_stats["timings"]
        pdf_total_time = sum(timings.values())

        if file_stats["page_count"] > 0:
            stats_to_print = (
                f'| {file_name:<{max_file_len}} | {file_stats.get("page_count", 0):^15} | '
                f'{file_stats.get("table_count", 0):^15} |'
            )
            if logger.isEnabledFor(logging.DEBUG):
                stats_to_print += (
                    f' {timings.get("digitizing", 0.0):^15.2f} |'
                    f' {timings.get("processing", 0.0):^15.2f} |'
                    f' {timings.get("chunking", 0.0):^15.2f} |'
                )
            stats_to_print += f" {pdf_total_time:>15.2f} |"
            print(stats_to_print)

    print("-" * len(header_format))
    footer = f'| {"Total":<{max_file_len}} | {total_pages:^15} | {total_tables:^15} |'
    print(footer)
    print("-" * len(footer))


def run_ingest() -> int:
    """Run CLI ingestion with database validation and job initialization."""
    if not check_db_connection():
        logger.error("Database connection required but not available. Please check PostgreSQL configuration.")
        return 1

    has_active, active_job_ids = has_active_jobs(operation=OperationType.INGESTION.value)
    if has_active:
        error_msg = "Cannot start ingestion: An ingestion job is already running"
        if active_job_ids:
            error_msg += f" (job_id: {active_job_ids[0]})"
        logger.error(error_msg)
        return 1

    try:
        base_path = Path(command_args.path)
        all_files = validate_input_directory(base_path)
        filenames = validate_files(all_files)
    except Exception as exc:
        logger.error(str(exc))
        return 1

    logger.info(f"All {len(filenames)} file(s) validated successfully")

    job_id = generate_uuid()
    doc_id_dict = initialize_job_state(job_id, OperationType.INGESTION, OutputFormat.JSON, filenames)

    logger.info(f"Generated UUIDs for {len(doc_id_dict)} document(s)")

    converted_pdf_stats = ingest(base_path, job_id, doc_id_dict)
    if converted_pdf_stats is None:
        logger.error("Ingestion failed")
        return 1

    print_ingestion_stats(converted_pdf_stats)
    return 0


def run_clean_db() -> int:
    """Run cleanup with database validation."""
    if not check_db_connection():
        logger.error("Database connection required but not available. Please check PostgreSQL configuration.")
        return 1

    try:
        reset_db()
    except Exception as exc:
        logger.error(f"Database cleanup failed: {exc}", exc_info=True)
        return 1

    return 0


def main() -> int:
    if command_args.command == "ingest":
        return run_ingest()

    if command_args.command == "clean-db":
        return run_clean_db()

    logger.error(f"Unsupported command: {command_args.command}")
    return 1


if __name__ == "__main__":
    sys.exit(main())
