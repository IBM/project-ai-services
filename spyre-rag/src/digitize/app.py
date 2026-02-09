from enum import Enum
from fastapi import FastAPI, UploadFile, File, Form, HTTPException, Depends
import os
from pydantic import BaseModel, ValidationError
from typing import List, Optional

app = FastAPI(title="Digitize documents API")

class OutputFormat(str, Enum):
    TEXT = "text"
    MD = "md"
    JSON = "json"

class DigitizeRequest(BaseModel):
    ingest: bool
    output_format: OutputFormat = OutputFormat.JSON

def get_payload(payload: str = Form(None)) -> Optional[DigitizeRequest]:
    if payload is None:
        return None
    try:
        return DigitizeRequest.model_validate_json(payload)
    except (ValueError, ValidationError) as e:
        raise HTTPException(status_code=422, detail=f"Invalid JSON payload: {str(e)}")

@app.post("/v1/digitize")
async def digitize_document(
    payload: Optional[DigitizeRequest] = Depends(get_payload),
    files: List[UploadFile] = File(default=[])
):
    try:
        if payload.ingest:
            # Handle the ingestion logic here
            return {"job_id": "UUID of the ingest digitization job"}
        else:
            # Handle the logic to return the digitized document in the requested format
            return {"document": {}}
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

@app.get("/v1/digitize/{job_id}")
def get_digitization_status(job_id: str):
    try:
        # Placeholder for actual status retrieval logic
        return {"job_id": job_id, "status": "In Progress"}
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

@app.get("/v1/health")
def health_check():
    return {"status": "ok"}

if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=os.getenv("PORT", 4000))
