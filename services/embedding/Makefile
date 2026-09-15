IMAGE=embedding-service
TAG?=v1.0.0

run:
	PORT=$${PORT:-7000} /var/venv/bin/python -m uvicorn app:app --host 0.0.0.0 --port $$PORT
.PHONY: run
