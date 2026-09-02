"""
Conversion dispatcher — 3-turn fair round-robin Docling conversion queue.

A single long-running asyncio task (started in app.py lifespan) that
polls the ``conversion_tasks`` table every ``conversion_poll_interval``
seconds and dispatches at most one task per tick.

Scheduling
----------
Three logical lanes are served in a fixed cycle:

  Turn 0 — U-ING : user ingestion
  Turn 1 — U-DIG : user digitization
  Turn 2 — C-ING : connector ingestion (round-robin across connectors)

This gives user lanes 2 out of every 3 dispatch opportunities and
connector lanes 1 out of 3, with no lane ever starving.

Turn advancement
----------------
The turn advances when a task was claimed OR the queue was empty.
It holds only when the head task exists but its weight exceeds available slots (HOL).

- Task claimed (dispatched)           → advance
- Queue empty for the lane            → advance (nothing to wait for; move on)
- HOL-blocked (head weight > avail)   → hold — retry same lane next tick

"Advance on empty" is critical: if a lane has nothing queued, holding its
turn would starve the other lanes indefinitely.  HOL-hold is intentionally
narrower — only fires when a real task is blocked on semaphore capacity.
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

# 3-turn round-robin state.
# _op_turn advances only when the current lane is not HOL-blocked.
_op_turn: int = 0             # 0 = U-ING  1 = U-DIG  2 = C-ING
_connector_rr_index: int = 0  # index into the live connector list for turn 2

# Process pool — created lazily inside dispatch_loop() so worker processes are
# not spawned on module import (which would affect test collection, CLI tools,
# and any code that transitively imports this module).
_process_pool: ProcessPoolExecutor | None = None


def _try_claim_if_fits(
    operation: str,
    available: int,
    connector_id: str | None = None,
) -> tuple[ConversionTask | None, bool]:
    """
    Peek at the head of the queue for ``operation`` (scoped to
    ``connector_id`` when given).  If the head task fits within
    ``available`` semaphore units, atomically claim and return it.

    Returns
    -------
    (task, hol_blocked)
        task        — the claimed ConversionTask, or None if not dispatched.
        hol_blocked — True when a head task exists but its weight exceeds
                      ``available``.  False when the queue is empty or the
                      task was claimed successfully.
    """
    head = db_manager.peek_head(operation, connector_id=connector_id)
    if head is None:
        return None, False  # empty queue — not HOL-blocked

    if (2 if head.is_large else 1) > available:
        return None, True   # head can't run yet — HOL-blocked

    return db_manager.claim_head(operation, connector_id=connector_id), False

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
    (queued → running → completed / failed) and result_path / error.
    All job and document status updates (JobStatus, DocStatus) are the
    responsibility of the pipeline layer that polls task.status.
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

        # Mark task as running — pipeline layer picks this up via poll.
        db_manager.update_task_status(task.task_id, ConversionTaskStatus.RUNNING)

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

        db_manager.update_task_status(task.task_id, ConversionTaskStatus.COMPLETED, result_path=result_path)
        logger.info(f"Task {task.task_id} completed → {result_path}")

    except Exception as exc:
        logger.error(f"Task {task.task_id} failed: {exc}", exc_info=True)
        db_manager.update_task_status(task.task_id, ConversionTaskStatus.FAILED, error=str(exc))

    finally:
        # Do NOT delete task.cached_file here — the pipeline layer owns staging
        # cleanup after all post-conversion processing (page loading, chunking,
        # indexing) has finished.  See _run_ingest / _run_digitize / sync_tick.
        await conversion_semaphore.release(weight)


async def _dispatch_one(task: ConversionTask) -> None:
    """Acquire a semaphore slot and fire the conversion coroutine."""
    weight = 2 if task.is_large else 1
    await conversion_semaphore.acquire(weight)
    asyncio.create_task(_run_conversion(task, weight))
    logger.debug(
        f"Dispatched task {task.task_id} "
        f"(op={task.operation}, file={Path(task.cached_file).name}, weight={weight}, "
        f"semaphore={conversion_semaphore.available}/{conversion_semaphore.capacity})"
    )


async def dispatch_loop() -> None:
    """
    Long-running coroutine that polls the DB and dispatches conversion tasks.

    Started once in app.py's lifespan and cancelled on shutdown.
    The process pool is created here (not at module import) so worker
    processes are only spawned when the dispatcher actually starts.
    """
    global _op_turn, _connector_rr_index, _process_pool

    _process_pool = ProcessPoolExecutor(
        max_workers=conversion_semaphore.capacity  # default 4
    )
    logger.info(
        f"Conversion dispatcher started (pool workers={conversion_semaphore.capacity})"
    )

    try:
        while True:
            try:
                available = conversion_semaphore.available

                hol_blocked = False

                if _op_turn == 0:                        # ── Turn 0: User Ingestion ──
                    task, hol_blocked = _try_claim_if_fits("ingestion", available)
                    if task:
                        await _dispatch_one(task)

                elif _op_turn == 1:                      # ── Turn 1: User Digitization ──
                    task, hol_blocked = _try_claim_if_fits("digitization", available)
                    if task:
                        await _dispatch_one(task)

                else:                                    # ── Turn 2: Connector Ingestion ──
                    cids = db_manager.get_connector_ids_with_queued_tasks()
                    if cids:
                        cid = cids[_connector_rr_index % len(cids)]
                        task, hol_blocked = _try_claim_if_fits("ingestion", available, connector_id=cid)
                        if task:
                            await _dispatch_one(task)
                            _connector_rr_index = (_connector_rr_index + 1) % len(cids)

                if not hol_blocked:
                    _op_turn = (_op_turn + 1) % 3        # advance only when not HOL-blocked

                # Promote pending → queued for user lanes only.
                # Connector tasks are always inserted as 'queued' — no promote needed.
                db_manager.promote_pending("ingestion",    settings.digitize.ingestion_queue_quota)
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
