"""
Unit tests of the subprocess runner (orchestrator/process.py).

They run real processes, with the Python interpreter and small scripts, because what matters is what
the operating system does: that the whole process group dies, and that memory is counted over the group.
"""
import asyncio
import ctypes
import os
import subprocess
import sys
import time
from pathlib import Path
from types import SimpleNamespace

import psutil
import pytest

from forecast.orchestrator.process import ProcessResult, group_members, group_rss_mb, kill_group, reap, run_process

pytestmark = pytest.mark.unit

FAST = 0.1  # Poll interval of the memory watchdog.


def python(code: str) -> list[str]:
    return [sys.executable, "-c", code]


def run(code: str, **kwargs) -> ProcessResult:
    kwargs.setdefault("timeout_s", 20)
    return asyncio.run(run_process(python(code), name="test process", poll_interval_s=FAST, **kwargs))


def is_gone(pid: int, wait_s: float = 5.0) -> bool:
    """Whether the process ended. A zombie that nobody has waited for yet counts as ended."""
    deadline = time.monotonic() + wait_s
    while time.monotonic() < deadline:
        try:
            if psutil.Process(pid).status() == psutil.STATUS_ZOMBIE:
                return True
        except psutil.NoSuchProcess:
            return True
        time.sleep(0.05)
    return False


def start_child_script(pid_file: Path, then: str) -> str:
    """A script that starts a long-lived child, records its pid and then does ``then``."""
    return (
        "import subprocess, sys, time\n"
        "child = subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(60)'])\n"
        f"open({str(pid_file)!r}, 'w').write(str(child.pid))\n"
        f"{then}\n"
    )


def test_a_process_that_exits_0_is_ok():
    result = run("pass")

    assert result == ProcessResult(exit_code=0)
    assert result.ok


@pytest.mark.parametrize("code", [1, 3])
def test_the_exit_code_is_returned(code):
    result = run(f"raise SystemExit({code})")

    assert result.exit_code == code
    assert not result.ok
    assert not result.timed_out and not result.memory_exceeded


def test_the_environment_is_passed_on(tmp_path):
    out = tmp_path / "out"

    run(f"import os; open({str(out)!r}, 'w').write(os.environ.get('MARKER', 'missing'))", env={**os.environ, "MARKER": "set"})

    assert out.read_text() == "set"


def test_the_process_leads_its_own_group(tmp_path):
    out = tmp_path / "out"

    run(f"import os; open({str(out)!r}, 'w').write(f'{{os.getpid()}} {{os.getpgid(0)}}')")

    pid, pgid = map(int, out.read_text().split())
    assert pgid == pid != os.getpgid(0)


def test_a_timeout_kills_the_process_and_everything_it_started(tmp_path):
    pid_file = tmp_path / "child.pid"

    started = time.monotonic()
    result = run(start_child_script(pid_file, "time.sleep(60)"), timeout_s=1.5)

    assert result.timed_out and not result.ok
    assert result.exit_code == -9
    assert time.monotonic() - started < 15
    assert is_gone(int(pid_file.read_text()))


def test_what_a_process_left_behind_is_killed_after_a_normal_exit(tmp_path):
    pid_file = tmp_path / "child.pid"

    result = run(start_child_script(pid_file, "pass"))

    assert result.ok
    assert is_gone(int(pid_file.read_text()))


def test_the_memory_of_the_whole_group_is_counted(tmp_path):
    # The parent holds little. Its child, as AutoGluon's forks do, holds the memory.
    child = "import time; x = bytearray(150 * 2**20); x[::4096] = b'x' * len(x[::4096]); time.sleep(60)"
    script = f"import subprocess, sys, time\nsubprocess.Popen([sys.executable, '-c', {child!r}])\ntime.sleep(60)\n"

    started = time.monotonic()
    result = run(script, memory_limit_mb=100, timeout_s=30)

    assert result.memory_exceeded and not result.timed_out and not result.ok
    assert time.monotonic() - started < 25


def test_a_process_under_the_memory_limit_is_left_alone():
    result = run("import time; time.sleep(0.5)", memory_limit_mb=4096)

    assert result.ok


def test_no_memory_limit_means_no_watchdog():
    result = run("x = bytearray(50 * 2**20); x[::4096] = b'x' * len(x[::4096])", memory_limit_mb=None)

    assert result.ok


def test_the_group_is_killed_when_the_caller_is_cancelled(tmp_path):
    pid_file = tmp_path / "child.pid"

    async def scenario():
        task = asyncio.ensure_future(
            run_process(python(start_child_script(pid_file, "time.sleep(60)")), name="test process", timeout_s=60)
        )
        for _ in range(100):
            await asyncio.sleep(0.1)
            if pid_file.exists() and pid_file.read_text():
                break
        task.cancel()
        with pytest.raises(asyncio.CancelledError):
            await task

    asyncio.run(scenario())

    assert is_gone(int(pid_file.read_text()))


def test_the_event_loop_keeps_running_while_the_process_runs():
    async def scenario():
        ticks = 0

        async def tick():
            nonlocal ticks
            while True:
                await asyncio.sleep(0.05)
                ticks += 1

        ticker = asyncio.ensure_future(tick())
        await run_process(python("import time; time.sleep(1)"), name="test process", timeout_s=20, memory_limit_mb=4096, poll_interval_s=FAST)
        ticker.cancel()
        return ticks

    assert asyncio.run(scenario()) >= 10


