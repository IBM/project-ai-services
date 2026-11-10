"""
CLI tool to test Db2 for i extraction via Mapepire and verify Parquet output.

Usage:
    python -m forecast.data_loader.cli \\
        --host ibmi.example.com \\
        --port 8076 \\
        --user MYUSER \\
        --password MYPASS \\
        --schema CC_DEMO \\
        --table CREDIT_DATA \\
        --output /tmp/extracted.parquet \\
        --insecure
"""
from __future__ import annotations

import argparse
import os
import sys
import time
from pathlib import Path
from typing import List, Optional

import pyarrow.parquet as pq

from forecast.data_loader.config import (
    DEFAULT_LOADER_CHUNK_ROWS,
    DEFAULT_MAPEPIRE_PORT,
    DEFAULT_MAX_TRAINING_ROWS,
)
from forecast.data_loader.db2_client import Db2Client
from forecast.data_loader.loader import Db2DataLoader


def parse_args(argv: Optional[List[str]] = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Extract tables from IBM i Db2 via Mapepire directly into compressed Parquet.",
        formatter_class=argparse.ArgumentDefaultsHelpFormatter,
    )
    parser.add_argument(
        "--host",
        default=os.getenv("DB2I_HOST", os.getenv("MAPEPIRE_HOST", "")),
        help="IBM i host name or IP address",
    )
    parser.add_argument(
        "--port",
        type=int,
        default=int(os.getenv("DB2I_PORT", os.getenv("MAPEPIRE_PORT", str(DEFAULT_MAPEPIRE_PORT)))),
        help="Mapepire daemon port",
    )
    parser.add_argument(
        "--database",
        default=os.getenv("DB2I_DATABASE", "*LOCAL"),
        help="IBM i database name (usually *LOCAL)",
    )
    parser.add_argument(
        "-u",
        "--user",
        default=os.getenv("DB2I_USER", os.getenv("MAPEPIRE_USER", "")),
        help="Database user ID",
    )
    parser.add_argument(
        "-p",
        "--password",
        default=os.getenv("DB2I_PASSWORD", os.getenv("MAPEPIRE_PASSWORD", "")),
        help="Database password",
    )
    parser.add_argument(
        "-s",
        "--schema",
        default=os.getenv("DB2I_SCHEMA", None),
        help="Schema / library name (e.g., CC_DEMO)",
    )
    parser.add_argument(
        "-t",
        "--table",
        default=os.getenv("DB2I_TABLE", ""),
        help="Table name to extract from (e.g., CREDIT_DATA)",
    )
    parser.add_argument(
        "-c",
        "--columns",
        default=None,
        help="Comma-separated column names to extract. If omitted, extracts all columns in the table.",
    )
    parser.add_argument(
        "-o",
        "--output",
        default="/tmp/db2_extracted.parquet",
        help="Destination path for Parquet output file",
    )
    parser.add_argument(
        "-b",
        "--batch-size",
        type=int,
        default=DEFAULT_LOADER_CHUNK_ROWS,
        help="Number of rows fetched per batch from Db2 cursor",
    )
    parser.add_argument(
        "-m",
        "--max-rows",
        type=int,
        default=DEFAULT_MAX_TRAINING_ROWS,
        help="Maximum allowed training rows before aborting",
    )
    parser.add_argument(
        "-w",
        "--where",
        default=None,
        help="Optional SQL filter condition (e.g., \"STATUS = 'ACTIVE'\")",
    )
    parser.add_argument(
        "--insecure",
        "--ignore-unauthorized",
        dest="ignore_unauthorized",
        action="store_true",
        default=os.getenv("MAPEPIRE_IGNORE_UNAUTHORIZED", "false").lower() in ("true", "1", "yes"),
        help="Ignore TLS/SSL certificate validation (useful for self-signed certs)",
    )
    parser.add_argument(
        "--preview",
        type=int,
        default=5,
        help="Number of rows from resulting Parquet file to display",
    )

    args = parser.parse_args(argv)

    if not args.host:
        parser.error("--host is required (or set DB2I_HOST / MAPEPIRE_HOST)")
    if not args.user:
        parser.error("--user is required (or set DB2I_USER / MAPEPIRE_USER)")
    if not args.password:
        parser.error("--password is required (or set DB2I_PASSWORD / MAPEPIRE_PASSWORD)")
    if not args.table:
        parser.error("--table is required (or set DB2I_TABLE)")

    return args


