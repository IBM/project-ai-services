"""
The directory of a trained forecaster, in the layout Triton serves.

The worker writes it to the output directory it is given, on local scratch. It never
touches the repository volume: the API copies the directory there and renames it to
``fc_<connector_id>_<version>`` (``forecast.orchestrator.publish``).

Triton model layout:
    <output_dir>/config.pbtxt
    <output_dir>/1/model.onnx

Directories are 0755 and files 0644 under the umask 022 that ``main`` sets.
"""
from pathlib import Path
from uuid import UUID

import onnx

from forecast.training.onnx_export import triton_config

TRITON_MODEL_VERSION = "1"  # Every forecaster version is its own Triton model.


def forecaster_name(connector_id: UUID, version: int) -> str:
    """Name of a forecaster: its directory on the volume and its model name in Triton."""
    return f"fc_{connector_id}_{version}"


def write_model_dir(output_dir: Path, name: str, model: onnx.ModelProto) -> Path:
    """Write ``config.pbtxt`` and ``1/model.onnx`` to ``output_dir`` and return it.

    Raises:
        FileExistsError: ``output_dir`` already holds files, such as those of an earlier attempt.
    """
    output_dir.mkdir(mode=0o755, parents=True, exist_ok=True)
    if any(output_dir.iterdir()):
        raise FileExistsError(f"{output_dir} is not empty")

    (output_dir / TRITON_MODEL_VERSION).mkdir(mode=0o755)
    (output_dir / TRITON_MODEL_VERSION / "model.onnx").write_bytes(model.SerializeToString())
    (output_dir / "config.pbtxt").write_text(triton_config(name, model))
    return output_dir
