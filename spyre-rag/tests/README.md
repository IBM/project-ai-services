# Spyre-RAG Chatbot Testing Guide

This comprehensive guide covers the setup, configuration, and execution of tests for the spyre-rag chatbot application.

## Table of Contents

- [Overview](#overview)
- [Test Structure](#test-structure)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Running Tests](#running-tests)
- [Test Coverage](#test-coverage)
- [Writing New Tests](#writing-new-tests)
- [Continuous Integration](#continuous-integration)
- [Troubleshooting](#troubleshooting)

## Overview

The test suite provides comprehensive coverage for the chatbot application (`spyre-rag/src/chatbot/app.py`), including:

- **Unit Tests**: Test individual components in isolation
- **Integration Tests**: Test interactions between components
- **API Endpoint Tests**: Test all REST API endpoints
- **Middleware Tests**: Test request/response middleware
- **Initialization Tests**: Test application startup and configuration


## Test Structure

```
spyre-rag/
├── src/
│   └── chatbot/
│       └── app.py                    # Application code
├── tests/
│   ├── __init__.py
│   ├── conftest.py                   # Shared fixtures and configuration
│   ├── TESTING_PLAN.md               # Detailed testing plan
│   ├── README.md                     # This file
│   └── unit/
│       ├── __init__.py
│       └── chatbot/
│           ├── __init__.py
│           ├── test_initialization.py  # Initialization tests
│           ├── test_middleware.py      # Middleware tests
│           └── test_endpoints.py       # API endpoint tests
├── pytest.ini                        # Pytest configuration
└── requirements-test.txt             # Test dependencies
```

## Prerequisites

### System Requirements

- **Python**: 3.9 or higher
- **pip**: Latest version
- **Virtual Environment**: Recommended (venv or conda)

### Application Dependencies

The chatbot application requires:
- FastAPI
- Uvicorn
- Pydantic
- Lingua (language detection)
- Various internal modules (common.*, chatbot.*)

## Installation

### Step 1: Set Up Python Environment

```bash
# Navigate to the spyre-rag directory
cd spyre-rag

# Create a virtual environment (recommended)
python -m venv venv

# Activate the virtual environment
# On Linux/macOS:
source venv/bin/activate
# On Windows:
venv\Scripts\activate
```

### Step 2: Install Application Dependencies

```bash

# If requirements.txt doesn't exist, install core dependencies:
pip install fastapi uvicorn pydantic lingua-language-detector tqdm cohere numpy
```

### Step 3: Install Test Dependencies

```bash
# Install all test dependencies
pip install -r requirements-test.txt
```

This installs:
- `pytest` - Testing framework
- `pytest-asyncio` - Async test support
- `pytest-cov` - Coverage reporting
- `pytest-mock` - Enhanced mocking
- `httpx` - HTTP client for FastAPI testing
- `faker` - Test data generation
- `freezegun` - Time-based testing
- And more...

### Step 4: Verify Installation

```bash
# Verify pytest is installed
pytest --version

# Verify all dependencies
pip list | grep pytest
```

## Running Tests

### Basic Test Execution

```bash
# Run all tests
pytest

# Run with verbose output
pytest -v

# Run with extra verbose output (shows test names)
pytest -vv
```

### Run Specific Test Categories

```bash
# Run only unit tests
pytest -m unit

# Run only integration tests
pytest -m integration

# Run tests that don't require external services
pytest -m "not requires_db and not requires_llm"
```

### Run Specific Test Files

```bash
# Run initialization tests only
pytest tests/unit/chatbot/test_initialization.py

# Run middleware tests only
pytest tests/unit/chatbot/test_middleware.py

# Run endpoint tests only
pytest tests/unit/chatbot/test_endpoints.py
```

### Run Specific Test Classes or Functions

```bash
# Run a specific test class
pytest tests/unit/chatbot/test_endpoints.py::TestHealthEndpoint

# Run a specific test function
pytest tests/unit/chatbot/test_endpoints.py::TestHealthEndpoint::test_health_returns_200

# Run tests matching a pattern
pytest -k "health"
pytest -k "test_successful"
```

### Parallel Test Execution

```bash
# Run tests in parallel (requires pytest-xdist)
pytest -n auto

# Run with specific number of workers
pytest -n 4
```

### Stop on First Failure

```bash
# Stop after first failure
pytest -x

# Stop after N failures
pytest --maxfail=3
```

## Test Coverage

### Generate Coverage Reports

```bash
# Run tests with coverage
pytest --cov=src/chatbot

# Generate HTML coverage report
pytest --cov=src/chatbot --cov-report=html

# Generate XML coverage report (for CI/CD)
pytest --cov=src/chatbot --cov-report=xml

# Generate terminal report with missing lines
pytest --cov=src/chatbot --cov-report=term-missing
```

### View Coverage Reports

```bash
# Open HTML coverage report
# On Linux/macOS:
open htmlcov/index.html
# On Windows:
start htmlcov/index.html

# Or use Python's HTTP server:
cd htmlcov && python -m http.server 8000
# Then open http://localhost:8000 in your browser
```

### Coverage Configuration

Coverage settings are configured in `pytest.ini`:
- **Target**: 90%+ coverage
- **Branch Coverage**: Enabled
- **Reports**: HTML, XML, and terminal

### Interpreting Coverage Results

```
Name                          Stmts   Miss Branch BrPart  Cover   Missing
-------------------------------------------------------------------------
src/chatbot/app.py              250     15     45      3    92%   45-47, 123
```

- **Stmts**: Total statements
- **Miss**: Missed statements
- **Branch**: Total branches
- **BrPart**: Partially covered branches
- **Cover**: Coverage percentage
- **Missing**: Line numbers not covered

## Writing New Tests

### Test File Naming Convention

- Test files: `test_*.py`
- Test classes: `Test*`
- Test functions: `test_*`

### Example Test Structure

```python
import pytest
from unittest.mock import Mock, patch

@pytest.mark.unit
class TestMyFeature:
    """Tests for my feature"""
    
    def test_successful_case(self, test_client, mock_dependency):
        """Test successful execution"""
        # Arrange
        expected_result = {"status": "ok"}
        
        # Act
        response = test_client.get("/my-endpoint")
        
        # Assert
        assert response.status_code == 200
        assert response.json() == expected_result
    
    def test_error_case(self, test_client, monkeypatch):
        """Test error handling"""
        # Arrange
        mock_func = Mock(side_effect=Exception("Error"))
        monkeypatch.setattr("module.function", mock_func)
        
        # Act
        response = test_client.get("/my-endpoint")
        
        # Assert
        assert response.status_code == 500
```

### Using Fixtures

Fixtures are defined in `conftest.py` and can be used in any test:

```python
def test_with_fixtures(test_client, mock_vectorstore, sample_documents):
    """Test using multiple fixtures"""
    # test_client: FastAPI test client
    # mock_vectorstore: Mocked vector database
    # sample_documents: Sample document data
    pass
```

### Async Tests

```python
@pytest.mark.asyncio
async def test_async_function():
    """Test async function"""
    result = await my_async_function()
    assert result is not None
```

### Mocking Best Practices

```python
# Mock at the boundary (external calls)
def test_with_mock(monkeypatch):
    mock_external = Mock(return_value="mocked")
    monkeypatch.setattr("module.external_call", mock_external)
    
    # Test your code
    result = my_function()
    
    # Verify mock was called
    mock_external.assert_called_once()
```

## Continuous Integration

### GitHub Actions Example

```yaml
name: Tests
on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Set up Python
        uses: actions/setup-python@v4
        with:
          python-version: '3.11'
      
      - name: Install dependencies
        run: |
          pip install -r requirements.txt
          pip install -r requirements-test.txt
      
      - name: Run tests
        run: |
          pytest --cov --cov-report=xml
      
      - name: Upload coverage
        uses: codecov/codecov-action@v3
        with:
          file: ./coverage.xml
```

### GitLab CI Example

```yaml
test:
  image: python:3.11
  script:
    - pip install -r requirements.txt
    - pip install -r requirements-test.txt
    - pytest --cov --cov-report=xml
  coverage: '/TOTAL.*\s+(\d+%)$/'
  artifacts:
    reports:
      coverage_report:
        coverage_format: cobertura
        path: coverage.xml
```

## Troubleshooting

### Common Issues and Solutions

#### 1. Import Errors

**Problem**: `ModuleNotFoundError: No module named 'chatbot'`

**Solution**:
```bash
# Ensure you're in the correct directory
cd spyre-rag

# Add src to PYTHONPATH
export PYTHONPATH="${PYTHONPATH}:${PWD}/src"

# Or run tests from the correct location
cd spyre-rag && pytest
```

#### 2. Fixture Not Found

**Problem**: `fixture 'test_client' not found`

**Solution**:
- Ensure `conftest.py` is in the correct location
- Check that `conftest.py` is not empty
- Verify fixture is defined correctly

#### 3. Async Test Failures

**Problem**: `RuntimeError: Event loop is closed`

**Solution**:
```bash
# Ensure pytest-asyncio is installed
pip install pytest-asyncio

# Check pytest.ini has asyncio_mode = auto
```

#### 4. Coverage Not Working

**Problem**: Coverage shows 0% or incorrect values

**Solution**:
```bash
# Ensure pytest-cov is installed
pip install pytest-cov

# Use correct source path
pytest --cov=src/chatbot --cov-report=term-missing
```

#### 5. Tests Hang or Timeout

**Problem**: Tests don't complete

**Solution**:
```bash
# Add timeout to pytest.ini or use command line
pytest --timeout=300

# Check for infinite loops or blocking calls
# Use -vv to see which test is hanging
pytest -vv
```

### Debug Mode

```bash
# Run with Python debugger
pytest --pdb

# Drop into debugger on first failure
pytest -x --pdb

# Show local variables on failure
pytest -l

# Show print statements
pytest -s
```

### Verbose Logging

```bash
# Enable logging output
pytest --log-cli-level=DEBUG

# Show all output
pytest -vv -s --log-cli-level=DEBUG
```

## Best Practices

### 1. Test Isolation
- Each test should be independent
- Use fixtures for setup/teardown
- Don't rely on test execution order

### 2. Test Naming
- Use descriptive names: `test_<function>_<scenario>_<expected_result>`
- Example: `test_get_reference_docs_empty_prompt_returns_400`

### 3. Assertions
- Use specific assertions
- Include helpful error messages
- Test both success and failure cases

### 4. Mocking
- Mock at the boundary (external calls)
- Don't mock the code under test
- Verify mock calls when relevant

### 5. Documentation
- Add docstrings to test functions
- Comment complex setup
- Keep tests readable

## Additional Resources

- [Pytest Documentation](https://docs.pytest.org/)
- [FastAPI Testing](https://fastapi.tiangolo.com/tutorial/testing/)
- [Coverage.py Documentation](https://coverage.readthedocs.io/)


## Contributing

When adding new tests:
1. Follow the existing structure. Make sure you tell Bob to read conftest.py and README before writing new tests.
2. Use appropriate markers (`@pytest.mark.unit`, etc.)
3. Add fixtures to `conftest.py` if reusable
4. Update this README if adding new test categories
5. Ensure tests pass before committing

---

**Test Framework Version**: pytest 7.4.0+
**Python Version**: 3.9+
