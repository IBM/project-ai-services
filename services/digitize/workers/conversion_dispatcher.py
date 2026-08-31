"""
Conversion dispatcher — round-robin Docling conversion queue.

A single long-running asyncio task (started in app.py lifespan) that
polls the ``conversion_tasks`` table every ``conversion_poll_interval``
seconds and drives at most one task per operation type per tick.

Round-robin pick strategy
--------------------------
On each poll tick the dispatcher alternates between operation types —
it claims one ingestion task and one digitization task per iteration
(subject to semaphore capacity), then loops.  This prevents a large
ingestion batch from starving waiting digitization tasks.

Head-of-line blocking
----------------------
If the oldest queued task for an operation needs more capacity (weight)
than is currently available, the dispatcher skips that operation *and*
reserves those units — it does NOT hand them to the other operation.
This prevents indefinite starvation of large files.

The reservation only applies when the other queue's head is itself a
large task (needed > 1).  A normal task (needed=1) can always fit into
whatever slots remain, so no pre-reservation is needed for it.

  second_reservation = second_needed if second_needed > 1 else 0
  budget_for_first   = available - second_reservation

  first_reservation  = first_needed if first_needed > 1 else 0
  budget_for_second  = available_after_first - first_reservation

Example: first=ING-large(2), second=DIG-normal(1), available=2
  Old (wrong): budget_for_first = max(0, 2-1) = 1  large ING blocked despite having room
  New (fixed): budget_for_first = max(0, 2-0) = 2  large ING dispatched correctly

Example: first=ING-normal(1), second=DIG-large(2), available=2
  budget_for_first = max(0, 2-2) = 0  ING normal held back, both slots preserved for DIG large
"""

import asyncio
from concurrent.futures import ProcessPoolExecutor
from pathlib import Path

from common.misc_utils import get_logger
from digitize.db.manager import db_manager
from digitize.db.models import ConversionTask, ConversionTaskStatus
from digitize.models import OutputFormat
from digitize.parsing.converter import convert_document_format
from digitize.settings import settings
from digitize.workers.conversion_semaphore import conversion_semaphore

logger = get_logger("conversion_dispatcher")

# Round-robin state — alternates each tick when a task was successfully claimed.
_rr_turn: str = "ingestion"

# Process pool — created lazily inside dispatch_loop() so worker processes are
# not spawned on module import (which would affect test collection, CLI tools,
# and any code that transitively imports this module).
_process_pool: ProcessPoolExecutor | None = None


def _other(op: str) -> str:
    return "digitization" if op == "ingestion" else "ingestion"


def _try_claim_if_fits(operation: str, available: int) -> ConversionTask | None:
    """
    Peek at the head of ``operation``'s queue.  If it fits within
    ``available`` semaphore units, atomically claim and return it.
    Otherwise return None (head-of-line blocking — nothing behind it
    is attempted).
    """
    head = db_manager.peek_head(operation)
    if head is None:
        return None  # nothing queued for this type

    needed = 2 if head.is_large else 1
    if needed > available:
        return None  # head can't run yet — hold the line

    return db_manager.claim_head(operation)

