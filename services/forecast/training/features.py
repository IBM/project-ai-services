"""
Turn the loaded columns into the numeric matrix the forecaster is trained on.

Numeric columns pass through as float32. Every other column is a category and becomes
the index of its value in a sorted list, with -1 for NULL. The ONNX graph (onnx_export.py)
maps a value that is not in the list to -1 as well, and the API can only send NULL as text,
so NULL and an unseen value must mean the same thing in training and in serving. The same
list goes into the graph, so Triton accepts the original strings.
"""
from dataclasses import dataclass, field

import pandas as pd


@dataclass(frozen=True)
class Encoding:
    """The input columns in model order, and the sorted values of each category column."""

    columns: list[str]
    categories: dict[str, list[str]] = field(default_factory=dict)


def encode(df: pd.DataFrame, columns: list[str]) -> tuple[pd.DataFrame, Encoding]:
    """Return the float32 feature matrix of ``columns`` and the encoding that produced it."""
    features = pd.DataFrame(index=df.index)
    categories: dict[str, list[str]] = {}
    for name in columns:
        series = df[name]
        if pd.api.types.is_numeric_dtype(series) or pd.api.types.is_bool_dtype(series):
            features[name] = series.astype("float32")
            continue
        text = series.astype("string")
        categories[name] = sorted(text.dropna().unique())
        codes = pd.Categorical(text, categories=categories[name]).codes  # -1: NULL
        features[name] = codes.astype("float32")
    return features, Encoding(columns=list(columns), categories=categories)
