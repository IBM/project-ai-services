from glob import glob
import os
import shutil
import numpy as np
import hashlib
from tqdm import tqdm
from opensearchpy import OpenSearch, helpers

from common.emb_utils import Embedding
from common.misc_utils import LOCAL_CACHE_DIR, get_logger
from common.vector_db import VectorStore


logger = get_logger("OpenSearch")

def generate_chunk_id(filename: str, page_content: str, index: int) -> int:
    """
    Generate a unique, deterministic chunk ID based on filename, content, and index.
    """
    base = f"{filename}-{index}-{page_content}"
    hash_digest = hashlib.md5(base.encode("utf-8")).hexdigest()
    chunk_int = int(hash_digest[:16], 16)    # Convert first 64 bits to int
    chunk_id = chunk_int % (2**63)           # Fit into signed 64-bit range
    return np.int64(chunk_id)

class OpensearchNotReadyError(Exception):
    pass

class OpensearchVectorStore(VectorStore):
    def __init__(self):
        self.host = os.getenv("OPENSEARCH_HOST")
        self.port = os.getenv("OPENSEARCH_PORT")
        self.db_prefix = os.getenv("OPENSEARCH_DB_PREFIX", "rag").lower()
        i_name = os.getenv("OPENSEARCH_INDEX_NAME", "default")
        self.index_name = self._generate_index_name(i_name.lower())

        self.client = OpenSearch(
            hosts=[{'host': self.host, 'port': self.port}],
            http_compress=True,
            use_ssl=True,
            http_auth=(os.getenv("OPENSEARCH_USERNAME"), os.getenv("OPENSEARCH_PASSWORD")),
            verify_certs=False,
            ssl_show_warn=False
        )
        self._embedder = None
        self._embedder_config = {}
        self._create_pipeline()

    def _generate_index_name(self, name):
        hash_part = hashlib.md5(name.encode()).hexdigest()
        return f"{self.db_prefix}_{hash_part}"

    def _create_pipeline(self):
        pipeline_body = {
            "description": "Post-processor for hybrid search using RRF",
            "phase_results_processors": [
                {
                    "normalization-processor": {
                        "normalization": {"technique": "min_max"},
                        "combination": {
                            "technique": "arithmetic_mean",
                            "parameters": {
                                "weights": [0.3, 0.7]    # Semantic heavy weights
                            }
                        }
                    }
                }
            ]
        }

        try:
            self.client.search_pipeline.delete(id="hybrid_rrf_pipeline")
            self.client.search_pipeline.put(id="hybrid_rrf_pipeline", body=pipeline_body)
        except Exception as e:
            logger.error(f"Failed to create hybrid rrf search pipeline: {e}")

    def _setup_index(self, dim):
        if self.client.indices.exists(index=self.index_name):
            logger.info(f"Index {self.index_name} already present in vectorstore")
            return

        # index body: setting and mappings
        index_body = {
            "settings": {
                "index": {
                    "knn": True,
                    "knn.algo_param.ef_search": 100
                }
            },
            "mappings": {
                "properties": {
                    "chunk_id": {"type": "long"},
                    "embedding": {
                        "type": "knn_vector",
                        "dimension": dim,
                        "method": {
                           "name": "hnsw",    # HNSW is standard for high performance
                            "space_type": "cosinesimil",
                            "engine": "lucene",
                            "parameters": {
                                "ef_construction": 128,
                                "m": 24
                            }
                        }
                    },
                    "page_content": {
                        "type": "text", 
                        "analyzer": "standard"
                    },
                    "filename": {"type": "keyword"},
                    "type": {"type": "keyword"},
                    "source": {"type": "keyword"},
                    "language": {"type": "keyword"}
                }
            }
        }
        # Create the Index
        self.client.indices.create(index=self.index_name, body=index_body)

    def _ensure_embedder(self, emb_model, emb_endpoint, max_tokens):
        config = {"model": emb_model, "endpoint": emb_endpoint, "max_tokens": max_tokens}
        if self._embedder is None or self._embedder_config != config:
            logger.debug(f"⚙️ Initializing embedder: {emb_model}")
            self._embedder = Embedding(emb_model, emb_endpoint, max_tokens)
            self._embedder_config = config

    def insert_chunks(self, emb_model, emb_endpoint, max_tokens, chunks, batch_size=10):
        if not chunks:
            logger.debug("Nothing to chunk!")
            return

        self._ensure_embedder(emb_model, emb_endpoint, max_tokens)

        sample_embedding = self._embedder.embed_documents([chunks[0]["page_content"]])[0]
        dim = len(sample_embedding)

        self._setup_index(dim)

        logger.debug(f"Inserting {len(chunks)} chunks into OpenSearch...")

        for i in tqdm(range(0, len(chunks), batch_size)):
            batch = chunks[i:i + batch_size]
            page_contents = [doc.get("page_content") for doc in batch]
            embeddings = self._embedder.embed_documents(page_contents)

            filenames = [doc.get("filename", "") for doc in batch]
            types = [doc.get("type", "") for doc in batch]
            sources = [doc.get("source", "") for doc in batch]
            languages = [doc.get("language", "") for doc in batch]

            chunk_ids = [generate_chunk_id(fn, pc, i+j) for j, (fn, pc) in enumerate(zip(filenames, page_contents))]

            # 1. Transform to OpenSearch document format
            actions = []
            for i in range(len(chunk_ids)):
                doc = {
                    "_index": self.index_name,  # The name of your index
                    "_id": str(chunk_ids[i]),   # Use chunk_id as the OpenSearch document ID for upsert logic
                    "_source": {
                        "chunk_id": chunk_ids[i],
                        "embedding": embeddings[i],
                        "page_content": page_contents[i],
                        "filename": filenames[i],
                        "type": types[i],
                        "source": sources[i],
                        "language": languages[i]
                    }
                }
                actions.append(doc)

            # 2. Use the Bulk helper to insert all documents
            # 'stats_only=True' returns a simple count of success/failure
            success, failed = helpers.bulk(self.client, actions, stats_only=True)
            if failed:
                logger.error("failed to insert chunks to OpenSearch vectorstore")

            logger.debug(f"Successfully indexed {success} chunks. Failed: {failed}")

        logger.debug(f"Inserted the chunks into collection.")

    def check_db_populated(self, emb_model, emb_endpoint, max_tokens):
        self._ensure_embedder(emb_model, emb_endpoint, max_tokens)
        if not self.client.indices.exists(index=self.index_name):
            return False
        return True

    def search(self, query, emb_model, emb_endpoint, max_tokens, top_k=5, deployment_type='cpu', mode="hybrid", language='en'):
        if not self.check_db_populated(emb_model, emb_endpoint, max_tokens):
            raise OpensearchNotReadyError(f"Opensearch database is empty. Ingest documents first.")

        query_vector = self._embedder.embed_query(query)

        limit=top_k * 3  # retrieve more for filtering

        if mode == "dense":
            # 1. Define the k-NN search body
            search_body = {
                "size": limit,
                "_source": ["chunk_id", "page_content", "filename", "type", "source", "language"],
                "query": {
                    "knn": {
                        "embedding": {
                            "vector": query_vector,
                            "k": limit,
                            # Efficient pre-filtering
                            "filter": {
                                "term": {"language": language}
                            } if language else {"match_all": {}}
                        }
                    }
                }
            }
            response = self.client.search(index=self.index_name, body=search_body)

            # Format results
            dense_results = [hit["_source"] for hit in response["hits"]["hits"]]
            return dense_results[:top_k]

        elif mode == "sparse":
            # OpenSearch native Sparse Search (BM25 or Neural Sparse)
            # Standard full-text match for sparse/keyword logic
            search_body = {
                "size": limit,
                "_source": ["chunk_id", "page_content", "filename", "type", "source", "language"],
                "query": {
                    "bool": {
                        "must": [
                            {"match": {"page_content": query}}
                        ],
                        "filter": [
                            {"term": {"language": language}}
                        ] if language else []
                    }
                }
            }

            response = self.client.search(index=self.index_name, body=search_body)

            # Format results
            sparse_results = []
            for hit in response["hits"]["hits"]:
                metadata = hit["_source"]
                metadata["score"] = hit["_score"]
                sparse_results.append(metadata)
                
            return sparse_results[:top_k]

        elif mode == "hybrid":
            # OpenSearch Hybrid Query combines Dense (k-NN) and Sparse (Match)
            search_body = {
                "size": top_k, # Final number of results after fusion
                "_source": ["chunk_id", "page_content", "filename", "type", "source", "language"],
                "query": {
                    "hybrid": {
                        "queries": [
                            # 1. Dense Component (k-NN)
                            {
                                "knn": {
                                    "embedding": {
                                        "vector": query_vector,
                                        "k": limit,
                                        "filter": {"term": {"language": language}} if language else None
                                    }
                                }
                            },
                            # 2. Sparse Component (BM25 Lexical)
                            {
                                "bool": {
                                    "must": [{"match": {"page_content": query}}],
                                    "filter": [{"term": {"language": language}}] if language else []
                                }
                            }
                        ]
                    }
                }
            }

            # Execute search using the RRF pipeline
            response = self.client.search(
                index=self.index_name,
                body=search_body,
                search_pipeline="hybrid_rrf_pipeline" # This triggers the server-side RRF
            )

            # Format results
            hybrid_results = []
            for hit in response["hits"]["hits"]:
                metadata = hit["_source"]
                metadata["score"] = hit["_score"] # unified RRF score
                hybrid_results.append(metadata)

            return hybrid_results

    def reset_index(self):
        if self.client.indices.exists(index=self.index_name):
            self.client.indices.delete(index=self.index_name)
            logger.info(f"Collection {self.index_name} deleted.")
        else:
            logger.info(f"Collection {self.index_name} does not exist!")

        # Clear local cache
        files_to_remove = glob(os.path.join(LOCAL_CACHE_DIR, self.index_name+"*"))
        if files_to_remove:
            for file_path in files_to_remove:
                try:
                    if os.path.isdir(file_path):
                        shutil.rmtree(file_path)
                        continue
                    os.remove(file_path)
                except OSError as e:
                    logger.error(f"Error removing {file_path}: {e}")
            logger.info("Local cache cleaned up.")
        else:
            logger.info("Local cache cleaned up already!")