async def _run_conversion(task: ConversionTask, weight: int) -> None:
    """
    Execute a single conversion task inside the shared process pool.
    Releases the semaphore unconditionally in the finally block.
    Staged input files are NOT deleted here — each pipeline (_run_ingest,
    _run_digitize, sync_tick batch loop) deletes them via its own
    cleanup_staging_directory() call after the full pipeline finishes.

    Responsibility boundary
    -----------------------
    This function owns only the conversion_tasks row — it writes task status
    (queued → running → completed / failed / cancelled) and result_path / error.
    All job and document status updates (JobStatus, DocStatus) are the
    responsibility of the pipeline layer that polls task.status.

    Cancellation
    ------------
    The pipeline calls ``db_manager.cancel_tasks_for_job`` which sets
    non-terminal tasks to ``cancel_pending``.  This function checks that
    flag at three points:

    1. Before marking the task RUNNING — if already cancel_pending (task was
       cancelled while still queued) we skip the conversion entirely.
    2. Inside the worker process between 100-page chunks — ``task_id`` is passed
       to ``convert_document_format`` which builds a DB-polling cancel check
       callable (``_make_db_cancel_check``) that is invoked between chunks inside
       ``convert_doc``.  When the task's DB row shows ``cancel_pending`` the
       callable returns True, ``JobCancelledError`` is raised in the worker, and
       the ``except`` block below maps it to ``CANCELLED``.
    3. After the process-pool future returns — if the task was set to
       cancel_pending while the conversion was running we write CANCELLED
       instead of COMPLETED so the pipeline's poll loop sees a terminal state
       it can map to job/doc CANCELLED rather than treating the output as valid.
    """
    try:
        cached_path = Path(task.cached_file)
        if not cached_path.exists():
            db_manager.update_task_status(
                task.task_id, ConversionTaskStatus.FAILED,
                error="Cached input file missing at dispatch time",
            )
            logger.warning(f"Task {task.task_id}: cached file missing — marked failed")
            return

        # Check 1: bail out early if the job was cancelled while this task
        # was still in the queue (cancel_pending was set before claim_head).
        fresh = db_manager.get_conversion_task(task.task_id)
        if fresh is not None and fresh.status == ConversionTaskStatus.CANCEL_PENDING:
            db_manager.update_task_status(
                task.task_id, ConversionTaskStatus.CANCELLED,
                error="Job cancelled before conversion started",
            )
            logger.info(f"Task {task.task_id} cancelled before starting (was cancel_pending)")
            return

        # Mark task as running — pipeline layer picks this up via poll.
        db_manager.update_task_status(task.task_id, ConversionTaskStatus.RUNNING)

        # Convert in a child process — CPU-bound, no GIL release.
        # task_id is forwarded so the worker can poll the DB for cancel_pending
        # between 100-page chunks and stop early for large files.
        out_dir = settings.digitize.digitized_docs_dir
        loop = asyncio.get_running_loop()
        result_path, _ = await loop.run_in_executor(
            _process_pool,
            convert_document_format,
            task.cached_file,
            out_dir,
            task.doc_id or task.task_id,
            OutputFormat(task.output_format),
            task.task_id,
        )

        # Check 2: if the job was cancelled while we were converting, write
        # CANCELLED so the pipeline's poll loop reaches the correct terminal state.
        fresh = db_manager.get_conversion_task(task.task_id)
        if fresh is not None and fresh.status == ConversionTaskStatus.CANCEL_PENDING:
            db_manager.update_task_status(
                task.task_id, ConversionTaskStatus.CANCELLED,
                error="Job cancelled during conversion",
            )
            logger.info(f"Task {task.task_id} cancelled after conversion completed")
            return

        db_manager.update_task_status(task.task_id, ConversionTaskStatus.COMPLETED, result_path=result_path)
        logger.info(f"Task {task.task_id} completed → {result_path}")

    except Exception as exc:
        # Re-read the task to distinguish a cancellation-induced exception
        # (e.g. JobCancelledError from convert_doc chunk loop) from a genuine failure.
        fresh = db_manager.get_conversion_task(task.task_id)
        if fresh is not None and fresh.status == ConversionTaskStatus.CANCEL_PENDING:
            db_manager.update_task_status(
                task.task_id, ConversionTaskStatus.CANCELLED,
                error="Job cancelled during conversion",
            )
            logger.info(f"Task {task.task_id} cancelled mid-conversion: {exc}")
        else:
            logger.error(f"Task {task.task_id} failed: {exc}", exc_info=True)
            db_manager.update_task_status(task.task_id, ConversionTaskStatus.FAILED, error=str(exc))

    finally:
        # Do NOT delete task.cached_file here — the pipeline layer owns staging
        # cleanup after all post-conversion processing (page loading, chunking,
        # indexing) has finished.  See _run_ingest / _run_digitize / sync_tick.
        await conversion_semaphore.release(weight)


