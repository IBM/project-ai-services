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


@pytest.mark.parametrize("kind", ["file link", "directory link", "dangling link", "fifo", "link in a subdirectory"])
def test_publish_refuses_a_model_directory_with_a_link_or_a_special_file(volume, tmp_path, kind):
    repository, model_dir = volume
    secret = tmp_path / "secret.txt"
    secret.write_text("api secret")
    if kind == "file link":
        (model_dir / "leak").symlink_to(secret)
    elif kind == "directory link":
        (model_dir / "leak").symlink_to(tmp_path)
    elif kind == "dangling link":
        (model_dir / "leak").symlink_to(tmp_path / "missing")
    elif kind == "fifo":
        os.mkfifo(model_dir / "leak")
    else:
        (model_dir / "1" / "leak").symlink_to(secret)

    with pytest.raises(ValueError, match="not a regular file or directory"):
        publish(repository, 5, "fc_x_5", model_dir)

    assert not (repository / STAGING).exists()  # Nothing was created, so nothing needs cleaning up.
    assert not (repository / "fc_x_5").exists()


def test_publish_refuses_a_model_directory_that_is_itself_a_link(volume, tmp_path):
    repository, model_dir = volume
    link = tmp_path / "scratch" / "link"
    link.symlink_to(model_dir, target_is_directory=True)

    with pytest.raises(ValueError, match="not a regular file or directory"):
        publish(repository, 5, "fc_x_5", link)

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


def test_a_failed_rename_leaves_no_staging_directory(volume, monkeypatch):
    repository, model_dir = volume

    def fail(self, target):
        raise OSError("cross-device link")

    monkeypatch.setattr("pathlib.Path.rename", fail)

    with pytest.raises(OSError, match="cross-device link"):
        publish(repository, 5, "fc_x_5", model_dir)

    assert list((repository / STAGING).iterdir()) == []
    assert not (repository / "fc_x_5").exists()
