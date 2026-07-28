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
"""

import asyncio
import shutil
from concurrent.futures import ProcessPoolExecutor
from pathlib import Path

from common.misc_utils import get_logger
from digitize.db.manager import db_manager
from digitize.db.models import ConversionTask
from digitize.models import OutputFormat
from digitize.parsing.converter import convert_document_format
from digitize.settings import settings
from digitize.workers.conversion_semaphore import conversion_semaphore

logger = get_logger("conversion_dispatcher")

# Round-robin state — alternates each tick when a task was successfully claimed.
_rr_turn: str = "ingestion"

# One shared process pool for all conversions.
# max_workers == semaphore capacity so a worker process is always immediately
# available when the semaphore grants a slot — no internal queuing in the pool.
_process_pool = ProcessPoolExecutor(
    max_workers=conversion_semaphore.capacity  # default 4
)


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
    """
    from digitize.utils.db import get_status_manager
    from digitize.models import DocStatus, JobStatus

    try:
        cached_path = Path(task.cached_file)
        if not cached_path.exists():
            db_manager.update_task_status(
                task.task_id, "failed",
                error="Cached input file missing at dispatch time",
            )
            logger.warning(f"Task {task.task_id}: cached file missing — marked failed")
            return

        # Mark task and associated job/doc as running / in_progress
        db_manager.update_task_status(task.task_id, "running")
        if task.job_id and task.doc_id:
            status_mgr = get_status_manager(task.job_id)
            status_mgr.update_doc_metadata(task.doc_id, {"status": DocStatus.IN_PROGRESS})
            status_mgr.update_job_progress(task.doc_id, DocStatus.IN_PROGRESS, JobStatus.IN_PROGRESS)

        # Convert in a child process — CPU-bound, no GIL release
        out_dir = settings.digitize.digitized_docs_dir
        loop = asyncio.get_running_loop()
        result_path, _ = await loop.run_in_executor(
            _process_pool,
            convert_document_format,
            task.cached_file,
            out_dir,
            task.doc_id or task.task_id,
            OutputFormat(task.output_format),
        )

        db_manager.update_task_status(task.task_id, "completed", result_path=result_path)
        logger.info(f"Task {task.task_id} completed → {result_path}")

        # For digitization: update job/doc to completed
        if task.operation == "digitization" and task.job_id and task.doc_id:
            from digitize.parsing.pdf import get_document_page_count
            from common.misc_utils import get_utc_timestamp
            page_count = get_document_page_count(task.cached_file)
            status_mgr = get_status_manager(task.job_id)
            status_mgr.update_doc_metadata(task.doc_id, {
                "status": DocStatus.COMPLETED,
                "pages": page_count,
                "completed_at": get_utc_timestamp(),
            })
            status_mgr.update_job_progress(task.doc_id, DocStatus.COMPLETED, JobStatus.COMPLETED)

        # For ingestion: orchestrator polls task.status — no extra update needed here.

    except Exception as exc:
        logger.error(f"Task {task.task_id} failed: {exc}", exc_info=True)
        db_manager.update_task_status(task.task_id, "failed", error=str(exc))

        if task.job_id and task.doc_id:
            from digitize.utils.db import get_status_manager
            from digitize.models import DocStatus, JobStatus
            status_mgr = get_status_manager(task.job_id)
            status_mgr.update_doc_metadata(
                task.doc_id, {"status": DocStatus.FAILED},
                error=f"Conversion failed: {exc}",
            )
            status_mgr.update_job_progress(
                task.doc_id, DocStatus.FAILED, JobStatus.FAILED,
                error=f"Conversion failed: {exc}",
            )
    finally:
        await conversion_semaphore.release(weight)


async def dispatch_loop() -> None:
    """
    Long-running coroutine that polls the DB and dispatches conversion tasks.

    Started once in app.py's lifespan and cancelled on shutdown.
    """
    global _rr_turn
    logger.info("Conversion dispatcher loop started")

    while True:
        try:
            available = conversion_semaphore.available
            if available > 0:
                first, second = _rr_turn, _other(_rr_turn)

                # Peek at first type's head weight whether or not we can claim it.
                first_head = db_manager.peek_head(first)
                first_needed = (2 if first_head.is_large else 1) if first_head else 0

                first_task = _try_claim_if_fits(first, available)
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

                # Budget for second = remaining capacity after reserving first_needed.
                # This prevents second from consuming units that first is waiting to accumulate.
                budget_for_second = max(0, available - first_needed)
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
            logger.info("Conversion dispatcher loop cancelled — shutting down")
            raise
        except Exception as exc:
            logger.error(f"Dispatcher loop error: {exc}", exc_info=True)
            # Don't crash the loop on transient errors; sleep and retry.

        await asyncio.sleep(settings.digitize.conversion_poll_interval)
