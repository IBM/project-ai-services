"""
Runs the loader and the worker as subprocesses of the API.

Each one starts in its own process group, so that a time limit or a memory limit kills it together
with every process it forked (AutoGluon forks). No cgroup can be created for it: the cgroup
filesystem of the container is read-only, both under Podman and on OpenShift. The memory cap is
therefore a watchdog on resident memory: once a second it sums the RSS of the group and kills the
group above ``memory_limit_mb``. The pod's own memory limit stays the last bound.

The API is PID 1 of its container (the Containerfile starts uvicorn directly), so the grandchildren of
a killed group are re-parented to it, and nothing else would wait for them: they would stay zombies
until the container restarts. After a kill the API therefore waits for them itself (``reap``).

The process is waited for on the event loop, and the one-second scan of the process table runs in a
thread, so the API keeps serving requests while a job runs.
"""
import asyncio
import contextlib
import os
import signal
import time
from dataclasses import dataclass
from typing import Mapping, Optional, Sequence

import psutil

from common.misc_utils import get_logger

logger = get_logger("forecast_orchestrator")

POLL_INTERVAL_S = 1.0
REAP_TIMEOUT_S = 2.0
REAP_POLL_INTERVAL_S = 0.05


@dataclass(frozen=True)
class ProcessResult:
    """How a subprocess ended. ``exit_code`` is negative when a signal ended it."""

    exit_code: int
    timed_out: bool = False
    memory_exceeded: bool = False

    @property
    def ok(self) -> bool:
        return self.exit_code == 0 and not self.timed_out and not self.memory_exceeded


def group_rss_mb(pgid: int) -> float:
    """Resident memory in MB of every process of the process group ``pgid``."""
    total = 0
    for process in psutil.process_iter():
        try:
            if os.getpgid(process.pid) == pgid:
                total += process.memory_info().rss
        except (psutil.Error, ProcessLookupError, PermissionError):
            continue  # The process ended while the table was scanned.
    return total / 2**20


def group_members(pgid: int) -> list[psutil.Process]:
    """Every process of the process group ``pgid``, each tied to its start time.

    ``create_time`` is read here so that a later ``is_running`` tells the process apart from a new one
    that was given the same pid.
    """
    members = []
    for process in psutil.process_iter():
        try:
            if os.getpgid(process.pid) == pgid:
                process.create_time()
                members.append(process)
        except (psutil.Error, ProcessLookupError, PermissionError):
            continue
    return members


def reap(processes: Sequence[psutil.Process], timeout_s: float = REAP_TIMEOUT_S) -> None:
    """Wait for the killed ``processes`` that this process has to wait for.

    A process that was killed stays a zombie until its parent waits for it. A killed grandchild is
    re-parented to the API when its parent dies, which happens a moment after the kill, so a process
    whose parent is another one of ``pids`` is waited for later. A process whose new parent is someone
    else, such as an init process of the container, is that parent's to reap and is left alone.
    Never waits for a pid that was not in ``processes``, and not for one that has been given to a new
    process since (``is_running`` compares the start time), so it cannot take the exit status of
    another child.
    """
    waiting = {process.pid: process for process in processes}
    pending, me = set(waiting), os.getpid()
    deadline = time.monotonic() + timeout_s
    while pending and time.monotonic() < deadline:
        for pid in sorted(pending):
            try:
                if not waiting[pid].is_running():
                    pending.discard(pid)  # Gone: reaped by its parent, and its pid may be someone else's now.
                    continue
                parent = waiting[pid].ppid()
            except psutil.NoSuchProcess:
                pending.discard(pid)
                continue
            if parent == me:
                try:
                    if os.waitpid(pid, os.WNOHANG)[0] == pid:
                        pending.discard(pid)
                except ChildProcessError:
                    pending.discard(pid)
            elif parent not in pending:
                pending.discard(pid)  # Somebody else's child.
        if pending:
            time.sleep(REAP_POLL_INTERVAL_S)
    if pending:
        logger.warning("Could not wait for processes %s after killing their group", sorted(pending))


def kill_group(pgid: int) -> list[psutil.Process]:
    """Send SIGKILL to the whole process group and return its members except the leader.

    Only a group that still has a member is signalled. A member keeps the group number reserved, but
    once the leader has been waited for and nobody is left, the number can be given to an unrelated
    process, and signalling it would kill that one. A group that is already gone is not an error, and
    macOS answers EPERM, not ESRCH, for a group whose members are all zombies, so that is ignored too.
    The API only kills groups that it started, so a real permission error cannot happen. The leader is
    not returned because asyncio waits for it.
    """
    members = group_members(pgid)
    if not members:
        return []
    with contextlib.suppress(ProcessLookupError, PermissionError):
        os.killpg(pgid, signal.SIGKILL)
    return [member for member in members if member.pid != pgid]


async def _watch_memory(pgid: int, limit_mb: int, interval_s: float) -> float:
    """Return the memory in MB that the group used when it passed ``limit_mb``. Runs until cancelled."""
    while True:
        await asyncio.sleep(interval_s)
        try:
            used = await asyncio.to_thread(group_rss_mb, pgid)
        except Exception:
            logger.exception("Could not read the memory of process group %s", pgid)
            continue
        if used > limit_mb:
            return used


async def run_process(
    args: Sequence[str],
    *,
    name: str,
    timeout_s: float,
    memory_limit_mb: Optional[int] = None,
    env: Optional[Mapping[str, str]] = None,
    poll_interval_s: float = POLL_INTERVAL_S,
) -> ProcessResult:
    """Run ``args`` in its own process group and wait for it. ``name`` is what the log calls it.

    The group is killed when ``timeout_s`` passes or, when ``memory_limit_mb`` is set, when its
    resident memory passes the limit. The group is also killed when the caller is cancelled and after
    a normal exit, so nothing a process left behind keeps running, and what it killed is waited for, so
    that it does not stay a zombie. The output is not captured: it goes
    to the log of the API.
    """
    process = await asyncio.create_subprocess_exec(
        *args, env=None if env is None else dict(env), start_new_session=True
    )
    pgid = process.pid  # start_new_session makes the process the leader of a new group.
    waiter = asyncio.ensure_future(process.wait())
    watchdog = None
    if memory_limit_mb:
        watchdog = asyncio.ensure_future(_watch_memory(pgid, memory_limit_mb, poll_interval_s))
    timed_out = memory_exceeded = False
    killed: dict[int, psutil.Process] = {}  # Members of the group, other than its leader, that the kills reached.
    try:
        done, _ = await asyncio.wait(
            [waiter, *([watchdog] if watchdog else [])], timeout=timeout_s, return_when=asyncio.FIRST_COMPLETED
        )
        if waiter not in done:
            if watchdog is not None and watchdog in done:
                memory_exceeded = True
                logger.error(
                    "The %s passed the memory limit of %s MB (%.0f MB used), killing it",
                    name, memory_limit_mb, watchdog.result(),
                )
            else:
                timed_out = True
                logger.error("The %s ran longer than %s s, killing it", name, timeout_s)
            killed.update({member.pid: member for member in kill_group(pgid)})
            await waiter
    finally:
        killed.update({member.pid: member for member in kill_group(pgid)})  # Leftovers of a normal exit, or everything after a cancel.
        for task in (waiter, watchdog):
            if task is not None and not task.done():
                task.cancel()
        if killed:
            await asyncio.to_thread(reap, list(killed.values()))  # In a thread: it polls, and the loop serves requests.
    return ProcessResult(process.returncode, timed_out=timed_out, memory_exceeded=memory_exceeded)
