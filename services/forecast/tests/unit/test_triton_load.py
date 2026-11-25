"""
Unit tests of the load of a forecaster into Triton (triton_client/triton.py: load_model).

Triton is replaced by a client that answers the two calls the API makes: the load and the repository
index. The clock and the sleep are replaced too, so that the wait on the model's state runs at once.
"""
import asyncio
from types import SimpleNamespace

import httpx
import pytest

from forecast.settings import settings
from forecast.triton_client import triton
from forecast.triton_client.triton import TritonLoadError, load_model, unload_model

pytestmark = pytest.mark.unit

NAME = "fc_c9e66790-7425-40de-944b-e07fc1f90ae7_7"
URL = "http://triton:8000"


def reply(status=200, body=None, text=""):
    return SimpleNamespace(
        status_code=status,
        text=text or str(body),
        json=lambda: body if body is not None else (_ for _ in ()).throw(ValueError("no json")),
    )


class FakeTriton:
    """``load`` is the answer (or exception) to the load call, ``index`` a list of answers to successive index calls."""

    def __init__(self, load=None, index=()):
        self.load = load if load is not None else reply(200, {})
        self.index = list(index)
        self.calls = []

    async def post(self, url, json=None, timeout=None):
        self.calls.append((url, json, timeout))
        answer = self.load if url.endswith(("/load", "/unload")) else self.index.pop(0) if len(self.index) > 1 else self.index[0]
        if isinstance(answer, Exception):
            raise answer
        return answer


@pytest.fixture
def triton_env(monkeypatch):
    monkeypatch.setattr(settings.forecast, "triton_url", URL + "/")
    monkeypatch.setattr(settings.forecast, "triton_load_timeout_s", 5)
    clock = SimpleNamespace(now=0.0)
    monkeypatch.setattr(triton, "_monotonic", lambda: clock.now)

    async def sleep(seconds):
        clock.now += seconds

    monkeypatch.setattr(triton, "_sleep", sleep)

    def install(fake):
        monkeypatch.setattr(triton, "get_client", lambda: fake)
        return fake

    return install


def index(state, reason="", name=NAME):
    return reply(200, [{"name": "other", "state": "READY"}, {"name": name, "version": "1", "state": state, "reason": reason}])


def test_a_loaded_forecaster_in_state_ready_is_accepted(triton_env):
    fake = triton_env(FakeTriton(index=[index("READY")]))

    asyncio.run(load_model(NAME))

    load, listing = fake.calls
    assert load[0] == f"{URL}/v2/repository/models/{NAME}/load" and load[2] == 5
    assert listing[0] == f"{URL}/v2/repository/index" and listing[2] == 5


def test_the_state_is_awaited_until_the_model_is_ready(triton_env):
    fake = triton_env(FakeTriton(index=[index("LOADING"), index("LOADING"), index("READY")]))

    asyncio.run(load_model(NAME))

    assert len(fake.calls) == 4  # One load and three reads of the index.


def test_a_model_that_is_still_not_ready_at_the_time_limit_fails(triton_env):
    triton_env(FakeTriton(index=[index("LOADING")]))

    with pytest.raises(TritonLoadError, match=r"not READY in Triton after 5 s \(state LOADING\)"):
        asyncio.run(load_model(NAME))


def test_a_model_missing_from_the_index_fails_at_the_time_limit(triton_env):
    triton_env(FakeTriton(index=[reply(200, [{"name": "other", "state": "READY"}])]))

    with pytest.raises(TritonLoadError, match="state unknown"):
        asyncio.run(load_model(NAME))


def test_an_unavailable_model_fails_at_once_with_the_reason_of_triton(triton_env):
    fake = triton_env(FakeTriton(index=[index("UNAVAILABLE", reason="onnx: invalid graph " + "x" * 1000)]))

    with pytest.raises(TritonLoadError) as raised:
        asyncio.run(load_model(NAME))

    message = str(raised.value)
    assert message.startswith(f"Triton could not load forecaster {NAME}: onnx: invalid graph")
    assert len(message) < 400  # The reason is cut.
    assert len(fake.calls) == 2  # No wait.


def test_a_refused_load_fails_with_the_error_of_triton(triton_env):
    fake = triton_env(FakeTriton(load=reply(400, {"error": "failed to load 'x', no version is available"})))

    with pytest.raises(TritonLoadError, match=r"HTTP 400\): failed to load 'x', no version is available"):
        asyncio.run(load_model(NAME))

    assert len(fake.calls) == 1  # The index is not read.


def test_a_refused_load_with_a_body_that_is_not_json_still_gives_a_message(triton_env):
    triton_env(FakeTriton(load=reply(500, None, text="Internal error")))

    with pytest.raises(TritonLoadError, match="Internal error"):
        asyncio.run(load_model(NAME))


@pytest.mark.parametrize("error", [httpx.ConnectError("refused"), httpx.ReadTimeout("slow")])
def test_an_unreachable_triton_fails_the_load(triton_env, error):
    triton_env(FakeTriton(load=error))

    with pytest.raises(TritonLoadError, match="Triton cannot be reached"):
        asyncio.run(load_model(NAME))


def test_another_request_error_fails_the_load(triton_env):
    triton_env(FakeTriton(load=httpx.DecodingError("bad")))

    with pytest.raises(TritonLoadError, match="The request to Triton failed"):
        asyncio.run(load_model(NAME))


@pytest.mark.parametrize("answer", [reply(500, {"error": "x"}), reply(200, {"not": "a list"}), reply(200, None)])
def test_an_unusable_index_fails_the_load(triton_env, answer):
    triton_env(FakeTriton(index=[answer]))

    with pytest.raises(TritonLoadError, match="unusable repository index"):
        asyncio.run(load_model(NAME))


# ---------------------------------------------------------------------------
# unload_model: best effort, never raises
# ---------------------------------------------------------------------------


def test_unload_calls_the_unload_endpoint_of_the_model(triton_env):
    fake = triton_env(FakeTriton(load=reply(200, {})))

    asyncio.run(unload_model(NAME))

    assert fake.calls == [(f"{URL}/v2/repository/models/{NAME}/unload", None, 5)]


def test_unload_logs_and_returns_when_triton_refuses(triton_env, caplog):
    triton_env(FakeTriton(load=reply(400, {"error": "unknown model"})))

    asyncio.run(unload_model(NAME))

    assert any("did not unload" in r.getMessage() and "HTTP 400" in r.getMessage() for r in caplog.records)


@pytest.mark.parametrize("error", [httpx.ConnectError("refused"), httpx.ReadTimeout("slow"), RuntimeError("boom")])
def test_unload_swallows_every_error(triton_env, caplog, error):
    triton_env(FakeTriton(load=error))

    asyncio.run(unload_model(NAME))

    assert any("Could not unload" in r.getMessage() for r in caplog.records)
