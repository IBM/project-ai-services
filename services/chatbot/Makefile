IMAGE=chatbot-service
TAG?=v0.0.21

run:
	PORT=$${PORT:-5000} /var/venv/bin/python -m uvicorn app:app --host 0.0.0.0 --port $$PORT
.PHONY: run
