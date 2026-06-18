IMAGE=digitize-service
TAG?=v0.0.26

run:
	PORT=$${PORT:-4000} /var/venv/bin/python -m uvicorn app:app --host 0.0.0.0 --port $$PORT
.PHONY: run
