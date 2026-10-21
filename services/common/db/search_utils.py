"""
Shared helpers for case-insensitive substring search on SQLAlchemy columns.

Mirrors the catalog server's Go ``escapeLikePattern`` / ``buildSearchClause``
helpers so that a ``?q=``/``?name=`` search behaves the same whichever
service evaluates it.

Sample usage::

    from common.db.search_utils import ilike_contains

    # ?q=sftp        → ILIKE '%sftp%'         plain substring, case-insensitive
    # ?q=my_conn     → ILIKE '%my\\_conn%'     underscore treated as literal
    # ?q=100%        → ILIKE '%100\\%%'        percent treated as literal
    # ?q=            → skip the filter (guard with ``if q`` in the caller)

    def list_connectors(db: Session, q: str | None = None):
        query = db.query(Connector)
        if q:
            query = query.filter(ilike_contains(Connector.name, q))
        return query.all()
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
