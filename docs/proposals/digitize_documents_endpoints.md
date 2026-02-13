# Digitize Documents Service 
 
## Current Design: 

User can interact with document ingestion pipeline via cli commands and can run the below command to run the ingestion(convert/process/chunk/index) after placing the documents in the expected directory of the host i.e. /var/lib/ai-services/applications/<app-name>/docs. 
```
ai-services application start <app-name> --pod=<app-name>--ingest-docs 
```
In case if user wants to clean it up, can run below command which will clean up the vdb & local cache of processed files. 
```
ai-services application start <app-name> --pod=<app-name>--clean-docs 
```
There is no option to just convert and not ingest the converted content into the vdb. 
 
## Proposal: 
 
As per the requirement from PM, need to convert cli into microservice which expose REST endpoints to do the digitize document service’s tasks, which are to
- Convert the file from source format pdf to output format and return the result
- Ingest the document after the conversion by going through following tasks 
    - process text & table 
    - chunking 
    - create embeddings & index it to vdb. 
- Also need to expose the service to the outside world for the end user consumption. Port 4000 can be considered
 
## Endpoints: 

| Method | Endpoint | Description | Content-Type | Expected Success |
| :--- | :--- | :--- | :--- | :--- |
| **POST** | `/v1/digitizations` | Synchronous PDF conversion. | `multipart/form-data` | `200 OK` |
| **POST** | `/v1/ingestions` | Asynchronous document ingestion on a background process. | `multipart/form-data` | `202 Accepted` |
| **GET** | `/v1/ingestions` | Retrieve the status of currently ingested documents. | `application/json` | `200 OK` |
| **GET** | `/v1/documents` | Retrieve the list of currently ingested documents with its metadata. | `application/json` | `200 OK` |
| **GET** | `/v1/documents/{document_id}` | Retrieve metadata of a specific ingested document. | `application/json` | `200 OK` |
| **DELETE** | `/v1/documents/{document_id}` | Remove a specific document from vdb & clear its cache | N/A | `204 No Content` |
| **DELETE** | `/v1/documents` | Bulk delete all documents from vdb & cache. | N/A | `204 No Content` |

---

### POST /v1/digitizations - Synchronous conversion

**Content Type:** multipart/form-data 
 
**Query Params:**
- output_format - str  - Optional param to mention the required output format of the pdf, Options: 'md/text/json', Default: 'json'

**Description:**
- User can pass the pdf directly as a byte stream and API server should convert the file into output_format(default: json) and return the result. 
- User should pass only one file at a time for conversion and it should not exceed 100 pages, since it would take lot of time to convert and cannot be done on a sync network call. 
- Why 100 pages?
    - This is something we need to assess and get feedback from PM to decide the number of pages to be supported. To get started on a number I mentioned 100 since that is not too much and not too small as well. 
    - The docling serve API project https://github.com/docling-project/docling-serve currently returns the result irrespective of the number of pages for the sync conversion requests. So we can also follow similar fashion to return the results.
    - Also from our experience 100 page pdf can be converted within couple of mins. Hence the size.
- Rate limiter would be added around conversion job to limit the number of conversion happen in parallel

**Sample curl:**
```
> curl -X 'POST' \ 
  'http://localhost:4000/v1/digitizations?output_format=md' \
  -F 'file=@/path/to/file.pdf'
> 
``` 
**Response codes:** 

| Status Code | Description | Details |
| :--- | :--- | :--- |
| **200 OK** | Success | Request successful. Returns `{ "result": ... }`. |
| **400 Bad Request** | Missing File | No file was attached to the request. |
| **400 Bad Request** | Multiple Files | Only one file is permitted per request. |
| **415 Unsupported Media Type** | Invalid Format | File must be a valid, non-corrupt PDF. |
| **413 Payload Too Large** | Page Limit Exceeded | The number of pages exceeds the maximum allowed limit. |
| **429 Too Many Requests** | Rate Limit | Request denied due to high volume (throttling). |
| **500 Internal Server Error** | Server Error | An unexpected error occurred on the server side. | 

---

### POST /v1/ingestions - Asynchronous ingestion

**Content Type:** multipart/form-data 

**Description:**
- User can send single or multiple files on the request and the ingestion will happen in a background process
- Only one ingestion will be allowed at a time
- App server should do following things on receiving the request: 
    - Validate no lock file exist already to ensure there is no ingestion in progress. 
    - Start the ingestion process in a background process  
    - Create a LOCK file in /var/lib/ai-services/applications/<app-name>/cache/
    - Create /var/lib/ai-services/applications/<app-name>/cache/status.json to manage/view the status of ingestion 
    - End the request 202 Accepted response
