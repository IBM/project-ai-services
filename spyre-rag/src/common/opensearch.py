from glob import glob
import os
import shutil
import numpy as np
import hashlib
from tqdm import tqdm
from opensearchpy import OpenSearch, helpers

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
        self._create_pipeline()

    def _generate_index_name(self, name):
        hash_part = hashlib.md5(name.encode()).hexdigest()
        return f"{self.db_prefix}_{hash_part}"

    def _create_pipeline(self):
        pipeline_body = {
            "description": "Post-processor for hybrid search",
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
            self.client.search_pipeline.put(id="hybrid_pipeline", body=pipeline_body)
        except Exception as e:
            logger.error(f"Failed to create hybrid search pipeline: {e}")

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

    def insert_chunks(self, chunks, vectors=None, embedder=None, batch_size=10):
        """
        Supports 2 modes of insertion
        1. Pure embedding: pass 'chunks' and 'vectors'
        2. Text chunks: pass 'chunks' and 'embedder' (class instance)
        """

        if not chunks:
            logger.debug("Nothing to chunk!")
            return

        # Handle Pre-computed Vectors if provided
        if vectors is not None:
            final_embeddings = vectors
            # Initialize index using pre-computed vector dimension
            self._setup_index(len(final_embeddings[0]))

        logger.debug(f"Inserting {len(chunks)} chunks into OpenSearch...")

        # Iterate through chunks in batches and insert in bulk
        for i in tqdm(range(0, len(chunks), batch_size)):
            batch = chunks[i:i + batch_size]
            page_contents = [doc.get("page_content") for doc in batch]

            # Generate embeddings only for this specific batch
            if vectors is None and embedder is not None:
                current_batch_embeddings = embedder.embed_documents(page_contents)

                # Initialize index on the first batch if not already done
                if i == 0:
                    dim = len(current_batch_embeddings[0])
                    self._setup_index(dim)
            else:
                # Use the relevant slice from pre-computed vectors
                current_batch_embeddings = final_embeddings[i:i + batch_size]

            # 3. Transform batch to OpenSearch document format
            actions = []
            for j, (doc, emb) in enumerate(zip(batch, current_batch_embeddings)):
                fn = doc.get("filename", "")
                pc = doc.get("page_content", "")

                # Generate chunk ID
                cid = generate_chunk_id(fn, pc, i + j)

                actions.append({
                    "_index": self.index_name,
                    "_id": str(cid),
                    "_source": {
                        "chunk_id": cid,
                        "embedding": emb.tolist() if isinstance(emb, np.ndarray) else emb,
                        "page_content": pc,
                        "filename": fn,
                        "type": doc.get("type", ""),
                        "source": doc.get("source", ""),
                        "language": doc.get("language", "")
                    }
                })

            # Bulk insert the current batch
            success, failed = helpers.bulk(self.client, actions, stats_only=True)
            if failed:
                logger.error(f"Failed to insert {failed} chunks in batch starting at {i}")
                return
            logger.debug(f"Successfully indexed {success} chunks. Failed: {failed}")

        logger.debug(f"Inserted the {len(chunks)} into index.")


    def search(self, query, vector=None, embedder=None, top_k=5, mode="hybrid", language='en'):
        """
        Supported search modes: dense(semantic search), sparse(keyword match) and hybrid(combination of dense and sparse).
        Accepts either a pre-computed 'vector' OR an 'embedder' instance.
        """
        if not self.client.indices.exists(index=self.index_name):
            raise OpensearchNotReadyError("Index is empty. Ingest documents first.")

        if vector is not None:
            query_vector = vector
        elif embedder is not None:
            query_vector = embedder.embed_query(query)
        else:
            raise ValueError("Provide 'vector' or 'embedder' to perform search.")

        limit = top_k * 3
        params = {}

        if mode == "dense":
            # 1. Define the k-NN search body
            search_body = {
                "size": limit,
                "_source": ["chunk_id", "page_content", "filename", "type", "source", "language"],
                "query": {
                    "knn": {
                        "embedding": {
                            "vector": query_vector.tolist() if isinstance(query_vector, np.ndarray) else query_vector,
                            "k": limit,
                            # Efficient pre-filtering
                            "filter": {
                                "term": {"language": language}
                            } if language else {"match_all": {}}
                        }
                    }
                }
            }
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
                                        "vector": query_vector.tolist() if isinstance(query_vector, np.ndarray) else query_vector,
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

        params = {"search_pipeline": "hybrid_pipeline"}
        response = self.client.search(index=self.index_name, body=search_body, params=params)

        # Format results
        results = []
        for hit in response["hits"]["hits"]:
            metadata = hit["_source"]
            metadata["score"] = hit["_score"] # unified search score
            results.append(metadata)

        return results

    def check_db_populated(self, emb_model, emb_endpoint, max_tokens):
        if not self.client.indices.exists(index=self.index_name):
            return False
        return True

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
