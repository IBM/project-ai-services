"""
Async job endpoints.

POST   /v1/translate/jobs              — create job
GET    /v1/translate/jobs              — list jobs
GET    /v1/translate/jobs/{job_id}     — job detail
GET    /v1/translate/jobs/{job_id}/result          — get result JSON
GET    /v1/translate/jobs/{job_id}/result/download — download translated file

This stub exists so the router is registered and the import tree is stable across all PRs. Business logic will be added in later PRs.
"""

from fastapi import APIRouter

router = APIRouter()

# Made with Bob
