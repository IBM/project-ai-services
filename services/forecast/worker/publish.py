"""
Atomic publish of a forecaster to the shared repository volume.

The files are written to ``.staging/<job_id>/`` on the volume and the directory is
renamed to ``fc_<connector_id>_<version>``, so Triton and the API see the whole
forecaster or nothing. Both paths are on one filesystem, so the rename is atomic.
This module only creates and renames. The API deletes leftovers (design, section 10.3).

Triton model layout:
    fc_<connector_id>_<version>/config.pbtxt
    fc_<connector_id>_<version>/1/model.onnx

Directories are 0755 and files 0644 under the umask 022 that ``main`` sets.
"""
from pathlib import Path
from uuid import UUID

import onnx

from forecast.worker.onnx_export import triton_config

STAGING = ".staging"
TRITON_MODEL_VERSION = "1"  # Every forecaster version is its own Triton model.


def forecaster_name(connector_id: UUID, version: int) -> str:
    """Name of a forecaster: its directory on the volume and its model name in Triton."""
    return f"fc_{connector_id}_{version}"


def publish(repository: Path, job_id: int, name: str, model: onnx.ModelProto) -> Path:
    """Write the forecaster to the staging directory, rename it to ``name`` and return its path.

    Raises:
        FileExistsError: the staging directory or the target already exists.
    """
    target = repository / name
    if target.exists():
        raise FileExistsError(f"{target} already exists")

    staging = repository / STAGING / str(job_id)
    staging.parent.mkdir(mode=0o755, exist_ok=True)
    staging.mkdir(mode=0o755)
    (staging / TRITON_MODEL_VERSION).mkdir(mode=0o755)
    (staging / TRITON_MODEL_VERSION / "model.onnx").write_bytes(model.SerializeToString())
    (staging / "config.pbtxt").write_text(triton_config(name, model))

    staging.rename(target)
    return target
