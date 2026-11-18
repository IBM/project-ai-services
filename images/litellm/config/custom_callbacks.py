import re
from collections.abc import Mapping, Sequence

from fastapi import HTTPException

from litellm.integrations.custom_logger import CustomLogger
from litellm.proxy.pass_through_endpoints.llm_provider_handlers.watsonx_passthrough_handler import (
    WatsonxPassthroughHandler,
)

PASS_THROUGH_CALL_TYPE = "pass_through_endpoint"
TOKENIZE_ROUTE_PATTERN = re.compile(r"^/tokenize/(?P<tag>[^/]+)/?$")
WATSONX_TOKENIZATION_ROUTE = "/watsonx/ml/v1/text/tokenization"
WATSONX_PROVIDER = "watsonx"
TAG_FIELD = "tokenize_tag"
ERROR_KEYWORD_STATUS_CODES = (
    (("unauthorized", "authentication"), 401),
    (("forbidden", "permission"), 403),
    (("not found",), 404),
    (("bad request", "invalid"), 400),
    (("timeout",), 504),
)


def _tokenize_tag(request_route: str | None) -> str | None:
    match = TOKENIZE_ROUTE_PATTERN.match(request_route or "")
    return match.group("tag") if match else None


def _deployment_tags(deployment: Mapping[str, object]) -> frozenset[str]:
    sources = (deployment.get("model_info"), deployment.get("litellm_params"))
    return frozenset(
        tag
        for source in sources
        if isinstance(source, Mapping)
        for tag in (source.get("tags") or ())
        if isinstance(tag, str)
    )


def _provider_of(params: Mapping[str, object]) -> str | None:
    provider = params.get("custom_llm_provider")
    if isinstance(provider, str):
        return provider
    model = params.get("model")
    return model.split("/", 1)[0] if isinstance(model, str) else None


def _watsonx_params_for_tag(deployments: Sequence[Mapping[str, object]], tag: str) -> Mapping[str, object] | None:
    for deployment in deployments:
        params = deployment.get("litellm_params")
        if tag not in _deployment_tags(deployment) or not isinstance(params, Mapping):
            continue
        if _provider_of(params) == WATSONX_PROVIDER:
            return params
    return None


def _router_deployments() -> Sequence[Mapping[str, object]]:
    from litellm.proxy.proxy_server import llm_router

    return (llm_router.get_model_list() or []) if llm_router is not None else []


def _route_of(user_api_key_dict: object) -> str | None:
    return getattr(user_api_key_dict, "request_route", None)


def _call_type_of(data: object) -> str | None:
    if not isinstance(data, Mapping):
        return None
    call_type = data.get("call_type")
    if isinstance(call_type, str):
        return call_type
    return getattr(data.get("litellm_logging_obj"), "call_type", None)


def _is_watsonx_tokenization(user_api_key_dict: object, data: object) -> bool:
    return _call_type_of(data) == PASS_THROUGH_CALL_TYPE and _route_of(user_api_key_dict) == WATSONX_TOKENIZATION_ROUTE


def _status_code_of(exception: Exception) -> int | None:
    status_code = getattr(exception, "status_code", None)
    if isinstance(status_code, int):
        return status_code
    message = str(exception)
    numeric_match = re.search(r"\b([45]\d{2})\b", message)
    if numeric_match:
        return int(numeric_match.group(1))
    lowered = message.lower()
    return next(
        (code for keywords, code in ERROR_KEYWORD_STATUS_CODES if any(keyword in lowered for keyword in keywords)),
        None,
    )


class TokenizeTranslator(CustomLogger):
    async def async_pre_call_hook(self, user_api_key_dict, cache, data, call_type):
        if call_type != PASS_THROUGH_CALL_TYPE or not isinstance(data, dict):
            return data

        route = _route_of(user_api_key_dict)
        tag = _tokenize_tag(route)
        if tag is not None and _watsonx_params_for_tag(_router_deployments(), tag) is not None:
            return {**data, TAG_FIELD: tag}
        if route == WATSONX_TOKENIZATION_ROUTE and isinstance(data.get(TAG_FIELD), str):
            return await self._translate_to_watsonx(user_api_key_dict, data)
        return data

    @staticmethod
    async def _translate_to_watsonx(user_api_key_dict, data: dict) -> dict:
        tag = data[TAG_FIELD]
        allowed = await WatsonxPassthroughHandler.allowed_deployments(
            user_api_key_dict=user_api_key_dict, requested_model=None
        )
        params = _watsonx_params_for_tag(allowed, tag) or {}
        model, project_id = params.get("model"), params.get("project_id")
        if not isinstance(model, str) or not isinstance(project_id, str):
            raise HTTPException(status_code=404, detail=f"No model found for tag '{tag}' accessible with this key")

        passthrough = {key: value for key, value in data.items() if key not in (TAG_FIELD, "prompt")}
        return {
            **passthrough,
            "input": data.get("prompt", data.get("input")),
            "model_id": model.removeprefix("watsonx/"),
            "project_id": project_id,
            "parameters": {"return_tokens": True},
        }

    async def async_post_call_success_hook(self, data, user_api_key_dict, response):
        if not _is_watsonx_tokenization(user_api_key_dict, data):
            return response
        if not (isinstance(response, dict) and isinstance(response.get("result"), dict)):
            return response

        result = response["result"]
        if "tokens" not in result:
            return response
        tokens = result["tokens"]
        return {"tokens": tokens, "count": result.get("token_count", len(tokens))}

    async def async_post_call_failure_hook(self, request_data, original_exception, user_api_key_dict, traceback_str):
        if not _is_watsonx_tokenization(user_api_key_dict, request_data):
            return None

        status_code = _status_code_of(original_exception)
        if status_code is None:
            return None
        return HTTPException(status_code=status_code, detail=str(original_exception))


translator_instance = TokenizeTranslator()
