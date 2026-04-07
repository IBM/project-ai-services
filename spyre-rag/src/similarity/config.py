"""
Configuration settings for the similarity search service.
Values can be overridden via environment variables.
"""
import os

# Number of results to return when top_k is not specified by the caller
NUM_CHUNKS_POST_SEARCH = int(os.getenv("NUM_CHUNKS_POST_SEARCH", "10"))
