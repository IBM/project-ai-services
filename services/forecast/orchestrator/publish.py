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
from pathlib import Path

STAGING = ".staging"


def publish(repository: Path, job_id: int, name: str, model_dir: Path) -> Path:
    """Copy ``model_dir`` to the staging directory, rename it to ``name`` and return its path.

    A staging directory that the copy left behind is removed before the error is raised.

    Raises:
        FileExistsError: the staging directory or the target already exists.
    """
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

    staging.rename(target)
    return target
