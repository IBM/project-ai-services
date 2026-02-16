# Digitize Documents Service 
 
## Current Design: 

User can interact with document ingestion pipeline via cli commands and can run the below command to run the ingestion(convert/process/chunk/index) after placing the documents in the expected directory of the host i.e. `/var/lib/ai-services/applications/<app-name>/docs`.
```
ai-services application start <app-name> --pod=<app-name>--ingest-docs 
```
In case if user wants to clean it up, can run below command which will clean up the vdb & local cache of processed files. 
```
ai-services application start <app-name> --pod=<app-name>--clean-docs 
```
There is no option to just convert and not ingest the converted content into the vdb. 
 
## Proposal: 
 
Recognizing the need for a more scalable and accessible architecture, we are moving to convert the current CLI into a microservice that offers REST endpoints for digitize‑document tasks. The microservice will support the following capabilities:

### File Conversion

- Convert the source file (e.g., PDF) into the required output format.
- Return the converted result through the API.

### Document Ingestion Workflow
After conversion, the document will be ingested via a structured processing pipeline that includes:

- Extracting and processing text and tables.
- Chunking the extracted content.
- Generating embeddings and indexing them into the vector database (VDB).

### External Service Exposure

- The microservice will be made accessible for external, end‑user consumption.
- Port 4000 may be used for hosting and exposure.
 
## Endpoints: 

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| **POST** | `/v1/documents` | Uploads files for async processing. Ingestion by default. Accepts `operation` and `output_format` as optional flags. Returns `job_id` to track the processing. |
| **GET** | `/v1/documents/jobs` | Retrieves information about all the jobs. |
| **GET** | `/v1/documents/jobs/{job_id}` | Retrieves information about `job_id`. |
| **GET** | `/v1/documents` | Retrieves all the documents with its metadata. |
| **GET** | `/v1/documents/{id}` | Retrieves metadata of a specific document. |
| **GET** | `/v1/documents/{id}/content` | Retrieves digitized content of the specific document. |
| **DELETE** | `/v1/documents/{id}` | Removes a specific document from vdb & clear its cache. |
| **DELETE** | `/v1/documents` | Bulk deletes all documents from vdb & cache. Required param `confirm=True` to proceed the deletion. |

---

### POST /v1/documents - Async Ingestions/Digitizations

**Content Type:** multipart/form-data 
 
**Query Params:**
- **operation**     - str  - Optional param to mention the operation to do on the attached files. Options: 'ingestion/digitization', Default: 'ingestion'
- **output_format** - str  - Optional param to mention the required output format of the pdf, Options: 'md/text/json', Default: 'json'

**Description:**
- User can pass one or more pdfs directly as a byte stream with the optional query params.
- API Server should do following
- Validate params to identiy whether its a digitization or ingestion operation.
- Validate no `<INGEST/DIGITIZE>.LOCK` file exist to ensure there is no existing process in progress.
- Create `<INGEST/DIGITIZE>.LOCK` file in `/var/lib/ai-services/applications/<app-name>/cache/`.
- Start the ingestion/digitization in a background process.
- Generate a UUID as job_id.
- End the request with returning job_id as response and 202 Accepted status.

- Background ingestion/digitization process should do following in common
    - Start the process pipeline.
    - Atomically write status into `<job_id>_status.json` from main process to avoid race issues 
    - Once done with ingestion, should remove the INGEST.LOCK file and conclude the job.
    - Keep `<job_id>_status.json` for preserving the history.

**Sample curl for ingestion:**
```
> curl -X 'POST' \ 
  'http://localhost:4000/v1/documents' \
  -F 'files=@/path/to/file1.pdf'
  -F 'files=@/path/to/file2.pdf'
> 
``` 
**Sample curl for digitization:**
```
> curl -X 'POST' \ 
  'http://localhost:4000/v1/documents?digitize_only=True&output_format=md' \
  -F 'files=@/path/to/file3.pdf'
  -F 'files=@/path/to/file4.pdf'
> 
``` 

**Response codes:** 

| Status Code | Description | Details |
| :--- | :--- | :--- |
| **202 Accepted** | Success | Request accepted. |
| **400 Bad Request** | Missing File | No file was attached to the request. |
| **400 Bad Request** | Duplicate Files | Request contains files with duplicate names. |
| **415 Unsupported Media Type** | Invalid Format | File must be a valid, non-corrupt PDF. |
| **429 Too Many Requests** | Rate Limit | Request denied due to high volume (throttling). |
| **500 Internal Server Error** | Server Error | An unexpected error occurred on the server side. | 

**Sample response:**

