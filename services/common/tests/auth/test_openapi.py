"""The OpenAPI document declares the bearer scheme on protected operations only."""

from fastapi import FastAPI
from fastapi.testclient import TestClient

from common.auth import AuthMiddleware
from common.auth.openapi import SCHEME_NAME, add_bearer_auth_to_openapi


def _schema(service="chat"):
    app = FastAPI(title="t", description="desc", docs_url="/docs", redoc_url=None)

    @app.get("/health")
    def health():
        return {}

    @app.get("/v1/things")
    def things():
        return {}

    app.add_middleware(AuthMiddleware)
    add_bearer_auth_to_openapi(app, service=service)
    return TestClient(app).get("/openapi.json").json()


def test_bearer_scheme_is_declared():
    scheme = _schema()["components"]["securitySchemes"][SCHEME_NAME]
    assert scheme["type"] == "http"
    assert scheme["scheme"] == "bearer"


def test_protected_operations_require_the_scheme_and_public_ones_do_not():
    paths = _schema()["paths"]
    assert paths["/v1/things"]["get"]["security"] == [{SCHEME_NAME: []}]
    assert "security" not in paths["/health"]["get"]


def test_page_description_explains_how_to_get_a_token():
    description = _schema("similarity")["info"]["description"]
    assert description.startswith("desc")
    assert "ai-services catalog service-token --service similarity" in description


def test_authorize_dialog_explains_how_to_get_a_token_for_this_service():
    scheme = _schema("digitize")["components"]["securitySchemes"][SCHEME_NAME]
    assert "ai-services catalog login" in scheme["description"]
    assert "ai-services catalog service-token --service digitize" in scheme["description"]
