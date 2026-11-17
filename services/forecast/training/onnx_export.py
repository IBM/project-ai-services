"""
Export the AutoGluon model to ONNX and describe it to Triton.

Only LightGBM and CatBoost are trained (``EXPORTABLE_HYPERPARAMETERS``), so the model
AutoGluon picks can always be exported. XGBoost is left out on purpose: AutoGluon feeds
it a sparse matrix, which makes XGBoost read 0 as missing, and the ONNX graph does not
reproduce that, so its predictions would differ from AutoGluon's.

The graph takes one tensor per input column, FLOAT for a numeric column and STRING for
a category column, and returns:
  classification  ``label`` (STRING, the original class value) and ``probabilities``
  regression      ``prediction`` (FLOAT)
A category value that was not seen in training maps to -1.
"""
import json

import onnx
from onnx import TensorProto, helper

from forecast.training.features import Encoding

LABEL = "label"
PROBABILITIES = "probabilities"
PREDICTION = "prediction"

_OPSET = 13
_IR_VERSION = 8
_TRITON_TYPES = {
    TensorProto.FLOAT: "TYPE_FP32",
    TensorProto.INT64: "TYPE_INT64",
    TensorProto.STRING: "TYPE_STRING",
}
_TREE_FEATURES = "features"


def _convert_lightgbm(booster, n_features: int) -> onnx.ModelProto:
    from onnxmltools import convert_lightgbm
    from onnxmltools.convert.common.data_types import FloatTensorType

    return convert_lightgbm(
        booster, initial_types=[(_TREE_FEATURES, FloatTensorType([None, n_features]))], zipmap=False
    )


def _convert_catboost(model, n_features: int) -> onnx.ModelProto:
    from catboost.utils import convert_to_onnx_object

    onnx_model = convert_to_onnx_object(model)
    graph = onnx_model.graph
    # CatBoost wraps the probabilities of a classifier in a ZipMap, which Triton cannot serve.
    zipmap = next((node for node in graph.node if node.op_type == "ZipMap"), None)
    if zipmap is not None:
        graph.node.remove(zipmap)
        graph.output[1].CopyFrom(
            helper.make_tensor_value_info(zipmap.input[0], TensorProto.FLOAT, [None, None])
        )
    return onnx_model


# AutoGluon's key of an algorithm -> its converter. The converter takes what the AutoGluon
# model wraps and the number of features, and returns a graph with one input.
_CONVERTERS = {"GBM": _convert_lightgbm, "CAT": _convert_catboost}
EXPORTABLE_HYPERPARAMETERS = {key: {} for key in _CONVERTERS}


def _rename(graph: onnx.GraphProto, old: str, new: str) -> None:
    for node in graph.node:
        node.input[:] = [new if name == old else name for name in node.input]
        node.output[:] = [new if name == old else name for name in node.output]


def export(predictor, encoding: Encoding) -> onnx.ModelProto:
    """Return the ONNX model of the best model of a trained ``TabularPredictor``."""
    best = predictor._trainer.load_model(predictor.model_best)
    tree = _CONVERTERS[best.ag_key](best.model, len(encoding.columns))
    graph = tree.graph

    tree_input = graph.input[0].name
    tree_outputs = []
    for i, output in enumerate(graph.output):
        _rename(graph, output.name, f"_tree_out{i}")
        tree_outputs.append(f"_tree_out{i}")

    inputs, before, parts = [], [], []
    for i, name in enumerate(encoding.columns):
        if name in encoding.categories:
            values = encoding.categories[name]
            inputs.append(helper.make_tensor_value_info(name, TensorProto.STRING, [None, 1]))
            before.append(
                helper.make_node(
                    "CategoryMapper", [name], [f"_id{i}"], domain="ai.onnx.ml",
                    cats_strings=values, cats_int64s=list(range(len(values))), default_int64=-1,
                )
            )
            before.append(helper.make_node("Cast", [f"_id{i}"], [f"_col{i}"], to=TensorProto.FLOAT))
            parts.append(f"_col{i}")
        else:
            inputs.append(helper.make_tensor_value_info(name, TensorProto.FLOAT, [None, 1]))
            parts.append(name)
    before.append(helper.make_node("Concat", parts, [tree_input], axis=1))

    after = []
    if predictor.problem_type == "regression":
        after.append(helper.make_node("Identity", [tree_outputs[0]], [PREDICTION]))
        outputs = [helper.make_tensor_value_info(PREDICTION, TensorProto.FLOAT, [None, 1])]
    else:
        index = predictor.class_labels_internal_map
        classes = sorted(index, key=index.get)  # Class values in the order of the model's columns.
        after.append(
            helper.make_node(
                "CategoryMapper", [tree_outputs[0]], [LABEL], domain="ai.onnx.ml",
                cats_int64s=list(range(len(classes))), cats_strings=[str(c) for c in classes],
                default_string="",
            )
        )
        after.append(helper.make_node("Identity", [tree_outputs[1]], [PROBABILITIES]))
        outputs = [
            helper.make_tensor_value_info(LABEL, TensorProto.STRING, [None]),
            helper.make_tensor_value_info(PROBABILITIES, TensorProto.FLOAT, [None, len(classes)]),
        ]

    existing = list(graph.node)
    del graph.node[:], graph.input[:], graph.output[:], graph.value_info[:]
    graph.node.extend(before + existing + after)
    graph.input.extend(inputs)
    graph.output.extend(outputs)

    opsets = {opset.domain: opset for opset in tree.opset_import}
    if "" in opsets:
        opsets[""].version = max(opsets[""].version, _OPSET)
    else:
        tree.opset_import.append(helper.make_opsetid("", _OPSET))
    if "ai.onnx.ml" not in opsets:
        tree.opset_import.append(helper.make_opsetid("ai.onnx.ml", 1))
    tree.ir_version = max(tree.ir_version, _IR_VERSION)
    onnx.checker.check_model(tree)
    return tree


def _tensors(values) -> str:
    entries = []
    for value in values:
        tensor = value.type.tensor_type
        dims = ", ".join(str(d.dim_value) if d.HasField("dim_value") else "-1" for d in tensor.shape.dim)
        entries.append(
            f"  {{\n    name: {json.dumps(value.name)}\n"
            f"    data_type: {_TRITON_TYPES[tensor.elem_type]}\n    dims: [ {dims} ]\n  }}"
        )
    return ",\n".join(entries)


def triton_config(name: str, model: onnx.ModelProto) -> str:
    """Return config.pbtxt for ``model``: its tensors are read from the ONNX graph."""
    return (
        f'name: "{name}"\nbackend: "onnxruntime"\nmax_batch_size: 0\n\n'
        f"input [\n{_tensors(model.graph.input)}\n]\n\n"
        f"output [\n{_tensors(model.graph.output)}\n]\n\n"
        "instance_group [ { kind: KIND_CPU, count: 1 } ]\n"
    )