def test_group_rss_counts_the_processes_of_the_group():
    assert group_rss_mb(os.getpgid(0)) > 0
    assert group_rss_mb(2**22 + 12345) == 0  # No such group.


def test_kill_group_ignores_a_group_that_is_gone():
    assert kill_group(2**22 + 12345) == []


def test_kill_group_does_not_signal_a_group_without_members(monkeypatch):
    # Its number may belong to an unrelated process by now.
    signalled = []
    monkeypatch.setattr(os, "killpg", lambda *args: signalled.append(args))

    assert kill_group(2**22 + 12345) == []

    assert signalled == []


def test_kill_group_returns_the_members_except_the_leader(tmp_path):
    pid_file = tmp_path / "child.pid"
    leader = subprocess.Popen(python(start_child_script(pid_file, "time.sleep(60)")), start_new_session=True)
    try:
        for _ in range(100):
            if pid_file.exists() and pid_file.read_text():
                break
            time.sleep(0.05)
        child = int(pid_file.read_text())

        assert sorted(member.pid for member in group_members(leader.pid)) == sorted([leader.pid, child])
        assert [member.pid for member in kill_group(leader.pid)] == [child]
    finally:
        kill_group(leader.pid)
        leader.wait()


def test_reap_waits_for_a_killed_child_of_this_process():
    child = subprocess.Popen(python("import time; time.sleep(60)"))
    child.kill()
    deadline = time.monotonic() + 5
    while psutil.Process(child.pid).status() != psutil.STATUS_ZOMBIE and time.monotonic() < deadline:
        time.sleep(0.01)

    reap([psutil.Process(child.pid)])

    assert not psutil.pid_exists(child.pid)  # A zombie would still be listed.


def test_reap_leaves_a_process_that_is_not_in_its_list_alone():
    other = subprocess.Popen(python("raise SystemExit(7)"))
    other_status = psutil.Process(other.pid)
    time.sleep(0.5)  # It is a zombie by now, which only other.wait() may collect.

    reap([psutil.Process(os.getppid())])  # Not our child: nothing to wait for, and no wait.

    assert other_status.status() == psutil.STATUS_ZOMBIE
    assert other.wait() == 7  # Its exit status was not taken.


def test_reap_leaves_a_pid_alone_that_has_been_given_to_a_new_process():
    other = subprocess.Popen(python("raise SystemExit(7)"))  # Our child, and a zombie soon: the new owner of the pid.
    time.sleep(0.5)
    # What was killed under this pid is gone: is_running compares the start time and says so.
    stale = SimpleNamespace(pid=other.pid, is_running=lambda: False)

    reap([stale])

    assert other.wait() == 7  # Its exit status was not taken.


def test_reap_gives_up_on_a_process_that_does_not_end(caplog):
    child = subprocess.Popen(python("import time; time.sleep(60)"))
    try:
        with caplog.at_level("WARNING"):
            reap([psutil.Process(child.pid)], timeout_s=0.3)
        assert "Could not wait for processes" in caplog.text
    finally:
        child.kill()
        child.wait()


@pytest.fixture
def subreaper():
    """Make this process adopt orphans, as the API does as PID 1 of its container. Linux only."""
    if not sys.platform.startswith("linux"):
        pytest.skip("PR_SET_CHILD_SUBREAPER exists on Linux only")
    PR_SET_CHILD_SUBREAPER = 36
    libc = ctypes.CDLL(None, use_errno=True)
    assert libc.prctl(PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0) == 0
    yield
    libc.prctl(PR_SET_CHILD_SUBREAPER, 0, 0, 0, 0)


def descendants():
    """The descendants of this process as (pid, start time), so that a reused pid is a different entry."""
    return {(child.pid, child.create_time()) for child in psutil.Process().children(recursive=True)}


def zombie_children():
    return [child.pid for child in psutil.Process().children(recursive=True) if child.status() == psutil.STATUS_ZOMBIE]


@pytest.mark.parametrize("how", ["timeout", "normal exit"])
def test_no_zombie_is_left_by_the_grandchildren_of_a_killed_group(subreaper, tmp_path, how):
    # The leader starts a child that starts a grandchild. Killing the group orphans them to this process.
    middle = (
        "import subprocess, sys, time\n"
        "subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(60)'])\n"
        "time.sleep(60)\n"
    )
    leader = f"import subprocess, sys, time\nsubprocess.Popen([sys.executable, '-c', {middle!r}])\n" + (
        "time.sleep(60)\n" if how == "timeout" else "time.sleep(1)\n"  # Exits while its descendants run.
    )

    # Other tests of the same pytest process may leave children behind (a multiprocessing resource tracker,
    # for one), and the whole suite runs in one process. Only what this test started counts.
    before = descendants()

    result = run(leader, timeout_s=3 if how == "timeout" else 20)

    assert result.timed_out == (how == "timeout")
    assert [pid for pid in zombie_children() if pid not in {p for p, _ in before}] == []
    assert descendants() - before == set()
