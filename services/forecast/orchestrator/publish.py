"""
Atomic publish of a forecaster to the shared repository volume.

The worker leaves the forecaster's Triton directory on local scratch. This module copies it to
``.staging/<job_id>/`` on the volume and renames that directory to ``fc_<connector_id>_<version>``,
so Triton and the API see the whole forecaster or nothing. Both paths are on one filesystem, so the
rename is atomic. The API runs this right before it loads the forecaster into Triton, and it
alone creates, renames and deletes directories on the volume (design, section 10.3).

The API sets umask 022 when it starts (app.py), so that Triton's user can read what is published.
"""
import shutil
import stat
from pathlib import Path

STAGING = ".staging"


def _check_plain_files(model_dir: Path) -> None:
    """Raise ValueError unless ``model_dir`` holds only directories and regular files.

    The worker writes this directory and is not trusted. The copy follows a symbolic link and publishes what it
    points to as a regular file, so a link to a file of the API (or a device or a pipe) must not get through.
    """
    entries = [model_dir, *model_dir.rglob("*")]
    for path in entries:
        mode = path.lstat().st_mode
        if not (stat.S_ISREG(mode) or stat.S_ISDIR(mode)):
            raise ValueError(f"{path} is not a regular file or directory, so the model directory is not published")


def publish(repository: Path, job_id: int, name: str, model_dir: Path) -> Path:
    """Copy ``model_dir`` to the staging directory, rename it to ``name`` and return its path.

    A staging directory that the copy or the rename left behind is removed before the error is raised.

    Raises:
        FileExistsError: the staging directory or the target already exists.
        ValueError: ``model_dir`` holds a symbolic link or another entry that is neither a file nor a directory.
    """
    _check_plain_files(model_dir)
    target = repository / name
    if target.exists():
        raise FileExistsError(f"{target} already exists")

    staging = repository / STAGING / str(job_id)
    staging.parent.mkdir(mode=0o755, exist_ok=True)
    try:
        shutil.copytree(model_dir, staging, copy_function=shutil.copyfile)
    except FileExistsError:
        raise
    except BaseException:
        shutil.rmtree(staging, ignore_errors=True)
        raise

    try:
        staging.rename(target)
    except BaseException:
        shutil.rmtree(staging, ignore_errors=True)
        raise
    return target
