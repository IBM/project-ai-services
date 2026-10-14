"""
Shared helpers for case-insensitive substring search on SQLAlchemy columns.

Mirrors the catalog server's Go ``escapeLikePattern`` / ``buildSearchClause``
helpers so that a ``?q=``/``?name=`` search behaves the same whichever
service evaluates it.
"""

_LIKE_ESCAPE_CHAR = "\\"


def escape_like(term: str) -> str:
    """Escape LIKE/ILIKE wildcards so '%', '_' and '\\' in user input match literally."""
    return (
        term.replace(_LIKE_ESCAPE_CHAR, _LIKE_ESCAPE_CHAR * 2)
        .replace("%", _LIKE_ESCAPE_CHAR + "%")
        .replace("_", _LIKE_ESCAPE_CHAR + "_")
    )


def ilike_contains(column, term: str):
    """Return a SQLAlchemy clause: ``column ILIKE '%term%' ESCAPE '\\'`` with wildcards escaped."""
    return column.ilike(f"%{escape_like(term)}%", escape=_LIKE_ESCAPE_CHAR)
