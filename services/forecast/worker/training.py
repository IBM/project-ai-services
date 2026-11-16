"""AutoGluon training of one forecaster from the loaded data."""
import time
from dataclasses import dataclass
from pathlib import Path

import pandas as pd

from forecast.worker.errors import PermanentFailure
from forecast.worker.features import Encoding, encode
from forecast.worker.jobs import TrainingJob
from forecast.worker.onnx_export import EXPORTABLE_HYPERPARAMETERS

# AutoGluon gets part of the job's time limit. Loading the data and exporting the model
# take the rest, and the API kills the worker at the limit.
FIT_TIME_FRACTION = 0.8


@dataclass(frozen=True)
class Trained:
    """A trained AutoGluon predictor and what jobs.result records about it."""

    predictor: object  # autogluon.tabular.TabularPredictor
    encoding: Encoding
    algorithm: str
    validation_metric: str
    validation_score: float  # AutoGluon's convention: higher is better, errors are negated.
    training_seconds: float


def train(data: pd.DataFrame, job: TrainingJob, workdir: Path, time_limit_seconds: int) -> Trained:
    """Train on ``data`` (the input columns and the target) and return the best model.

    Raises:
        PermanentFailure: the target has fewer than two different values.
    """
    from autogluon.features.generators import IdentityFeatureGenerator
    from autogluon.tabular import TabularPredictor

    target = data[job.target_column]
    labelled = target.notna()
    if target[labelled].nunique() < 2:
        raise PermanentFailure(
            f"Target column {job.target_column} needs at least two different values to train on"
        )

    features, encoding = encode(data[labelled], [column["name"] for column in job.input_columns])
    features[job.target_column] = target[labelled]

    started = time.monotonic()
    predictor = TabularPredictor(label=job.target_column, path=str(workdir / "autogluon"), verbosity=0)
    predictor.fit(
        features,
        time_limit=max(1, int(time_limit_seconds * FIT_TIME_FRACTION)),
        hyperparameters=EXPORTABLE_HYPERPARAMETERS,
        fit_weighted_ensemble=False,  # An ensemble cannot be exported to ONNX.
        feature_generator=IdentityFeatureGenerator(),  # Keep the columns the ONNX graph is given.
    )
    return Trained(
        predictor=predictor,
        encoding=encoding,
        algorithm=predictor.model_best,
        validation_metric=predictor.eval_metric.name,
        validation_score=float(predictor.info()["best_model_score_val"]),
        training_seconds=round(time.monotonic() - started, 2),
    )
