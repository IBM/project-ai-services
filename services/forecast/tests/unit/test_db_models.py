"""
Unit tests that keep the ORM models in line with db/scripts/init_schema.sql.

The tables are created from the SQL file, so a model that drifts from it only fails at
runtime. The tests run without a database: the SQL file is read as text.
"""

import re

import pytest
from sqlalchemy import CheckConstraint, Identity

from forecast.db import models
from forecast.db.scripts import init_db

pytestmark = pytest.mark.unit

SCHEMA = init_db.SCHEMA_FILE.read_text()
TABLES = models.Base.metadata.tables


def _table_body(table: str) -> str:
    return re.search(rf"CREATE TABLE IF NOT EXISTS {table} \((.*?)\n\);", SCHEMA, re.S).group(1)


def _sql_columns(table: str) -> dict[str, str]:
    """Return {column name: its SQL definition} for a table of the schema file."""
    columns = {}
    depth = 0  # A multi-line constraint continues until its parentheses close.
    for line in _table_body(table).splitlines():
        line = line.split("--")[0].strip()
        if not line:
            continue
        if depth == 0 and not line.upper().startswith(("CONSTRAINT", "PRIMARY KEY")):
            name, _, definition = line.partition(" ")
            columns[name] = definition
        depth += line.count("(") - line.count(")")
    return columns


def _sql_primary_key(table: str) -> set[str]:
    composite = re.search(r"^\s*PRIMARY KEY \(([^)]*)\)", _table_body(table), re.M)
    if composite:
        return {name.strip() for name in composite.group(1).split(",")}
    return {name for name, definition in _sql_columns(table).items() if "PRIMARY KEY" in definition}


def test_models_cover_exactly_the_expected_tables():
    assert set(TABLES) == init_db.EXPECTED_TABLES


@pytest.mark.parametrize("table", sorted(init_db.EXPECTED_TABLES))
def test_columns_match_the_schema_file(table):
    assert {column.name for column in TABLES[table].columns} == set(_sql_columns(table))


@pytest.mark.parametrize("table", sorted(init_db.EXPECTED_TABLES))
def test_primary_key_matches_the_schema_file(table):
    assert {column.name for column in TABLES[table].primary_key.columns} == _sql_primary_key(table)


@pytest.mark.parametrize("table", sorted(init_db.EXPECTED_TABLES))
def test_nullability_matches_the_schema_file(table):
    sql_columns = _sql_columns(table)
    primary_key = _sql_primary_key(table)
    for column in TABLES[table].columns:
        sql_nullable = "NOT NULL" not in sql_columns[column.name] and column.name not in primary_key
        assert column.nullable == sql_nullable, f"{table}.{column.name}"


@pytest.mark.parametrize("table", sorted(init_db.EXPECTED_TABLES))
def test_foreign_keys_match_the_schema_file(table):
    sql_columns = _sql_columns(table)
    for column in TABLES[table].columns:
        match = re.search(
            r"REFERENCES (\w+)\((\w+)\) ON DELETE (SET NULL|CASCADE)", sql_columns[column.name]
        )
        foreign_keys = list(column.foreign_keys)
        if match is None:
            assert not foreign_keys, f"{table}.{column.name}"
            continue
        assert len(foreign_keys) == 1, f"{table}.{column.name}"
        assert foreign_keys[0].target_fullname == f"{match.group(1)}.{match.group(2)}"
        assert foreign_keys[0].ondelete == match.group(3)


def test_check_constraint_names_match_the_schema_file():
    model_checks = {
        constraint.name
        for table in TABLES.values()
        for constraint in table.constraints
        if isinstance(constraint, CheckConstraint)
    }
    assert model_checks == set(re.findall(r"CONSTRAINT (chk_\w+) CHECK", SCHEMA))


def test_index_names_match_the_schema_file():
    model_indexes = {index.name for table in TABLES.values() for index in table.indexes}
    assert model_indexes == set(re.findall(r"CREATE (?:UNIQUE )?INDEX IF NOT EXISTS (\w+)", SCHEMA))


def test_single_running_index_is_unique_and_partial():
    index = next(i for i in TABLES["jobs"].indexes if i.name == "idx_jobs_single_running")
    assert index.unique
    assert "RUNNING" in str(index.dialect_options["postgresql"]["where"])


def test_seq_is_an_always_identity_column():
    seq = TABLES["jobs"].c.seq
    assert isinstance(seq.identity, Identity) and seq.identity.always