```
{
    "job_id": "c7b2ee21-ccc2-5d93-9865-7fcea2ea9623"
}
```

**Note:**
- `output_format` passed will be ignored in case of ingestion, .
- Users can submit digitized documents for ingestion; if the file hasn't been cleared, the pipeline will use its cached version.

---

### GET /v1/documents/jobs
- Returns status of all the submitted jobs(ingestion/digitization)

**Query Params:**
- latest - bool  - Optional param to return the latest ingestion status

**Sample curl:**
```
> curl \ 
  'http://localhost:4000/v1/document/jobs
>  
```

**Response codes:** 
| Status Code | Description | Details |
| :--- | :--- | :--- |
| **200 OK** | Success | Returns current/last job status, stats, and individual doc states. |
| **500 Internal Server Error** | Server Error | Internal failure while retrieving job information. |

**Sample response:**

```
[
    {
        "job_id": "c7b2ee21-ccc2-5d93-9865-7fcea2ea9623",
        "operation": "ingestion",
        "status": "completed",
        "created_at": "2025-12-10T16:40:00Z",
        "total_pages": 123,
        "total_tables": 20,
        "documents": {
            "pdf11": {...}
        }
    },
    {
        "job_id": "stb34e21-9865-5d93-ccc2-8gcea2ea23456",
        "operation": "digitization",
        "status": "in_progress",
        "created_at": "2026-01-10T10:00:00Z",
        "total_pages": 343,
        "total_tables": 57,
        "documents": {
            "pdf1": {...}
        }
    }
]
```

**With latest=True**
```
[
    {
        "job_id": "stb34e21-9865-5d93-ccc2-8gcea2ea23456",
        "operation": "digitization",
        "status": "in_progress",
        "created_at": "2026-01-10T10:00:00Z",
        "total_pages": 343,
        "total_tables": 57,
        "documents": [
            {
                "id": "c7b2ee21-ccc2-5d93-9865-7fcea2ea9623",
                "name": "file1.pdf",
                "type": "digitization",
                "status": "completed",
                "digitizing_time": 120,
                "processing_time": N/A,
                "chunking_time": N/A,
                "indexing_time": N/A,
                "pages": 210,
                "tables": 10
            },
            {
                "id": "6083ecba-dd7e-572e-8cd5-5f950d96fa54",
                "name": "file2.pdf",
                "type": "digitization",
                "status": "in_progress",
                "digitizing_time": 0,
                "processing_time": N/A,
                "chunking_time": N/A,
                "indexing_time": N/A,
                "pages": 0,
                "tables": 0
            }
        ]
    }
] 
```

---

### GET /v1/documents/jobs/{job_id}
- Returns status of job_id specified.

**Sample curl:**
```
> curl \ 
  'http://localhost:4000/v1/document/jobs/stb34e21-9865-5d93-ccc2-8gcea2ea23456
>  
```

**Response codes:** 
| Status Code | Description | Details |
| :--- | :--- | :--- |
| **200 OK** | Success | Returns current/last job status, stats, and individual doc states. |
| **404 Not Found** | No Job Found | No job Found |
| **500 Internal Server Error** | Server Error | Internal failure while retrieving job information. |

**Sample response:**
```
{
    "job_id": "stb34e21-9865-5d93-ccc2-8gcea2ea23456",
    "operation": "digitization",
    "status": "in_progress",
    "created_at": "2026-01-10T10:00:00Z",
    "total_pages": 343,
    "total_tables": 57,
    "documents": [
        {
            "id": "c7b2ee21-ccc2-5d93-9865-7fcea2ea9623",
            "name": "file1.pdf",
            "type": "digitization",
            "status": "completed",
            "digitizing_time": 120,
            "processing_time": N/A,
            "chunking_time": N/A,
            "indexing_time": N/A,
            "pages": 210,
            "tables": 10
        },
        {
            "id": "6083ecba-dd7e-572e-8cd5-5f950d96fa54",
            "name": "file2.pdf",
            "type": "digitization",
            "status": "in_progress",
            "digitizing_time": 0,
            "processing_time": N/A,
            "chunking_time": N/A,
            "indexing_time": N/A,
            "pages": 0,
            "tables": 0
        }
    ]
}
```

---

### GET /v1/documents
- Returns all the pdf documents processed(ingested/digitized)

**Sample curl:**
```
> curl \ 
  'http://localhost:4000/v1/documents
>  
```
**Response codes:**
| Status Code | Description | Details |
| :--- | :--- | :--- |
| **200 OK** | Success | Returns a JSON list of all ingested documents and their metadata. |
| **500 Internal Error** | Server Failure | Failure to query the Vector Database or access the local storage record. |

