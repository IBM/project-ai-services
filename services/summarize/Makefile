IMAGE=summarize-service
TAG?=v0.0.15

run:
	PORT=$${PORT:-6000} /var/venv/bin/python -m uvicorn app:app --host 0.0.0.0 --port $$PORT
.PHONY: run
