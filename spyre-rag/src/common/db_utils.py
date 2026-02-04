from glob import glob
import os
import shutil
import numpy as np
import hashlib
from tqdm import tqdm
from opensearchpy import OpenSearch, helpers

from common.emb_utils import Embedding
from common.misc_utils import LOCAL_CACHE_DIR, get_logger

logger = get_logger("Opensearch")

def generate_chunk_id(filename: str, page_content: str, index: int) -> int:
    """
    Generate a unique, deterministic chunk ID based on filename, content, and index.
    """
    base = f"{filename}-{index}-{page_content}"
    hash_digest = hashlib.md5(base.encode("utf-8")).hexdigest()
    chunk_int = int(hash_digest[:16], 16)  # Convert first 64 bits to int
    chunk_id = chunk_int % (2**63)  # Fit into signed 64-bit range
    return np.int64(chunk_id)

class OpensearchNotReadyError(Exception):
    pass

class OpensearchVectorStore:
    def __init__(
        self,
        host=os.getenv("OPENSEARCH_HOST"),
        port=os.getenv("OPENSEARCH_PORT"),
        db_prefix=os.getenv("OPENSEARCH_DB_PREFIX"),
        c_name=os.getenv("OPENSEARCH_COLLECTION_NAME"),
        uname=os.getenv("OPENSEARCH_USERNAME"),
        password=os.getenv("OPENSEARCH_PASSWORD")
    ):
        self.host = host
        self.port = port
        self.db_prefix = db_prefix
        self.c_name = c_name
        self.uname = uname
        self.password = password
        self.collection = None
        self.collection_name = None
        self._embedder = None
        self._embedder_config = {}
        self.page_content_corpus = []
        self.metadata_map = []
        self.index_name = None

        auth = (self.uname, self.password)
        self.client = OpenSearch(
            hosts=[{'host': self.host, 'port': self.port}],
            http_compress=True, # Recommended to reduce latency for large vector payloads
            use_ssl=True,
            http_auth=auth,
            verify_certs=False, # Set to True in production with valid certificates
            ssl_assert_hostname=False,
            ssl_show_warn=False
        )
        # Test connection
        if self.client.ping():
            print("Successfully connected to OpenSearch!")
        self.create_pipeline()

    def create_pipeline(self):
        pipeline_body = {
            "description": "Post-processor for hybrid search using RRF",
            "phase_results_processors": [
                {
                    "normalization-processor": {
                        "normalization": {"technique": "min_max"},
                        "combination": {
                            "technique": "arithmetic_mean",
                            "parameters": {
                                # Removed "rank_constant" as it caused the 400 error
                                # Optional: "weights": [0.5, 0.5] 
                            }
                        }
                    }
                }
            ]
        }
        # Delete the old failed attempt first to be safe
        try:
            self.client.search_pipeline.delete(id="hybrid_rrf_pipeline")
        except:
            pass

        # Create the corrected pipeline
        self.client.search_pipeline.put(id="hybrid_rrf_pipeline", body=pipeline_body)

    def _generate_collection_name(self):
        hash_part = hashlib.md5(self.c_name.encode()).hexdigest()
        return f"{self.db_prefix}_{hash_part}"

    def _setup_index(self, name, dim):
        if self.client.indices.exists(index=name):
            logger.info(f"Index {name} already present in vectorstore")
            self.index_name = name
            return name

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
                            "name": "hnsw",       # HNSW is standard for high performance
                            "space_type": "l2",
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

        # Create the Index (or collection)
        self.client.indices.create(index=name, body=index_body)
        self.index_name = name
        return self.index_name


    def _ensure_embedder(self, emb_model, emb_endpoint, max_tokens):
        config = {"model": emb_model, "endpoint": emb_endpoint, "max_tokens": max_tokens}
        if self._embedder is None or self._embedder_config != config:
            logger.debug(f"⚙️ Initializing embedder: {emb_model}")
            self._embedder = Embedding(emb_model, emb_endpoint, max_tokens)
            self._embedder_config = config

    def reset_collection(self):
        name = self._generate_collection_name()
        if self.client.indices.exists(index=name):
            self.client.indices.delete(index=name)
            logger.info(f"Collection {name} deleted.")
        else:
            logger.info(f"Collection {name} does not exist!")

        files_to_remove = glob(os.path.join(LOCAL_CACHE_DIR, name+"*"))
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

        self.page_content_corpus = []
        self.metadata_map = []


    def insert_chunks(self, emb_model, emb_endpoint, max_tokens, chunks, batch_size=10):
        if not chunks:
            logger.debug("Nothing to chunk!")
            return

        self._ensure_embedder(emb_model, emb_endpoint, max_tokens)
        self.collection_name = self._generate_collection_name()

        sample_embedding = self._embedder.embed_documents([chunks[0]["page_content"]])[0]
        dim = len(sample_embedding)

        self.collection = self._setup_index(self.collection_name.lower(), dim)

        logger.debug(f"Inserting {len(chunks)} chunks into Opensearch...")

        for i in tqdm(range(0, len(chunks), batch_size)):
            batch = chunks[i:i + batch_size]
            page_contents = [doc.get("page_content") for doc in batch]
            embeddings = self._embedder.embed_documents(page_contents)

            filenames = [doc.get("filename", "") for doc in batch]
            types = [doc.get("type", "") for doc in batch]
            sources = [doc.get("source", "") for doc in batch]
            languages = [doc.get("language", "") for doc in batch]

            chunk_ids = [generate_chunk_id(fn, pc, i+j) for j, (fn, pc) in enumerate(zip(filenames, page_contents))]

            # 1. Transform columnar format to OpenSearch document format
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

            # 2. Use the Bulk helper to insert/upsert all documents
            # 'stats_only=True' returns a simple count of success/failure
            success, failed = helpers.bulk(self.client, actions, stats_only=True)
            if failed:
                logger.error("failed to insert chunks to vectorstore")

            print(f"Successfully indexed {success} chunks. Failed: {failed}")

            self.page_content_corpus.extend(page_contents)
            self.metadata_map.extend([
                {"chunk_id": cid, "filename": fn, "type": t, "source": s, "page_content": pc, "language": l}
                for cid, fn, t, s, pc, l in zip(chunk_ids, filenames, types, sources, page_contents, languages)
            ])

        logger.debug(f"Inserted the chunks into collection.")
    
    def check_db_populated(self, emb_model, emb_endpoint, max_tokens):
        self._ensure_embedder(emb_model, emb_endpoint, max_tokens)
        self.collection_name = self._generate_collection_name()

        if not self.client.indices.exists(index=self.index_name):
            return False
        return True

    def search(self, query, emb_model, emb_endpoint, max_tokens, top_k=5, deployment_type='cpu', mode="hybrid", language='en'):
        self._ensure_embedder(emb_model, emb_endpoint, max_tokens)
        self.collection_name = self._generate_collection_name()
        index_name = self.collection_name.lower()
        if not self.client.indices.exists(index=index_name):
            raise OpensearchNotReadyError(
                    f"Opensearch database is empty. Ingest documents first."
                )

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
                            # Efficient pre-filtering (Equivalent to Milvus expr)
                            "filter": {
                                "term": {"language": language}
                            } if language else {"match_all": {}}
                        }
                    }
                }
            }
            
            response = self.client.search(index=index_name, body=search_body)
            
            # Format results to match Milvus entity output
            dense_results = [hit["_source"] for hit in response["hits"]["hits"]]
            return dense_results[:top_k]

        elif mode == "sparse":
            # 2. OpenSearch native Sparse Search (BM25 or Neural Sparse)
            # In 2026, we use a standard full-text match for sparse/keyword logic
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

            response = self.client.search(index=index_name, body=search_body)
            
            # Format results
            sparse_results = []
            for hit in response["hits"]["hits"]:
                metadata = hit["_source"]
                metadata["score"] = hit["_score"]
                sparse_results.append(metadata)
                
            return sparse_results[:top_k]

        elif mode == "hybrid":
            logger.info(f"Hybrid search => value of k: {limit}")
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
                index=index_name,
                body=search_body,
                search_pipeline="hybrid_rrf_pipeline" # This triggers the server-side RRF
            )

            # Format results to match your existing metadata structure
            hybrid_results = []
            for hit in response["hits"]["hits"]:
                metadata = hit["_source"]
                metadata["score"] = hit["_score"] # This is the unified RRF score
                hybrid_results.append(metadata)

            return hybrid_results
