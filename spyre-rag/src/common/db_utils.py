from glob import glob
import os
import shutil
import numpy as np
import hashlib
import joblib
from tqdm import tqdm
from scipy import sparse
from collections import defaultdict
# from pymilvus import (
#     connections, utility, Collection, CollectionSchema,
#     FieldSchema, DataType
# )
from opensearchpy import OpenSearch, helpers

from sklearn.feature_extraction.text import TfidfVectorizer
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
        self.vectorizer = None
        self.sparse_matrix = None

        auth = (self.uname, self.password)
        self.client = OpenSearch(
            hosts=[{'host': self.host, 'port': self.port}],
            http_compress=True, # Recommended to reduce latency for large vector payloads
            http_auth=auth,
            verify_certs=False, # Set to True in production with valid certificates
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
                            "technique": "rrf",
                            "parameters": {"rank_constant": 60}
                        }
                    }
                }
            ]
        }
        self.client.search_pipeline.put(id="hybrid_rrf_pipeline", body=pipeline_body)

    def _generate_collection_name(self):
        hash_part = hashlib.md5(self.c_name.encode()).hexdigest()
        return f"{self.db_prefix}_{hash_part}"

    def _get_index_paths(self):
        base_path = os.path.join(LOCAL_CACHE_DIR, f"{self.collection_name}_sparse_index")
        return f"{base_path}_vectorizer.joblib", f"{base_path}_matrix.npz", f"{base_path}_metadata.joblib"

    def _save_sparse_index(self):
        vectorizer_path, matrix_path, metadata_path = self._get_index_paths()
        joblib.dump(self.vectorizer, vectorizer_path)
        sparse.save_npz(matrix_path, self.sparse_matrix)
        joblib.dump(self.metadata_map, metadata_path)

    def _load_sparse_index(self):
        vectorizer_path, matrix_path, metadata_path = self._get_index_paths()
        if os.path.exists(vectorizer_path) and os.path.exists(matrix_path) and os.path.exists(metadata_path):
            self.vectorizer = joblib.load(vectorizer_path)
            self.sparse_matrix = sparse.load_npz(matrix_path)
            self.metadata_map = joblib.load(metadata_path)
            logger.info(f"✅ Loaded sparse index for collection '{self.collection_name}'.")
            return True
        return False

    # def _setup_collection(self, name, dim):
    #     if utility.has_collection(name):
    #         return Collection(name=name)

    #     fields = [
    #         FieldSchema(name="chunk_id", dtype=DataType.INT64, is_primary=True, auto_id=False),
    #         FieldSchema(name="embedding", dtype=DataType.FLOAT_VECTOR, dim=dim),
    #         FieldSchema(name="page_content", dtype=DataType.VARCHAR, max_length=32768, enable_analyzer=True),
    #         FieldSchema(name="filename", dtype=DataType.VARCHAR, max_length=512),
    #         FieldSchema(name="type", dtype=DataType.VARCHAR, max_length=32),
    #         FieldSchema(name="source", dtype=DataType.VARCHAR, max_length=32768),
    #         FieldSchema(name="language", dtype=DataType.VARCHAR, max_length=8),
    #     ]

    #     schema = CollectionSchema(fields=fields, description="RAG chunk storage (dense only)")
    #     collection = Collection(name=name, schema=schema)

    #     collection.create_index(
    #         field_name="embedding",
    #         index_params={"metric_type": "L2", "index_type": "IVF_FLAT", "params": {"nlist": 128}}
    #     )

    #     return collection
    
    def _setup_index(self, name, dim):
        if self.client.indices.exists(index=name):
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
        return name


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
        self.vectorizer = None
        self.sparse_matrix = None

    def insert_chunks(self, emb_model, emb_endpoint, max_tokens, chunks, batch_size=10):
        if not chunks:
            logger.debug("Nothing to chunk!")
            return

        self._ensure_embedder(emb_model, emb_endpoint, max_tokens)
        self.collection_name = self._generate_collection_name()

        sample_embedding = self._embedder.embed_documents([chunks[0]["page_content"]])[0]
        dim = len(sample_embedding)

        self.collection = self._setup_index(self.collection_name, dim)
        self.collection.load()

        logger.debug(f"Inserting {len(chunks)} chunks into Milvus...")

        for i in tqdm(range(0, len(chunks), batch_size)):
            batch = chunks[i:i + batch_size]
            page_contents = [doc.get("page_content") for doc in batch]
            embeddings = self._embedder.embed_documents(page_contents)

            filenames = [doc.get("filename", "") for doc in batch]
            types = [doc.get("type", "") for doc in batch]
            sources = [doc.get("source", "") for doc in batch]
            languages = [doc.get("language", "") for doc in batch]

            chunk_ids = [generate_chunk_id(fn, pc, i+j) for j, (fn, pc) in enumerate(zip(filenames, page_contents))]

            # self.collection.upsert([
            #     chunk_ids,
            #     embeddings,
            #     page_contents,
            #     filenames,
            #     types,
            #     sources,
            #     languages
            # ])

            # 1. Transform Milvus columnar format to OpenSearch document format
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

        logger.debug("Fitting external TF-IDF vectorizer")
        self.vectorizer = TfidfVectorizer()
        self.sparse_matrix = self.vectorizer.fit_transform(self.page_content_corpus)

        self._save_sparse_index()
        logger.debug(f"Inserted the chunks into collection.")

    def _rrf_fusion(self, dense_results, sparse_results, top_k):
        """
        Perform Reciprocal Rank Fusion (RRF) on dense and sparse results.
        Each result should be a list of dicts with at least 'chunk_id' field.
        """
        rrf_k = 60  # RRF constant to dampen higher ranks
        score_map = defaultdict(float)
        doc_map = {}

        # Process dense results
        for rank, doc in enumerate(dense_results):
            cid = doc["chunk_id"]
            score_map[cid] += 1 / (rank + 1 + rrf_k)
            doc_map[cid] = doc  # Store full metadata

        # Process sparse results
        for rank, doc in enumerate(sparse_results):
            cid = doc["chunk_id"]
            score_map[cid] += 1 / (rank + 1 + rrf_k)
            doc_map[cid] = doc  # Will overwrite if duplicate, but that's fine

        # Sort by combined RRF score
        sorted_items = sorted(score_map.items(), key=lambda x: x[1], reverse=True)[:top_k]

        # Assemble final results
        final_results = []
        for cid, score in sorted_items:
            result = doc_map[cid].copy()
            result["rrf_score"] = score
            final_results.append(result)

        return final_results
    
    def check_db_populated(self, emb_model, emb_endpoint, max_tokens):
        self._ensure_embedder(emb_model, emb_endpoint, max_tokens)
        self.collection_name = self._generate_collection_name()

        if not self.client.indices.exists(index=self.index_name):
            return False
        return True

    def search(self, query, emb_model, emb_endpoint, max_tokens, top_k=5, deployment_type='cpu', mode="hybrid", language='en'):
        self._ensure_embedder(emb_model, emb_endpoint, max_tokens)
        self.collection_name = self._generate_collection_name()

        if not self.client.indices.exists(index=self.index_name):
            raise OpensearchNotReadyError(
                    f"Opensearch database is empty. Ingest documents first."
                )

        query_vector = self._embedder.embed_query(query)
        self.collection.load()

        # if mode == "dense":
        #     results = self.collection.search(
        #         data=[query_vector],
        #         anns_field="embedding",
        #         param={"metric_type": "L2", "params": {"nprobe": 10}},
        #         limit=top_k * 3,  # retrieve more for filtering
        #         output_fields=["chunk_id", "page_content", "filename", "type", "source", "language"],
        #         expr=f"language == \"{language}\"" if language else None
        #     )
        #     dense_results = [hit.get('entity') for hit in results[0]]
        #     dense_results = dense_results[:top_k]
            
        #     return dense_results

        # elif mode == "sparse":
        #     if self.vectorizer is None or self.sparse_matrix is None:
        #         loaded = self._load_sparse_index()
        #         if not loaded:
        #             raise RuntimeError("Sparse search index not initialized.")

        #     query_vec = self.vectorizer.transform([query])
        #     scores = (self.sparse_matrix @ query_vec.T).toarray().ravel()
        #     ranked = sorted(enumerate(scores), key=lambda x: x[1], reverse=True)[:3*top_k] # retrieve more for filtering
        #     sparse_results = []
        #     for idx, score in ranked:
        #         metadata = self.metadata_map[idx]
        #         if language is None or metadata.get("language") == language:
        #             sparse_results.append({**metadata, "score": score})
        #         if len(sparse_results) >= top_k:
        #             break
            
        #     return sparse_results



        limit=top_k * 3,  # retrieve more for filtering

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
            
            response = self.client.search(index=self.index_name, body=search_body)
            
            # Format results to match Milvus entity output
            dense_results = [hit["_source"] for hit in response["hits"]["hits"]]
            return dense_results[:top_k]

        elif mode == "sparse":
            if self.vectorizer is None or self.sparse_matrix is None:
                loaded = self._load_sparse_index()
                if not loaded:
                    raise RuntimeError("Sparse search index not initialized.")

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

            response = self.client.search(index=self.index_name, body=search_body)
            
            # Format results
            sparse_results = []
            for hit in response["hits"]["hits"]:
                metadata = hit["_source"]
                metadata["score"] = hit["_score"]
                sparse_results.append(metadata)
                
            return sparse_results[:top_k]


        # elif mode == "hybrid":
        #     if self.vectorizer is None or self.sparse_matrix is None:
        #         loaded = self._load_sparse_index()
        #         if not loaded:
        #             raise RuntimeError("Sparse index missing for hybrid search.")

        #     dense_results = self.collection.search(
        #         data=[query_vector],
        #         anns_field="embedding",
        #         param={"metric_type": "L2", "params": {"nprobe": 10}},
        #         limit=top_k * 3,  # retrieve more for filtering
        #         output_fields=["chunk_id", "page_content", "filename", "type", "source", "language"],
        #         expr=f"language == \"{language}\"" if language else None
        #     )
        #     dense_results = [hit.get('entity') for hit in dense_results[0]]
        #     dense_results = dense_results[:top_k]

        #     query_vec = self.vectorizer.transform([query])
        #     scores = (self.sparse_matrix @ query_vec.T).toarray().ravel()
        #     sparse_ranked = sorted(enumerate(scores), key=lambda x: x[1], reverse=True)[:3*top_k] # retrieve more for filtering

        #     sparse_results = []
        #     for idx, score in sparse_ranked:
        #         metadata = self.metadata_map[idx]
        #         if language is None or metadata.get("language") == language:
        #             sparse_results.append({**metadata, "score": score})
        #         if len(sparse_results) >= top_k:
        #             break

        #     return self._rrf_fusion(dense_results, sparse_results, top_k)

        # else:
        #     raise ValueError("Invalid search mode. Choose from ['dense', 'sparse', 'hybrid'].")


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

            # Format results to match your existing metadata structure
            hybrid_results = []
            for hit in response["hits"]["hits"]:
                metadata = hit["_source"]
                metadata["score"] = hit["_score"] # This is the unified RRF score
                hybrid_results.append(metadata)

            return hybrid_results
