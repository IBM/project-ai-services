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

def generate_chunk_id(page_content: str, filename: str = "") -> np.int64:
    """
    Generate a unique, deterministic chunk ID based on content and filename.
    
    NOTE: chunk_id is based on CONTENT + FILENAME only (not doc_id).
    This ensures that re-ingesting the same file with a new doc_id will
    UPDATE the existing chunks rather than creating duplicates.
    
    The doc_id field in the chunk is separate and can be updated.
    """
    # Use filename + content for chunk ID generation
    # This allows updating doc_id when re-ingesting the same file
    base = f"{filename}||{page_content}"
    hash_digest = hashlib.md5(base.encode("utf-8")).hexdigest()
    chunk_int = int(hash_digest[:16], 16)    # Convert first 64 bits to int
    chunk_id = chunk_int % (2**63)           # Fit into signed 64-bit range
    return np.int64(chunk_id)

class OpensearchNotReadyError(Exception):
    pass

class OpensearchVectorStore(VectorStore):
    def __init__(self):
        logger.debug("Initializing OpensearchVectorStore")

        self.host = os.getenv("OPENSEARCH_HOST")
        self.port = os.getenv("OPENSEARCH_PORT")
        self.db_prefix = os.getenv("OPENSEARCH_DB_PREFIX", "rag").lower()
        i_name = os.getenv("OPENSEARCH_INDEX_NAME", "default")
        self.index_name = self._generate_index_name(i_name.lower())

        logger.debug(f"Connecting to OpenSearch at {self.host}:{self.port}, index: {self.index_name}")

        self.client = OpenSearch(
            hosts=[{'host': self.host, 'port': self.port}],
            http_compress=True,
            use_ssl=True,
            http_auth=(os.getenv("OPENSEARCH_USERNAME"), os.getenv("OPENSEARCH_PASSWORD")),
            verify_certs=False,
            ssl_show_warn=False
        )

        logger.debug("OpenSearch client initialized successfully")
        self._create_pipeline()

    def _generate_index_name(self, name):
        hash_part = hashlib.md5(name.encode()).hexdigest()
        return f"{self.db_prefix}_{hash_part}"

    def _create_pipeline(self):
        logger.debug("Creating hybrid search pipeline")

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
            logger.debug("Hybrid search pipeline created successfully")
        except Exception as e:
            logger.error(f"Failed to create hybrid search pipeline: {e}")

    def _setup_index(self, dim):
        logger.debug(f"Setting up index {self.index_name} with dimension {dim}")

        if self.client.indices.exists(index=self.index_name):
            return

        logger.debug(f"Creating new index {self.index_name}")

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
                    "doc_id": {"type": "keyword"},
                    "type": {"type": "keyword"},
                    "source": {"type": "keyword"},
                    "language": {"type": "keyword"}
                }
            }
        }
        # Create the Index
        try:
            self.client.indices.create(index=self.index_name, body=index_body)
            logger.debug(f"Index {self.index_name} created successfully with {dim} dimensions")
        except Exception as e:
            logger.error(f"Failed to create index {self.index_name}: {e}")
            raise

    def insert_chunks(self, chunks, vectors=None, embedding=None, batch_size=10):
        """
        Supports 2 modes of insertion
        1. Pure embedding: pass 'chunks' and 'vectors'
        2. Text chunks: pass 'chunks' and 'embedding' (class instance)
        """
        logger.info("Starting insert_chunks operation")

        if not chunks:
            logger.debug("Nothing to chunk!")
            return

        logger.info(f"Inserting {len(chunks)} chunks into OpenSearch with batch_size={batch_size}")

        # Handle Pre-computed Vectors if provided
        final_embeddings = vectors
        if vectors is not None and len(vectors) > 0:
            logger.debug(f"Using pre-computed vectors, dimension: {len(vectors[0])}")
            # Initialize index using pre-computed vector dimension
            self._setup_index(len(vectors[0]))
        else:
            logger.debug("Will generate embeddings using provided embedding instance")

        # Iterate through chunks in batches and insert in bulk
        for i in tqdm(range(0, len(chunks), batch_size)):
            batch = chunks[i:i + batch_size]
            page_contents = [doc.get("page_content") for doc in batch]

            # Generate embeddings only for this specific batch
            if vectors is None and embedding is not None:
                current_batch_embeddings = embedding.embed_documents(page_contents)

                # Initialize index on the first batch if not already done
                if i == 0:
                    dim = len(current_batch_embeddings[0])
                    self._setup_index(dim)
            else:
                # Use the relevant slice from pre-computed vectors
                assert final_embeddings is not None, "final_embeddings must be set when vectors is provided"
                current_batch_embeddings = final_embeddings[i:i + batch_size]

            # 3. Transform batch to OpenSearch document format
            actions = []
            for j, (doc, emb) in enumerate(zip(batch, current_batch_embeddings)):
                fn = doc.get("filename", "")
                pc = doc.get("page_content", "")

                # Generate chunk ID based on content + filename (not doc_id)
                # This allows updating doc_id when re-ingesting the same file
                doc_id = doc.get("doc_id") or fn # Fallback to filename if UUID missing
                logger.debug(f"Inserting chunk {j+1}: doc_id={doc_id}, filename={fn}")
                cid = generate_chunk_id(pc, fn)

                actions.append({
                    "_index": self.index_name,
                    "_id": str(cid),
                    "_source": {
                        "chunk_id": cid,
                        "embedding": emb.tolist() if isinstance(emb, np.ndarray) else emb,
                        "page_content": pc,
                        "filename": fn,
                        "doc_id": doc_id,
                        "type": doc.get("type", ""),
                        "source": doc.get("source", ""),
                        "language": doc.get("language", "")
                    }
                })

            # Bulk insert the current batch
            batch_num = i // batch_size + 1

            try:
                success, failed = helpers.bulk(self.client, actions, stats_only=True)
                if failed:
                    logger.error(f"Failed to insert {failed} chunks in batch {batch_num} starting at index {i}")
                    return
                logger.debug(f"Batch {batch_num}: Successfully inserted {success} chunks, failed {failed}")
                
                # Log the doc_ids that were inserted in this batch for verification
                inserted_doc_ids = list(set([action["_source"]["doc_id"] for action in actions]))
                logger.info(f"Batch {batch_num}: Inserted chunks for doc_ids: {inserted_doc_ids}")
            except Exception as e:
                logger.error(f"Exception during bulk insert for batch {batch_num}: {e}")
                raise

        logger.info(f"Insert operation completed: {len(chunks)} chunks inserted into index {self.index_name}")


    def search(self, query_text, vector=None, embedding=None, top_k=5, mode=None, doc_id=None, language='en'):
        """
        Supported search modes: dense(semantic search), sparse(keyword match) and hybrid(combination of dense and sparse).
        Accepts either a pre-computed 'vector' OR an 'embedding' instance.
        """
        logger.debug(f"Starting search operation: query='{query_text[:50]}...', top_k={top_k}, mode={mode}, language={language}")

        query = query_text
        if not self.client.indices.exists(index=self.index_name):
            logger.error(f"Index {self.index_name} does not exist")
            raise OpensearchNotReadyError("Index is empty. Ingest documents first.")

        if vector is not None:
            logger.debug("Using pre-computed query vector")
            query_vector = vector
        elif embedding is not None:
            logger.debug("Generating query embedding")
            query_vector = embedding.embed_query(query)
        else:
            logger.error("No vector or embedding provided for search")
            raise ValueError("Provide 'vector' or 'embedding' to perform search.")

        # Default to hybrid mode if not specified
        if mode is None:
            mode = "hybrid"
            logger.debug("Mode not specified, defaulting to 'hybrid'")

        limit = top_k * 3
        logger.debug(f"Search mode: {mode}, limit: {limit}")
        params = {}

        if mode == "dense":
            # 1. Define the k-NN search body
            search_body = {
                "size": limit,
                "_source": ["chunk_id", "page_content", "filename", "doc_id", "type", "source", "language"],
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
                "_source": ["chunk_id", "page_content", "filename", "doc_id", "type", "source", "language"],
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
                "_source": ["chunk_id", "page_content", "filename", "doc_id", "type", "source", "language"],
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
        else:
            logger.error(f"Invalid search mode: {mode}")
            raise ValueError(f"Invalid search mode: {mode}. Must be 'dense', 'sparse', or 'hybrid'.")

        params = {"search_pipeline": "hybrid_pipeline"}

        try:
            logger.debug(f"Executing search query on index {self.index_name}")
            response = self.client.search(index=self.index_name, body=search_body, params=params)

            total_hits = response["hits"]["total"]["value"] if isinstance(response["hits"]["total"], dict) else response["hits"]["total"]
            logger.info(f"Search completed: found {total_hits} total hits, returning top {len(response['hits']['hits'])} results")
        except Exception as e:
            logger.error(f"Search query failed: {e}")
            raise

        # Format results
        results = []
        for idx, hit in enumerate(response["hits"]["hits"]):
            metadata = hit["_source"]
            metadata["score"] = hit["_score"] # unified search score
            results.append(metadata)
            logger.debug(f"Result {idx+1}: doc_id={metadata.get('doc_id', 'N/A')}, score={hit['_score']:.4f}")

        logger.debug(f"Search operation completed successfully with {len(results)} results")
        return results

    def check_db_populated(self):
        logger.debug(f"Checking if database is populated for index {self.index_name}")

        exists = self.client.indices.exists(index=self.index_name)
        logger.info(f"Database populated check: {exists}")
        return exists

    def reset_index(self):
        logger.debug(f"Starting reset_index operation for {self.index_name}")

        if self.client.indices.exists(index=self.index_name):
            try:
                self.client.indices.delete(index=self.index_name)
                logger.info(f"Index {self.index_name} deleted successfully")
            except Exception as e:
                logger.error(f"Failed to delete index {self.index_name}: {e}")
                raise
        else:
            logger.info(f"Index {self.index_name} does not exist, nothing to delete")

        # Clear local cache
        cache_pattern = os.path.join(LOCAL_CACHE_DIR, self.index_name+"*")
        logger.debug(f"Searching for cache files matching pattern: {cache_pattern}")

        files_to_remove = glob(cache_pattern)
        if files_to_remove:
            logger.debug(f"Found {len(files_to_remove)} cache files/directories to remove")
            for file_path in files_to_remove:
                try:
                    if os.path.isdir(file_path):
                        shutil.rmtree(file_path)
                        logger.debug(f"Removed directory: {file_path}")
                        continue
                    os.remove(file_path)
                    logger.debug(f"Removed file: {file_path}")
                except OSError as e:
                    logger.error(f"Error removing {file_path}: {e}")
            logger.info("Local cache cleaned up successfully")
        else:
            logger.info("No cache files found, cache already clean")

        logger.info("Reset index operation completed")

    def delete_document_by_id(self, doc_id: str):
        """
        Delete all chunks associated with a specific document from the index.
        
        Args:
            doc_id: The unique identifier of the document to delete
            
        Returns:
            Number of chunks deleted
        """
        logger.debug(f"Starting delete operation for document {doc_id}")
        
        if not self.client.indices.exists(index=self.index_name):
            logger.warning(f"Index {self.index_name} does not exist, nothing to delete")
            return 0
        
        try:
            # First, check if any chunks exist for this document (for debugging)
            count_query = {
                "query": {
                    "term": {
                        "doc_id": doc_id
                    }
                }
            }
            
            try:
                logger.debug(f"Count query: {count_query}")
                count_response = self.client.count(index=self.index_name, body=count_query)
                chunk_count = count_response.get("count", 0)
                logger.debug(f"Count response: {count_response}")
                logger.debug(f"Found {chunk_count} chunks for document {doc_id} before deletion")
                
                if chunk_count == 0:
                    # Try to find if chunks exist with a different doc_id (e.g., filename instead of UUID)
                    # Search for any chunks to see what doc_ids are actually stored
                    sample_query = {
                        "size": 5,
                        "_source": ["doc_id", "filename"],
                        "query": {"match_all": {}}
                    }
                    try:
                        sample_response = self.client.search(index=self.index_name, body=sample_query)
                        sample_docs = sample_response.get("hits", {}).get("hits", [])
                        if sample_docs:
                            sample_doc_ids = [hit['_source'].get('doc_id', 'N/A') for hit in sample_docs[:3]]
                            sample_filenames = [hit['_source'].get('filename', 'N/A') for hit in sample_docs[:3]]
                            logger.warning(f"Sample doc_ids in index: {sample_doc_ids}")
                            logger.warning(f"Sample filenames in index: {sample_filenames}")
                            logger.warning(f"Looking for doc_id: {doc_id}")
                        else:
                            logger.warning(f"Index {self.index_name} appears to be empty (no documents found)")
                    except Exception as e:
                        logger.warning(f"Could not retrieve sample documents: {e}")
                    
                    logger.warning(
                        f"No chunks found for document {doc_id} in index {self.index_name}. "
                        f"Possible causes: (1) doc_id mismatch - chunks may be indexed with filename instead of UUID, "
                        f"(2) document not ingested, (3) already deleted"
                    )
                    return 0
            except Exception as count_error:
                logger.warning(f"Could not count chunks for document {doc_id}: {count_error}")
                # Continue with deletion attempt anyway
            
            # Use delete_by_query to remove all chunks with matching doc_id
            delete_query = {
                "query": {
                    "term": {
                        "doc_id": doc_id
                    }
                }
            }
            
            response = self.client.delete_by_query(
                index=self.index_name,
                body=delete_query,
                params={"refresh": "true"}  # Ensure changes are immediately visible
            )
            
            # delete_by_query returns a response with structure:
            # {
            #   "took": <time_in_ms>,
            #   "timed_out": false,
            #   "total": <total_docs_matched>,
            #   "deleted": <docs_deleted>,
            #   "batches": <number_of_batches>,
            #   "version_conflicts": 0,
            #   "noops": 0,
            #   "retries": {...},
            #   "throttled_millis": 0,
            #   "failures": []
            # }
            
            deleted_count = response.get("deleted", 0)
            total_matched = response.get("total", 0)
            failures = response.get("failures", [])
            
            # Log detailed response for debugging
            logger.debug(f"delete_by_query response: took={response.get('took')}ms, total={total_matched}, deleted={deleted_count}, failures={len(failures)}")
            
            if failures:
                logger.error(f"Deletion failures for document {doc_id}: {failures}")
            
            if deleted_count > 0:
                logger.info(f"✓ Deleted {deleted_count} chunks for document {doc_id} from index {self.index_name}")
            else:
                if total_matched == 0:
                    logger.info(f"Deleted {deleted_count} chunks for document {doc_id} from index {self.index_name} (no matching documents found)")
                else:
                    logger.warning(f"Matched {total_matched} documents but deleted {deleted_count} for document {doc_id} (possible version conflicts or failures)")
            
            return deleted_count
            
        except Exception as e:
            logger.error(f"Failed to delete document {doc_id} from index: {e}")
            raise