- Background ingestion process should write the status into status.json like following information 
    - Current stage(conversion/processing/chunking/indexing) 
    - Stats of current stage (timings, number of pages, tables …) 
    - Once done with all the stages, it should remove the LOCK file and conclude the job. 

**Sample curl:**
```
> # Ingest the attached files 
> curl -X 'POST' \ 
  'http://localhost:4000/v1/ingestions' \
  -F 'file=@/path/to/file1.pdf' \ 
  -F 'file=@/path/to/file2.pdf' \ 
> 
```
**Response codes:** 

| Status Code | Description | Details |
| :--- | :--- | :--- |
| **202 Accepted** | Success | Request accepted. |
| **400 Bad Request** | Missing File | No files were attached to the request. |
| **400 Bad Request** | Duplicate Files | Request contains files with duplicate names. |
| **415 Unsupported Media Type** | Invalid Format | File must be a valid, non-corrupt PDF. |
| **409 Conflict** | Ingestion Busy | Request conflicts with an ingestion process already in progress. |
| **429 Too Many Requests** | Rate Limit | Request denied due to rate limiting. |
| **500 Internal Server Error** | Server Error | An unexpected error occurred on the server side. |

---

### GET /v1/ingestions
- Returns status of submitted ingestions
- If there are more than one submissions available, will return all

**Query Params:**
- latest - bool  - Optional param to return the latest ingestion status

**Sample curl:**
```
> curl \ 
  'http://localhost:4000/v1/ingestions
>  
```

**Response codes:** 
| Status Code | Description | Details |
| :--- | :--- | :--- |
| **200 OK** | Success | Returns current/last ingestion status, stats, and individual doc states. |
| **404 Not Found** | No Job Found | No ingestion found. |
| **500 Internal Server Error** | Server Error | Internal failure while retrieving ingestion metrics. |

**Sample response:**

```
[
    {
        "status": "completed",
        "created_at": "2025-12-10T16:40:00Z",
        "total_pages": 123,
        "total_tables": 20,
        "documents": {
            "pdf11": {...}
        }
    },
    {
        "status": "partial",
        "created_at": "2026-01-10T10:00:00Z",
        "total_pages": 10,
        "total_tables": 5,
        "documents": {
            "pdf1": {...}
        }
    }
]
```

**With latest=True**
```
{
    "status": "partial",
    "created_at": "2026-01-10T10:00:00Z",
    "total_pages": 10,
    "total_tables": 5,
    "documents": {
        "pdf1": { 
            "status": "chunking", // possible values: conversion, processing, chunking, indexing  
            "stats": { 
                "num_pages": 10,
                "num_tables": 5,
                "num_chunks": 25, 
                "timings": { 
                    "conversion": 12.5, 
                    "processing": 30.2, 
                    "chunking": 15.0, 
                    "indexing": 20.3 
                } 
            } 
        }, 
        "pdf2": { 
            "status": "error", 
            "stats": { 
                "num_pages": 0, 
                "timings": {} 
            } 
        } 
    }
} 
```

---

### GET /v1/documents
- Returns the pdf documents currently ingested into the vector db

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
| **400 Bad Request** | No Data | No ingested documents are currently available in the system. |
| **500 Internal Error** | Server Failure | Failure to query the Vector Database or access the local storage record. |

**Sample response:**
```
{
    "documents": [ 
        {
            "name": "file1.pdf",
            "id": "c7b2ee21-ccc2-5d93-9865-7fcea2ea9623",
            "pages": 120,
            "tables": 5
        },
        {
            "name": "file2.pdf",
            "id": "6083ecba-dd7e-572e-8cd5-5f950d96fa54",
            "pages": 554,
            "tables": 47
        }
    ]
}
```

---

