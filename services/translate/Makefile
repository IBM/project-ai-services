IMAGE=translate-service
TAG?=v0.0.1

run:
	PORT=$${PORT:-9000} /var/venv/bin/python -m uvicorn app:app --host 0.0.0.0 --port $$PORT
.PHONY: run