async def dispatch_loop() -> None:
    """
    Long-running coroutine that polls the DB and dispatches conversion tasks.

    Started once in app.py's lifespan and cancelled on shutdown.
    The process pool is created here (not at module import) so worker
    processes are only spawned when the dispatcher actually starts.
    """
    global _rr_turn, _process_pool

    # Initialise the pool on first entry.  max_workers == semaphore capacity
    # so a worker process is always immediately available when the semaphore
    # grants a slot — no internal queuing in the pool.
    _process_pool = ProcessPoolExecutor(
        max_workers=conversion_semaphore.capacity  # default 4
    )
    logger.info(
        f"Conversion dispatcher started "
        f"(pool workers={conversion_semaphore.capacity})"
    )

    try:
        while True:
            try:
                available = conversion_semaphore.available
                if available > 0:
                    first, second = _rr_turn, _other(_rr_turn)

                    # Peek at both heads up-front so each queue's budget can account
                    # for the other queue's pending weight requirement.
                    first_head  = db_manager.peek_head(first)
                    second_head = db_manager.peek_head(second)
                    first_needed  = (2 if first_head.is_large  else 1) if first_head  else 0
                    second_needed = (2 if second_head.is_large else 1) if second_head else 0

                    # Only reserve capacity for second's head when it is a large task
                    # (needed > 1).  A normal task (needed=1) can always fit into
                    # whatever remains after first runs — pre-reserving for it would
                    # wrongly block a large first task that has exactly enough room now.
                    second_reservation = second_needed if second_needed > 1 else 0
                    budget_for_first = max(0, available - second_reservation)
                    first_task = _try_claim_if_fits(first, budget_for_first)
                    if first_task:
                        weight = 2 if first_task.is_large else 1
                        await conversion_semaphore.acquire(weight)
                        asyncio.create_task(_run_conversion(first_task, weight))
                        queued_counts = db_manager.get_queued_counts()
                        logger.debug(
                            f"Dispatched {first} task {first_task.task_id} "
                            f"(file={Path(first_task.cached_file).name}, weight={weight}, "
                            f"semaphore={conversion_semaphore.available}/{conversion_semaphore.capacity}, "
                            f"queued ingestion={queued_counts['ingestion']}, queued digitization={queued_counts['digitization']})"
                        )

                    # Re-read available after first may have acquired slots.
                    # Using the stale pre-acquire value would give second too generous
                    # a budget on the tick where first finally dispatches (e.g. large
                    # ING acquires weight=2, available drops from 2→0, but stale
                    # available=2 would yield budget_for_second=0 by coincidence only
                    # when first_needed==available; for other values it over-grants).
                    available_after_first = conversion_semaphore.available

                    # Same rule for second: only reserve if first's head is large.
                    first_reservation = first_needed if first_needed > 1 else 0
                    budget_for_second = max(0, available_after_first - first_reservation)
                    second_task = _try_claim_if_fits(second, budget_for_second)
                    if second_task:
                        weight = 2 if second_task.is_large else 1
                        await conversion_semaphore.acquire(weight)
                        asyncio.create_task(_run_conversion(second_task, weight))
                        queued_counts = db_manager.get_queued_counts()
                        logger.debug(
                            f"Dispatched {second} task {second_task.task_id} "
                            f"(file={Path(second_task.cached_file).name}, weight={weight}, "
                            f"semaphore={conversion_semaphore.available}/{conversion_semaphore.capacity}, "
                            f"queued ingestion={queued_counts['ingestion']}, queued digitization={queued_counts['digitization']})"
                        )

                    # Advance turn only when first was successfully claimed.
                    if first_task:
                        _rr_turn = second

                # After each tick, promote pending → queued to backfill quota headroom.
                db_manager.promote_pending("ingestion", settings.digitize.ingestion_queue_quota)
                db_manager.promote_pending("digitization", settings.digitize.digitization_queue_quota)

            except asyncio.CancelledError:
                raise
            except Exception as exc:
                logger.error(f"Dispatcher loop error: {exc}", exc_info=True)
                # Don't crash the loop on transient errors; sleep and retry.

            await asyncio.sleep(settings.digitize.conversion_poll_interval)

    except asyncio.CancelledError:
        logger.info("Conversion dispatcher loop cancelled — shutting down")
        _process_pool.shutdown(wait=False)
        raise
