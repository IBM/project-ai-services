# Sample documents for the local RAG stack

AI Services is a platform for deploying AI applications such as retrieval augmented generation (RAG) assistants.

The catalog is the control plane. It stores application definitions in PostgreSQL and exposes a REST API and a web UI.
The chatbot service answers questions by retrieving relevant chunks from OpenSearch, reranking them with a
cross-encoder reranker model, and sending them as context to a large language model.

The similarity service exposes the same retrieval pipeline as a standalone API at /v1/similarity-search.
Embeddings are produced by the granite-embedding-278m-multilingual model and ranked with bge-reranker-v2-m3.

## Local development

For local development on macOS the RAG stack runs under Tilt. The embedding and reranker models run on the
developer machine, while the language model is served remotely by IBM watsonx.ai.
