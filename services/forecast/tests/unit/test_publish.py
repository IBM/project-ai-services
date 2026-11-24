"""Unit tests of the API's publish of a forecaster to the repository volume (orchestrator/publish.py)."""
import os
import stat

import pytest

from forecast.orchestrator.publish import STAGING, publish
from forecast.tests.unit.test_worker import identity_model
from forecast.training.model_dir import write_model_dir

pytestmark = pytest.mark.unit


@pytest.fixture
def volume(tmp_path):
    """A repository volume, and a forecaster directory on scratch as the worker leaves it."""
    os.umask(0o022)
    repository = tmp_path / "models"
    repository.mkdir()
    model_dir = write_model_dir(tmp_path / "scratch" / "model", "fc_x_5", identity_model())
    return repository, model_dir


def test_publish_renames_a_complete_copy_to_the_forecaster_name(volume):
    repository, model_dir = volume

    published = publish(repository, 5, "fc_x_5", model_dir)

    assert published == repository / "fc_x_5"
    assert (published / "config.pbtxt").read_bytes() == (model_dir / "config.pbtxt").read_bytes()
    assert (published / "1" / "model.onnx").read_bytes() == (model_dir / "1" / "model.onnx").read_bytes()
    assert list((repository / STAGING).iterdir()) == []
    assert model_dir.is_dir()  # The scratch directory is the caller's to remove.


def test_published_directories_are_0755_and_files_0644(volume):
    repository, model_dir = volume

    published = publish(repository, 5, "fc_x_5", model_dir)

    modes = {p.relative_to(published): stat.S_IMODE(p.stat().st_mode) for p in [published, *published.rglob("*")]}
    assert set(modes.values()) == {0o755, 0o644}
    assert stat.S_IMODE((repository / STAGING).stat().st_mode) == 0o755


def test_publish_never_replaces_or_deletes_an_existing_forecaster(volume):
    repository, model_dir = volume
    existing = repository / "fc_x_5"
    existing.mkdir()
    (existing / "keep").write_text("kept")

    with pytest.raises(FileExistsError):
        publish(repository, 5, "fc_x_5", model_dir)

    assert (existing / "keep").read_text() == "kept"


def test_publish_never_writes_into_or_deletes_a_staging_directory_that_is_already_there(volume):
    repository, model_dir = volume
    staging = repository / STAGING / "5"
    staging.mkdir(parents=True)
    (staging / "keep").write_text("kept")

    with pytest.raises(FileExistsError):
        publish(repository, 5, "fc_x_5", model_dir)

    assert [p.name for p in staging.iterdir()] == ["keep"]  # Not merged into, not removed.
    assert not (repository / "fc_x_5").exists()


def test_a_failed_copy_leaves_no_staging_directory(volume, monkeypatch):
    repository, model_dir = volume

    def fail(src, dst):
        raise OSError("disk full")

    monkeypatch.setattr("forecast.orchestrator.publish.shutil.copyfile", fail)

    with pytest.raises(OSError, match="disk full"):
        publish(repository, 5, "fc_x_5", model_dir)

    assert list((repository / STAGING).iterdir()) == []
    assert not (repository / "fc_x_5").exists()