def run(args: argparse.Namespace) -> int:
    output_path = Path(args.output).resolve()
    print("=" * 70)
    print("Forecast Data Loader — IBM i Db2 to Parquet Extraction Test")
    print("=" * 70)
    print(f"Host              : {args.host}:{args.port}")
    print(f"Database          : {args.database}")
    print(f"User              : {args.user}")
    print(f"Schema            : {args.schema or '(default)'}")
    print(f"Table             : {args.table}")
    print(f"Batch Size        : {args.batch_size:,} rows")
    print(f"Max Training Rows : {args.max_rows:,} rows")
    print(f"Ignore TLS Certs  : {args.ignore_unauthorized}")
    print(f"Destination Path  : {output_path}")
    print("-" * 70)

    client = Db2Client(
        host=args.host,
        database=args.database,
        username=args.user,
        password=args.password,
        port=args.port,
        ignore_unauthorized=args.ignore_unauthorized,
    )

    with client:
        # Step 1: Connectivity check
        print("\n[1/4] Testing connection to IBM i Db2 via Mapepire...")
        t0 = time.monotonic()
        client.test_connection()
        t1 = time.monotonic()
        print(f"      Connection test succeeded in {t1 - t0:.3f}s")

        # Step 2: Metadata discovery & column resolution
        print(f"\n[2/4] Inspecting table metadata for '{args.table}'...")
        columns_info = client.list_columns(schema=args.schema, table=args.table)
        print(f"      Found {len(columns_info)} total columns in database:")
        for col in columns_info:
            print(f"        - {col.name:<25} ({col.type:<15}, nullable={col.nullable})")

        if args.columns:
            target_columns = [c.strip() for c in args.columns.split(",") if c.strip()]
        else:
            target_columns = [col.name for col in columns_info]

        print(f"\n      Selected {len(target_columns)} columns for extraction: {target_columns}")

        # Step 3: Stream and write to Parquet
        print(f"\n[3/4] Streaming data rows and writing directly to Parquet...")
        loader = Db2DataLoader(client)
        result = loader.extract_to_parquet(
            schema=args.schema,
            table=args.table,
            columns=target_columns,
            output_path=output_path,
            where_clause=args.where,
            batch_size=args.batch_size,
            max_training_rows=args.max_rows,
        )

        print("\n" + "=" * 70)
        print("Extraction Completed Successfully")
        print("=" * 70)
        print(f"Total Rows Extracted : {result.rows_written:,}")
        print(f"Total Batches Written: {result.batches_count}")
        print(f"Total Duration       : {result.duration_seconds:.2f} seconds")
        if result.duration_seconds > 0:
            throughput = result.rows_written / result.duration_seconds
            print(f"Throughput           : {throughput:,.0f} rows/second")
        print(f"Parquet File Size    : {result.bytes_written / (1024 * 1024):.2f} MB ({result.bytes_written:,} bytes)")
        print(f"Parquet File Path    : {result.output_path}")

    # Step 4: Validate resulting Parquet file
    print(f"\n[4/4] Validating resulting Parquet file on disk...")
    if not output_path.exists():
        print(f"ERROR: Expected Parquet file at {output_path} does not exist!", file=sys.stderr)
        return 1

    table = pq.read_table(output_path)
    print(f"      Verified Parquet Schema : {len(table.schema.names)} columns")
    print(f"      Verified Total Rows     : {table.num_rows:,} rows")

    if args.preview > 0 and table.num_rows > 0:
        print(f"\n--- Preview First {min(args.preview, table.num_rows)} Rows ---")
        df_preview = table.slice(0, args.preview).to_pandas()
        print(df_preview.to_string())

    print("\n" + "=" * 70)
    print("ALL VERIFICATIONS PASSED")
    print("=" * 70)
    return 0


def main() -> None:
    try:
        args = parse_args()
        sys.exit(run(args))
    except Exception as exc:
        print(f"\nExtraction failed with error: {exc}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
