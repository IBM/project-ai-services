"""Names that the API, the worker and Triton must agree on."""
from uuid import UUID


def forecaster_name(connector_id: UUID | str, version: int) -> str:
    """Name of a forecaster: its directory on the volume and its model name in Triton."""
    return f"fc_{connector_id}_{version}"