### GET /v1/documents/{document_id}
- Returns the metadata of the pdf document's id specified in the request if it is ingested into the vector db

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
    "name": "file2.pdf",
    "id": "6083ecba-dd7e-572e-8cd5-5f950d96fa54",
    "pages": 554,
    "tables": 47
}
```

---

### DELETE /v1/documents/{document_id}
- Ensure there is no ingestion happening currently by checking the LOCK file
- Remove the vectors of a specific document in vdb and clean up the local cache generated for the document

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
| **409 Conflict** | Resource Locked | Action denied; an ingestion job is currently active (Lock detected). |
| **500 Internal Error** | Server Failure | Error occurred while communicating with VDB or deleting cache files. |

---

### DELETE /v1/documents
- Ensure there is no ingestion happening currently by checking the lock file
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
| **409 Conflict** | Resource Locked | Action denied; an ingestion job is currently active (Lock detected). |
| **500 Internal Error** | Server Failure | Failure occurred during VDB truncation or recursive file deletion. |

---

### Assumptions:
- Digitize documents pod/container mounted with a Read/Write persistent volume and data persists over restarts to store cached results
- In case of multiple replicas, same volume should be shared to maintain the ingestion job status
- During ingestion
    - User should pass files with unique names
    - In case user pass same file again, vdb will be upserted

## Stretch/Future:

### To support asynchronous multiple files conversion:
**POST /v1/digitizations/async**
**Content Type:** multipart/form-data 
 
**Query Params:**
- output_format - str  - Optional param to mention the required output format of the pdf, Options: 'md/text/json', Default: 'json'

**Description:**
- To run the conversion job asynchronously over multiple files and the results can be collected using result endpoints described below.

**Sample curl:**
```
> curl -X 'POST' \ 
  'http://localhost:4000/v1/digitizations/async?output_format=text' \ 
  -F 'file=@/path/to/file1.pdf' \ 
  -F 'file=@/path/to/file2.pdf' \ 
> 
```
**Response codes:**

| Status Code | Description | Details |
| :--- | :--- | :--- |
| **202 Accepted** | Success | Request accepted. |
| **400 Bad Request** | Missing File | No files were attached to the request. |
| **400 Bad Request** | Duplicate Files | Request contains files with duplicate names. |
| **415 Unsupported Media Type** | Invalid Format | File must be a valid, non-corrupt PDF. |
| **429 Too Many Requests** | Rate Limit | Request denied due to rate limiting. |
| **500 Internal Server Error** | Server Error | An unexpected error occurred on the server side. |

**Sample response:**
```
{
    "job_id": "ba1e0105-15d7-5c41-8474-d87b8540c1d9"
}
```
---

**GET /v1/digitizations/async/{job_id}**
- To retrieve current status of the given job id 

**Sample curl:**
```
> curl \ 
  'http://localhost:4000/v1/digitizations/async/4ae018be-9674-4bf9-be8b-af311a5d4d92
> 
```
**Response codes:** 
| Status Code | Description | Details |
| :--- | :--- | :--- |
| **200 OK** | Success| Returns conversion status, stats, and individual doc states. |
| **404 Not Found** | No Job Found | Conversion might not be submitted or invalid job id is used |
| **500 Internal Server Error** | Server Error | Internal failure while retrieving conversion results |

**Sample response:**
TBD

---

**GET /v1/digitizations/async/{job_id}/result**
- To retrieve results from a particular job 
- If there are multiple files submitted in the request, result should contain key value pairs of file names and the result

**Sample curl:**
```
> curl \ 
  'http://localhost:4000/v1/digitizations/async/4ae018be-9674-4bf9-be8b-af311a5d4d92/result
> 
```
**Response codes:** 
| Status Code | Description | Payload / Scenario |
| :--- | :--- | :--- |
| **200 OK** | Success | Job is still in progress / Returns results of files submitted for conversion if it is completed |
| **404 Not Found** | No Job Found | Conversion might not be submitted or invalid job id is used |
| **500 Internal Server Error** | Server Error | Internal failure while retrieving conversion results |

**Sample response:**
TBD

---

**GET /v1/digitizations/{job_id}/result/{document_id}**
- To retrieve results of a particular document in a submitted job

**Sample curl:**
```
> curl \ 
  'http://localhost:4000/v1/digitizations/async/4ae018be-9674-4bf9-be8b-af311a5d4d92/result/
> 
```
**Response codes:** 
| Status Code | Description | Payload / Scenario |
| :--- | :--- | :--- |
| **200 OK** | InProgress/Success | Job is still in progress / Returns results of file name specified in the request if it is converted |
| **404 Not Found** | No Job Found | Conversion might not be submitted or invalid job id is used |
| **404 Not Found** | No File Found | File name passed might not belong to the job id mentioned |
| **500 Internal Server Error** | Server Error | Internal failure while retrieving conversion results |

**Sample response:**
TBD

---