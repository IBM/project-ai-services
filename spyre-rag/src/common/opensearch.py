import numpy as np
import hashlib
from tqdm import tqdm
from opensearchpy import OpenSearch, helpers

from common.misc_utils import get_logger
from common.vector_db import VectorStore, VectorStoreNotReadyError
from common.retry_utils import retry_on_transient_error
from common.settings import settings

logger = get_logger("OpenSearch")


def generate_chunk_id(doc_id: str, page_content: str) -> np.int64:
    """
    Generate a unique, deterministic chunk ID based on filename, content, and index.
    """
    # Using doc_id (UUID) is safer than filename to prevent collisions
    # between different users uploading 'document.pdf'
    base = f"{doc_id}||{page_content}"
    hash_digest = hashlib.md5(base.encode("utf-8")).hexdigest()
    chunk_int = int(hash_digest[:16], 16)    # Convert first 64 bits to int
    chunk_id = chunk_int % (2**63)           # Fit into signed 64-bit range
    return np.int64(chunk_id)

class OpensearchNotReadyError(VectorStoreNotReadyError):
    """Raised when OpenSearch is unreachable or initializing."""
    pass

class OpensearchVectorStore(VectorStore):
    def __init__(self):
        logger.debug("Initializing OpensearchVectorStore")

        self.host = settings.vector_store.opensearch_host
        self.port = settings.vector_store.opensearch_port
        self.db_prefix = settings.vector_store.opensearch_db_prefix.lower()
        i_name = settings.vector_store.opensearch_index_name
        self.index_name = self._generate_index_name(i_name.lower())
        
        # Replication settings for multi-node clusters
        self.num_shards = settings.vector_store.opensearch_num_shards
        
        logger.debug(f"Connecting to OpenSearch at {self.host}:{self.port}, index: {self.index_name}")
        logger.debug(f"Index configuration: shards={self.num_shards}")

        self.client = OpenSearch(
            hosts=[{'host': self.host, 'port': self.port}],
            http_compress=True,
            use_ssl=True,
            http_auth=(
                settings.vector_store.opensearch_username,
                settings.vector_store.opensearch_password,
            ),
            verify_certs=False,
            ssl_show_warn=False
        )

        logger.debug("OpenSearch client initialized successfully")
        self._create_pipeline()

    def _generate_index_name(self, name):
        hash_part = hashlib.md5(name.encode()).hexdigest()
        return f"{self.db_prefix}_{hash_part}"

    @retry_on_transient_error(max_retries=3, initial_delay=5.0, backoff_multiplier=2.0)
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
            raise

    @retry_on_transient_error(max_retries=3, initial_delay=5.0, backoff_multiplier=2.0)
    def _setup_index(self, dim):
        logger.debug(f"Setting up index {self.index_name} with dimension {dim}")

        if self.client.indices.exists(index=self.index_name):
            return

        logger.debug(f"Creating new index {self.index_name}")

        # index body: setting and mappings
        index_body = {
            "settings": {
                "index": {
                    "knn": True,  # Enable k-NN search functionality
                    "knn.algo_param.ef_search": 100,  # Number of candidates to consider during search (higher = more accurate but slower)
                    "number_of_shards": self.num_shards,  # Number of primary shards for data distribution
                    'auto_expand_replicas': '0-all' # dynamically set the replicas based on nodes
                }
            },
            "mappings": {
                "properties": {
                    # Unique identifier for each chunk, generated from doc_id and content hash
                    "chunk_id": {"type": "long"},
                    
                    # Vector embedding field for semantic search
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
                    
                    # The actual text content of the chunk for keyword search and retrieval
                    "text": {
                        "type": "text",
                        "analyzer": "standard"
                    },
                    
                    # Metadata container for document attributes
                    "metadata": {
                        "dynamic": "true",  # Allow additional metadata fields to be added dynamically
                        "properties": {
                            "filename": {"type": "keyword"},  # Original filename
                            "doc_id": {"type": "keyword"},  # Unique document identifier (UUID)
                            "type": {"type": "keyword"},  # Document type (e.g., text, table)
                            "source": {"type": "keyword"},  # Source of the text e.g: Chapter -> Section etc..
                            "language": {"type": "keyword"},  # Language code (e.g., 'en', 'de') for language-specific filtering
                            "page_number": {"type": "integer"},  # Page number where this chunk originated
                            "chunk_index": {"type": "integer"},  # Sequential index of this chunk within the document
                            "total_chunks": {"type": "integer"},  # Total number of chunks in the parent document
                            "created_at": {"type": "date"}  # Timestamp when the chunk was indexed
                        }
                    }
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

    @retry_on_transient_error(max_retries=3, initial_delay=5.0, backoff_multiplier=2.0)
    def insert_chunks(self, chunks, vectors=None, embedding=None, batch_size=10):
        """
        Supports 2 modes of insertion with retry logic for transient failures.
        1. Pure embedding: pass 'chunks' and 'vectors'
        2. Text chunks: pass 'chunks' and 'embedding' (class instance)

        Args:
            chunks: List of document chunks to insert (for a single document)
            vectors: Pre-computed embeddings (optional)
            embedding: Embedding instance to generate embeddings (optional)
            batch_size: Number of chunks to insert per batch

        Returns:
            bool: True if indexing succeeded, False if it failed
        """
        logger.debug("Starting insert_chunks operation")

        if not chunks:
            logger.debug("Nothing to chunk!")
            return True

        logger.debug(f"Inserting {len(chunks)} chunks into OpenSearch with batch_size={batch_size}")

        # Handle Pre-computed Vectors if provided
        final_embeddings = vectors
        if vectors is not None and len(vectors) > 0:
            logger.debug(f"Using pre-computed vectors, dimension: {len(vectors[0])}")
            # Initialize index using pre-computed vector dimension (only once)
            self._setup_index(len(vectors[0]))
        elif embedding is not None:
            logger.debug("Will generate embeddings using provided embedding instance")
            # Generate first batch to get dimension and setup index once
            first_batch = chunks[:min(batch_size, len(chunks))]
            first_page_contents = [doc.get("page_content") for doc in first_batch]
            first_embeddings = embedding.embed_documents(first_page_contents)
            dim = len(first_embeddings[0])
            self._setup_index(dim)
            logger.debug(f"Index setup completed with dimension: {dim}")

        # Iterate through chunks in batches and insert in bulk
        for i in tqdm(range(0, len(chunks), batch_size)):
            batch = chunks[i:i + batch_size]
            page_contents = [doc.get("page_content") for doc in batch]

            # Generate embeddings for this batch
            if vectors is None and embedding is not None:
                current_batch_embeddings = embedding.embed_documents(page_contents)
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
                cid = generate_chunk_id(doc_id, pc)

                # Build metadata object with all fields
                metadata = {
                    "filename": fn,
                    "doc_id": doc_id,
                    "type": doc.get("type", ""),
                    "source": doc.get("source", ""),
                    "language": doc.get("language", "")
                }
                
                # Add optional fields if they exist
                if doc.get("page_number") is not None:
                    metadata["page_number"] = doc.get("page_number")
                if doc.get("chunk_index") is not None:
                    metadata["chunk_index"] = doc.get("chunk_index")
                if doc.get("total_chunks") is not None:
                    metadata["total_chunks"] = doc.get("total_chunks")
                if doc.get("created_at") is not None:
                    metadata["created_at"] = doc.get("created_at")

                actions.append({
                    "_index": self.index_name,
                    "_id": str(cid),
                    "_source": {
                        "chunk_id": cid,
                        "embedding": emb.tolist() if isinstance(emb, np.ndarray) else emb,
                        "text": pc,
                        "metadata": metadata
                    }
                })

            # Bulk insert the current batch
            batch_num = i // batch_size + 1

            try:
                # Use bulk insert with error handling
                success_count, errors = helpers.bulk(
                    self.client,
                    actions,
                    stats_only=False,               # Get detailed error information for failed chunks
                    raise_on_error=False,           # Continue indexing other chunks even if some fail
                    refresh=True
                )

                # If any errors occurred in this batch, the document failed
                if errors:
                    logger.debug(f"Batch {batch_num}: {len(errors)} chunks failed to insert")
                    for error_item in errors:
                        # OpenSearch returns errors in a dict format: {'index': {'_id': '...', 'error': ...}}
                        action_type = list(error_item.keys())[0]
                        error_detail = error_item[action_type]
                        logger.error(f"Chunk insertion error: {error_detail.get('error', 'Unknown error')}")
                    return False

                logger.debug(f"Batch {batch_num}: {success_count} chunks inserted successfully")

            except Exception as e:
                logger.error(f"Exception during bulk insert for batch {batch_num}: {e}")
                raise

        logger.info(f"Insert operation completed successfully: {len(chunks)} chunks inserted into index {self.index_name}")
        return True


    @retry_on_transient_error(max_retries=3, initial_delay=5.0, backoff_multiplier=2.0)
    def search(self, query_text, vector=None, embedding=None, top_k=5, mode=None, doc_id=None, language='en'):
        """
        Supported search modes: dense(semantic search), sparse(keyword match) and hybrid(combination of dense and sparse).
        Accepts either a pre-computed 'vector' OR an 'embedding' instance.
        Includes retry logic for transient failures.
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
                "size": top_k,
                "_source": ["chunk_id", "text", "metadata"],
                "query": {
                    "knn": {
                        "embedding": {
                            "vector": query_vector.tolist() if isinstance(query_vector, np.ndarray) else query_vector,
                            "k": limit,
                            # Efficient pre-filtering
                            "filter": {
                                "term": {"metadata.language": language}
                            } if language else {"match_all": {}}
                        }
                    }
                }
            }
        elif mode == "sparse":
            # OpenSearch native Sparse Search (BM25 or Neural Sparse)
            # Standard full-text match for sparse/keyword logic
            search_body = {
                "size": top_k,
                "_source": ["chunk_id", "text", "metadata"],
                "query": {
                    "bool": {
                        "must": [
                            {"match": {"text": query}}
                        ],
                        "filter": [
                            {"term": {"metadata.language": language}}
                        ] if language else []
                    }
                }
            }
        elif mode == "hybrid":
            # OpenSearch Hybrid Query combines Dense (k-NN) and Sparse (Match)
            search_body = {
                "size": top_k, # Final number of results after fusion
                "_source": ["chunk_id", "text", "metadata"],
                "query": {
                    "hybrid": {
                        "queries": [
                            # 1. Dense Component (k-NN)
                            {
                                "knn": {
                                    "embedding": {
                                        "vector": query_vector.tolist() if isinstance(query_vector, np.ndarray) else query_vector,
                                        "k": limit,
                                        "filter": {"term": {"metadata.language": language}} if language else None
                                    }
                                }
                            },
                            # 2. Sparse Component (BM25 Lexical)
                            {
                                "bool": {
                                    "must": [{"match": {"text": query}}],
                                    "filter": [{"term": {"metadata.language": language}}] if language else []
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
            source = hit["_source"]
            # Flatten the structure for backward compatibility
            result = {
                "chunk_id": source.get("chunk_id"),
                "text": source.get("text"),
                "page_content": source.get("text"),  # Alias for backward compatibility
                "score": hit["_score"]
            }
            # Add metadata fields
            if "metadata" in source:
                result.update(source["metadata"])
                result["metadata"] = source["metadata"]  # Keep nested structure too
            
            results.append(result)
            logger.debug(f"Result {idx+1}: doc_id={result.get('doc_id', 'N/A')}, score={hit['_score']:.4f}")

        logger.debug(f"Search operation completed successfully with {len(results)} results")
        return results

    @retry_on_transient_error(max_retries=3, initial_delay=5.0, backoff_multiplier=2.0)
    def check_db_populated(self):
        """
        Check if the database index exists and is populated.
        Includes retry logic for transient failures.
        """
        logger.debug(f"Checking if database is populated for index {self.index_name}")

        exists = self.client.indices.exists(index=self.index_name)
        logger.info(f"Database populated check: {exists}")
        return exists


    @retry_on_transient_error(max_retries=3, initial_delay=5.0, backoff_multiplier=2.0)
    def remove_docs_from_index(self, doc_ids: list[str]):
        """
        Delete all chunks associated with the specified document IDs from the index.

        This performs a targeted deletion of documents rather than wiping the entire index.
        Uses batch deletion for efficiency. Includes retry logic for transient failures.

        Args:
            doc_ids: List of document IDs whose chunks should be deleted from the index

        Returns:
            Number of chunks deleted
        """
        if not doc_ids:
            logger.warning(f"No document ids provided to remove from index {self.index_name}. Skipping.")
            return 0

        logger.debug(f"Starting targeted cleanup of {len(doc_ids)} documents in {self.index_name}")

        if not self.client.indices.exists(index=self.index_name):
            logger.info(f"Index {self.index_name} does not exist.")
            return 0

        # Construct terms query for batch deletion
        # 'metadata.doc_id' is the nested keyword field in the mapping
        delete_query = {
            "query": {
                "terms": {
                    "metadata.doc_id": doc_ids
                }
            }
        }

        response = self.client.delete_by_query(
            index=self.index_name,
            body=delete_query,
            params={
                "refresh": "true",
                "conflicts": "proceed"
            }
        )

        deleted_count = response.get("deleted", 0)
        logger.info(f"Successfully deleted {deleted_count} chunks for {len(doc_ids)} documents from {self.index_name}")

        return deleted_count


    @retry_on_transient_error(max_retries=3, initial_delay=5.0, backoff_multiplier=2.0)
    def delete_document_by_id(self, doc_id: str):
        """
        Delete all chunks associated with a specific document from the index.
        Includes retry logic for transient failures.

        Args:
            doc_id: The unique identifier of the document to delete

        Returns:
            Number of chunks deleted
        """
        logger.debug(f"Starting delete operation for document {doc_id}")

        if not self.client.indices.exists(index=self.index_name):
            logger.error(f"Index {self.index_name} does not exist, nothing to delete")
            return 0

        # 1. Immediate Refresh (Safety Check)
        # If a user ingests and then deletes immediately, OpenSearch might not have 'seen' the documents yet
        self.client.indices.refresh(index=self.index_name)

        # STEP 2: Perform the actual deletion
        delete_query = {
            "query": {
                "term": {
                    "metadata.doc_id": str(doc_id).strip()
                }
            }
        }

        response = self.client.delete_by_query(
            index=self.index_name,
            body=delete_query,
            params={
                        "refresh": "true",             # Update index stats immediately
                        "conflicts": "proceed",        # Ignore locks from concurrent indexing
                        "wait_for_completion": "true"  # Synchronous for the API response
                    },
        )

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
                logger.error(f"Matched {total_matched} documents but deleted {deleted_count} for document {doc_id} (possible version conflicts or failures)")
        return deleted_count
