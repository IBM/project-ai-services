"""Declare the bearer-token scheme in a service's OpenAPI document.

Authentication is enforced by AuthMiddleware, not by FastAPI security dependencies, so
FastAPI does not know the API needs a token. Without a declared scheme, Swagger UI has no
"Authorize" button and "Try it out" sends no Authorization header. This adds the scheme to
the generated document (documentation only; it does not enforce anything), plus help text on
how to get a token. Swagger UI renders both the API description (top of the page) and the
scheme description (inside the Authorize dialog) as Markdown.
"""

from fastapi import FastAPI
from fastapi.openapi.utils import get_openapi

from common.auth.middleware import _PUBLIC_METHODS, DEFAULT_PUBLIC_PATHS

SCHEME_NAME = "BearerAuth"

_STEPS = """\
1. Sign in once: `ai-services catalog login --server <catalog_backend_endpoint> --username <user>`
   (the endpoint is shown by `ai-services catalog info`).
2. Print a token: `ai-services catalog service-token --service {service}`
3. Click **Authorize** and paste the token. Do not type the `Bearer ` prefix.

The token is valid for the `{service}` service only and expires after at most one hour; run step 2
again when it does. `401` means the token is missing, expired or for another service; `403` means
your role does not allow that endpoint."""


def token_help(service: str) -> str:
    """Markdown that explains how to get and use a token for ``service``."""
    return _STEPS.format(service=service)


def add_bearer_auth_to_openapi(app: FastAPI, service: str) -> None:
    """Make ``app.openapi()`` declare a bearer scheme on every non-public operation.

    ``service`` is the catalog service name used in the token instructions (for example ``chat``).
    """
    help_text = token_help(service)

    def custom_openapi():
        if app.openapi_schema:
            return app.openapi_schema

        schema = get_openapi(
            title=app.title,
            version=app.version,
            description=(
                (app.description or "")
                + "\n\n**Authentication**: calling an endpoint needs a bearer token. "
                + "Browsing this page does not.\n\n"
                + help_text
            ),
            routes=app.routes,
            tags=app.openapi_tags,
        )
        schema.setdefault("components", {}).setdefault("securitySchemes", {})[SCHEME_NAME] = {
            "type": "http",
            "scheme": "bearer",
            "bearerFormat": "JWT",
            "description": "**How to get a token**\n\n" + help_text,
        }
        for path, operations in schema.get("paths", {}).items():
            for method, operation in operations.items():
                if method.upper() in _PUBLIC_METHODS and path in DEFAULT_PUBLIC_PATHS:
                    continue
                operation["security"] = [{SCHEME_NAME: []}]

        app.openapi_schema = schema
        return schema

    app.openapi = custom_openapi
