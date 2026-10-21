from sqlalchemy import Column, String, select
from sqlalchemy.dialects import postgresql
from sqlalchemy.orm import declarative_base

from common.db.search_utils import escape_like, ilike_contains

Base = declarative_base()


class _Item(Base):
    __tablename__ = "items"
    id = Column(String, primary_key=True)
    name = Column(String)


class TestEscapeLike:
    def test_plain_text_unchanged(self):
        assert escape_like("sftp") == "sftp"

    def test_escapes_percent(self):
        assert escape_like("100%") == "100\\%"

    def test_escapes_underscore(self):
        assert escape_like("my_conn") == "my\\_conn"

    def test_escapes_backslash_first(self):
        # Backslash must be doubled before wildcards are escaped, otherwise the
        # escape characters added for '%' / '_' would themselves be doubled.
        assert escape_like("a\\b") == "a\\\\b"
        assert escape_like("\\%") == "\\\\\\%"

    def test_empty_string(self):
        assert escape_like("") == ""


class TestIlikeContains:
    def _compile(self, term):
        stmt = select(_Item).where(ilike_contains(_Item.name, term))
        return stmt.compile(dialect=postgresql.dialect())

    def test_uses_ilike_with_escape_clause(self):
        compiled = self._compile("sftp")
        sql = str(compiled)
        assert "ILIKE" in sql.upper()
        assert "ESCAPE" in sql.upper()

    def test_wraps_term_in_wildcards_and_escapes_input(self):
        compiled = self._compile("50%_off")
        assert list(compiled.params.values()) == ["%50\\%\\_off%"]
