"""Per-route role checks. Authentication itself is done by AuthMiddleware."""

from fastapi import HTTPException, Request

from common.auth.verifier import Principal


def get_principal(request: Request) -> Principal:
    """Return the verified Principal; fail closed (401) if the middleware did not run."""
    principal = getattr(request.state, "principal", None)
    if not isinstance(principal, Principal):
        raise HTTPException(
            status_code=401,
            detail="Authentication required",
            headers={"WWW-Authenticate": "Bearer"},
        )
    return principal


def require_role(role: str):
    """Build a dependency that allows the request only if the token grants ``role``."""

    def dependency(request: Request) -> Principal:
        principal = get_principal(request)
        if not principal.has_role(role):
            raise HTTPException(status_code=403, detail="Insufficient permissions")
        return principal

    return dependency
