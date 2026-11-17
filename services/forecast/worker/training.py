"""AutoGluon training of one forecaster from the loaded data."""
import time
from dataclasses import dataclass
from pathlib import Path

import pandas as pd
from sklearn.model_selection import train_test_split

from forecast.worker.errors import PermanentFailure
from forecast.worker.features import Encoding, encode
from forecast.worker.jobs import TrainingJob
from forecast.worker.onnx_export import EXPORTABLE_HYPERPARAMETERS

# AutoGluon gets part of the job's time limit. Loading the data and exporting the model
# take the rest, and the API kills the worker at the limit.
FIT_TIME_FRACTION = 0.8

# The model is fitted on the training split only. The held-out test split scores it on rows
# it has never seen, which is the number to trust. AutoGluon's own validation score is
# taken from rows it picked the model on.
TEST_SIZE = 0.2
RANDOM_STATE = 42


@dataclass(frozen=True)
class Trained:
    """A trained AutoGluon predictor and what jobs.result records about it."""

    predictor: object  # autogluon.tabular.TabularPredictor
    encoding: Encoding
    algorithm: str
    validation_metric: str
    validation_score: float  # AutoGluon's convention: higher is better, errors are negated.
    test_score: float  # The same metric and convention, on the held-out test split.
    training_seconds: float


def _split(features: pd.DataFrame, target: str) -> tuple[pd.DataFrame, pd.DataFrame]:
    """Split into training and test rows, keeping the class ratio when the target has classes.

    A target that cannot be stratified (a regression target, or a class with a single row)
    is split at random.
    """
    from autogluon.core.constants import REGRESSION
    from autogluon.core.utils import infer_problem_type

    is_class = infer_problem_type(features[target], silent=True) != REGRESSION
    can_stratify = is_class and features[target].value_counts().min() >= 2
    return train_test_split(
        features,
        test_size=TEST_SIZE,
        random_state=RANDOM_STATE,
        stratify=features[target] if can_stratify else None,
    )


def train(data: pd.DataFrame, job: TrainingJob, workdir: Path, time_limit_seconds: int) -> Trained:
    """Train on 80% of ``data`` (the input columns and the target) and test the best model on the rest.

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

    features, encoding = encode(data[labelled], job.input_names)
    features[job.target_column] = target[labelled]
    train_rows, test_rows = _split(features, job.target_column)

    started = time.monotonic()
    predictor = TabularPredictor(label=job.target_column, path=str(workdir / "autogluon"), verbosity=0)
    predictor.fit(
        train_rows,
        time_limit=max(1, int(time_limit_seconds * FIT_TIME_FRACTION)),
        hyperparameters=EXPORTABLE_HYPERPARAMETERS,
        fit_weighted_ensemble=False,  # An ensemble cannot be exported to ONNX.
        feature_generator=IdentityFeatureGenerator(),  # Keep the columns the ONNX graph is given.
    )
    metric = predictor.eval_metric.name
    return Trained(
        predictor=predictor,
        encoding=encoding,
        algorithm=predictor.model_best,
        validation_metric=metric,
        validation_score=float(predictor.info()["best_model_score_val"]),
        test_score=float(predictor.evaluate(test_rows, silent=True)[metric]),
        training_seconds=round(time.monotonic() - started, 2),
    )
