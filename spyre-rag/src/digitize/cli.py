import logging
import argparse
from pathlib import Path

from common.misc_utils import set_log_level
from common.settings import settings

# Setting log level, 1st priority is to the flag received via cli, 2nd priority to the LOG_LEVEL env var.
log_level = logging.INFO

if "debug" in settings.app.log_level.lower():
    log_level = logging.DEBUG

# Parse args early to check for debug flag
common_parser = argparse.ArgumentParser(add_help=False)
common_parser.add_argument("--debug", action="store_true", help="Enable debug logging")

parser = argparse.ArgumentParser(description="Data Ingestion CLI", formatter_class=argparse.RawTextHelpFormatter, parents=[common_parser])
command_parser = parser.add_subparsers(dest="command", required=True)

ingest_parser = command_parser.add_parser("ingest", help="Ingest the DOCs", description="Ingest the DOCs into Milvus after all the processing\n", formatter_class=argparse.RawTextHelpFormatter, parents=[common_parser])
ingest_parser.add_argument("--path", type=str, default="/var/docs", help="Path to the documents that needs to be ingested into the RAG")

command_parser.add_parser("clean-db", help="Clean the DB", description="Clean the Milvus DB\n", formatter_class=argparse.RawTextHelpFormatter, parents=[common_parser])

command_args = parser.parse_args()
if command_args.debug:
    log_level = logging.DEBUG

set_log_level(log_level)

from digitize.digitize_utils import generate_uuid, initialize_job_state, has_active_jobs
from common.misc_utils import get_logger, validate_pdf_file
from digitize.types import OperationType, OutputFormat
from digitize.ingest import ingest
from digitize.cleanup import reset_db

logger = get_logger("Ingest")

def main():
    if command_args.command == "ingest":
        # Check for active ingestion jobs before proceeding (cross-process coordination)
        has_active, active_job_ids = has_active_jobs(operation=OperationType.INGESTION.value)
        if has_active:
            error_msg = "Cannot start ingestion: An ingestion job is already running"
            if active_job_ids:
                error_msg += f" (job_id: {active_job_ids[0]})"
            logger.error(error_msg)
            return
        
        job_id = generate_uuid()
        base_path = Path(command_args.path)

        # Check if path exists and is a directory
        if not base_path.exists():
            logger.error(f"Path does not exist: {base_path}")
            return

        if not base_path.is_dir():
            logger.error(f"Path is not a directory: {base_path}")
            return

        # Get all files recursively
        all_files = [path for path in base_path.rglob('*') if path.is_file()]

        # Check if any files exist (matching app.py: "No files provided")
        if not all_files or len(all_files) == 0:
            logger.error(f"No files provided. Please ensure at least one file exists in: {base_path}")
            return

        # Validate each file
        filenames = []
        for file_path in all_files:
            # Read only first 4 bytes for validation
            try:
                with open(file_path, 'rb') as f:
                    content = f.read(4)
            except Exception as e:
                logger.error(f"Failed to read file: {file_path.name}: {e}")
                return

            try:
                validate_pdf_file(file_path.name, content)
                filenames.append(file_path.name)
            except ValueError as e:
                logger.error(f"File validation failed: {e}")
                return

        logger.info(f"All {len(filenames)} file(s) validated successfully")

        # Loop through all documents in the path and generate UUIDs
        doc_id_dict = {}
        doc_id_dict = initialize_job_state(job_id, OperationType.INGESTION, OutputFormat.JSON, filenames)

        logger.info(f"Generated UUIDs for {len(doc_id_dict)} document(s)")

        # Pass doc_id_dict as the last argument to ingest
        converted_pdf_stats = ingest(base_path, job_id, doc_id_dict)

        # Check if ingestion failed
        if converted_pdf_stats is None:
            logger.error("Ingestion failed")
            return

        # Print detailed stats
        total_pages = sum(converted_pdf_stats[file]["page_count"] for file in converted_pdf_stats)
        if not total_pages:
            # No pages were processed, ingestion must have done using cached data.
            return
        print("Stats of processed PDFs:")
        max_file_len = max(len(key) for key in converted_pdf_stats.keys())
        total_tables = sum(converted_pdf_stats[file]["table_count"] for file in converted_pdf_stats)
        total_time = 0
        header_format = f"| {"PDF":<{max_file_len}} | {"Total Pages":^{15}} | {"Total Tables":^{15}} |"
        if logger.isEnabledFor(logging.DEBUG):
            header_format += f" {"Conversion":^{15}} | {"Processing":^{15}} | {"Chunking":^{15}} |"
        header_format += f" {"Total Time (s)":>{15}} |"

        print("-" * len(header_format))
        print(header_format)
        print("-" * len(header_format))
        for file in converted_pdf_stats:
            timings = converted_pdf_stats[file]["timings"]
            pdf_total_time = sum(timings.values())
            total_time += pdf_total_time
            if converted_pdf_stats[file]["page_count"] > 0:
                stats_to_print = f"| {file:<{max_file_len}} | {converted_pdf_stats[file].get("page_count", 0):^{15}} | {converted_pdf_stats[file].get("table_count", 0):^{15}} |"
                if logger.isEnabledFor(logging.DEBUG):
                    stats_to_print += f" {timings.get("digitizing", 0.0):^{15}.2f} | {timings.get("processing", 0.0):^{15}.2f} | {timings.get("chunking", 0.0):^{15}.2f} |"
                stats_to_print += f" {pdf_total_time:>{15}.2f} |"
                print(stats_to_print)
        print("-" * len(header_format))
        footer = f"| {"Total":<{max_file_len}} | {total_pages:^{15}} | {total_tables:^{15}} |"
        print(footer)
        print("-" * len(footer))

    elif command_args.command == "clean-db":
        reset_db()

if __name__ == "__main__":
    main()
