# RAG

## Ingest
Ingest pipeline requires following components to be up and running.
- **Milvus Stack**
    - Milvus
    - Minio
    - Etcd
- **LLM Serving**
    - vLLM serving an embedding model, preferably `ibm-granite/granite-embedding-278m-multilingual`
    - vLLM serving an LLM, preferably `ibm-granite/granite-3.3-8b-instruct`
### Build
Ingest container image uses rag-base image built [here](../images/rag-base) as base image.
```
podman build -f Containerfile-ingest .
```

### Deploy
To-Do: # Once the image is published, it would be deployed via ai-services cli.

### Usage
Please set the following env vars to feed various input to your ingest application.
```
export EMB_ENDPOINT="http://serving:8001/v1/embeddings"
export EMB_MODEL="ibm-granite/granite-embedding-278m-multilingual"
export EMB_MAX_TOKENS=512
export LLM_ENDPOINT="http://serving:8000"
export LLM_MODEL="ibm-granite/granite-3.3-8b-instruct"
export MILVUS_HOST="mkumatag-milvus"
export MILVUS_PORT=19530
export MILVUS_DB_PREFIX=RAG_DB
export DOCLING_MODELS_DIR=/var/docling-models
export SETTINGS_PATH=/opt/rag/settings.json
export CACHE_DIR=/var/rag_cache
```

Ingest pipeline currently exposes cli containing following commands to ingest your docs as embeddings into Milvus DB as well as cleaning the ingested docs.
```
python -m ingest.cli  -h      
usage: cli.py [-h] {ingest,clean-db} ...

Data Ingestion CLI

positional arguments:
  {ingest,clean-db}
    ingest           Ingest the DOCs
    clean-db         Clean the DB

options:
  -h, --help         show this help message and exit
```


