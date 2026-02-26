import logging
import os
import argparse

from common.misc_utils import *

common_parser = argparse.ArgumentParser(add_help=False)
common_parser.add_argument("--debug", action="store_true", help="Enable debug logging")

parser = argparse.ArgumentParser(description="Data Ingestion CLI", formatter_class=argparse.RawTextHelpFormatter, parents=[common_parser])
command_parser = parser.add_subparsers(dest="command", required=True)

ingest_parser = command_parser.add_parser("ingest", help="Ingest the DOCs", description="Ingest the DOCs into Milvus after all the processing\n", formatter_class=argparse.RawTextHelpFormatter, parents=[common_parser])
ingest_parser.add_argument("--path", type=str, default="/var/docs", help="Path to the documents that needs to be ingested into the RAG")

command_parser.add_parser("clean-db", help="Clean the DB", description="Clean the Milvus DB\n", formatter_class=argparse.RawTextHelpFormatter, parents=[common_parser])

# Setting log level, 1st priority is to the flag received via cli, 2nd priority to the LOG_LEVEL env var.
log_level = logging.INFO

env_log_level = os.getenv("LOG_LEVEL", "")
if "debug" in env_log_level.lower():
    log_level = logging.DEBUG

command_args = parser.parse_args()
if command_args.debug:
    log_level = logging.DEBUG

set_log_level(log_level)

from digitize.ingest import ingest
from digitize.cleanup import reset_db

logger = get_logger("Ingest")

def main():
    if command_args.command == "ingest":
        converted_pdf_stats = ingest(command_args.path)

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
            header_format += f" {"Conversion":^{15}} | {"Processing Text":^{15}} | {"Processing Tables":^{17}} | {"Chunking":^{15}} |"
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
                    stats_to_print += f" {timings.get("conversion", 0.0):^{15}.2f} | {timings.get("process_text", 0.0):^{15}.2f} | {timings.get("process_tables", 0.0):^{17}.2f} | {timings.get("chunking", 0.0):^{15}.2f} |"
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