**Sample response:**
```
[
    {
        "id": "c7b2ee21-ccc2-5d93-9865-7fcea2ea9623",
        "name": "file1.pdf",
        "type": "digitization",
        "status": "completed",
        "digitizing_time": 120,
        "processing_time": N/A,
        "chunking_time": N/A,
        "indexing_time": N/A,
        "pages": 210,
        "tables": 10
    },
    {
        "id": "6083ecba-dd7e-572e-8cd5-5f950d96fa54",
        "name": "file2.pdf",
        "type": "digitization",
        "status": "in_progress",
        "digitizing_time": 0,
        "processing_time": N/A,
        "chunking_time": N/A,
        "indexing_time": N/A,
        "pages": 0,
        "tables": 0
    },
    {
        "id": "4365eifa-dd7e-8cd5-572e-5f950d96fa54",
        "name": "file2.pdf",
        "type": "ingestion",
        "status": "completed",
        "digitizing_time": 120,
        "processing_time": 300,
        "chunking_time": 10,
        "indexing_time": N/A,
        "pages": 200,
        "tables": 20
    }
]
```

---

### GET /v1/documents/{id}
- Returns specific document's information 

**Sample curl:**
```
> curl \ 
  'http://localhost:4000/v1/documents/6083ecba-dd7e-572e-8cd5-5f950d96fa54
>  
```
**Response codes:**
| Status Code | Description | Details |
| :--- | :--- | :--- |
| **200 OK** | Success | Returns metadata of the pdf's id requested. |
| **400 Bad Request** | No Data | No ingested documents matching the id. |
| **500 Internal Error** | Server Failure | Failure to query the Vector Database or access the local storage record. |

**Sample response:**
```
{
    "id": "4365eifa-dd7e-8cd5-572e-5f950d96fa54",
    "name": "file2.pdf",
    "type": "ingestion",
    "status": "completed",
    "digitizing_time": 120,
    "processing_time": 300,
    "chunking_time": 10,
    "indexing_time": N/A,
    "pages": 200,
    "tables": 20
}
```

---

### GET /v1/documents/{id}/content
- Returns specific document's digitized content

**Sample curl:**
```
> curl \ 
  'http://localhost:4000/v1/documents/6083ecba-dd7e-572e-8cd5-5f950d96fa54
>  
```

**Response codes:**
| Status Code | Description | Details |
| :--- | :--- | :--- |
| **200 OK** | Success | Returns digitized content of requested docuemnt. |
| **202 Accepted** | Success | Digitization is in progress |
| **400 Bad Request** | No Data | No documents matching the id. |
| **500 Internal Error** | Server Failure | Failure to query the Vector Database or access the local storage record. |

**Sample response:**
```
{
    "result": ... // Based on output_format request, result will contain str in case of md/text, dict in case of json output format.
}
```
---

### DELETE /v1/documents/{id}
- Ensure the document is not part of an active job
- Remove the vectors of a specific document in vdb if it is ingested and clean up the local cache generated for the document

**Sample curl:**
```
> curl -X DELETE \ 
  'http://localhost:4000/v1/documents/6083ecba-dd7e-572e-8cd5-5f950d96fa54
>  
```
**Response codes:**

| Status Code | Description | Details |
| :--- | :--- | :--- |
| **204 No Content** | Success | File successfully purged from VDB and local cache. |
| **404 Not Found** | Missing Resource | The specified `{document_id}` does not exist in the system. |
| **409 Conflict** | Resource Locked | Action denied; document is part of an active job. |
| **500 Internal Error** | Server Failure | Error occurred while communicating with VDB or deleting cache files. |

---

### DELETE /v1/documents
- Ensure there is active job.
- Equivalent to clean-db command, will clean up the vdb and remove the local cache.

**Query Params:**
- confirm - bool  - Required param to comfirm the bulk delete

**Sample curl:**
```
> curl -X DELETE\ 
  'http://localhost:4000/v1/documents?confirm=True
>  
```
**Response codes:**

| Status Code | Description | Details |
| :--- | :--- | :--- |
| **204 No Content** | Success | Full cleanup completed; VDB and local cache are now empty. |
| **409 Conflict** | Resource Locked | Action denied; an job is currently active. |
| **500 Internal Error** | Server Failure | Failure occurred during VDB truncation or recursive file deletion. |

---

### Assumptions:
- Digitize documents pod/container mounted with a Read/Write persistent volume and data persists over restarts to store cached results
- In case of multiple replicas, same volume should be shared to maintain the ingestion job status
- During ingestion
    - User should pass files with unique names
    - In case user pass same file again, vdb will be upserted