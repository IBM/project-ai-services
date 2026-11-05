"""
Minimal document ingestion for the local (Tilt) RAG stack.

Chunks .txt/.md files from a directory, embeds them through the local model server and
indexes them in OpenSearch using the same code path as the RAG services
(common.opensearch.OpensearchVectorStore). It is a lightweight stand-in for the digitize
service, which is not part of the local stack. PDFs/Office files are not supported.

Usage (the Tiltfile's `rag-ingest` resource sets the required environment):
  python hack/tilt/ingest.py <docs_dir> [--chunk-chars 1200] [--overlap 150]
"""
import argparse
import hashlib
import sys
from pathlib import Path

SUPPORTED = {".txt", ".md"}


def chunk_text(text: str, size: int, overlap: int):
    text = text.strip()
    start = 0
    while start < len(text):
        end = min(start + size, len(text))
        # prefer to break on a paragraph or sentence boundary
        if end < len(text):
            cut = max(text.rfind("\n\n", start, end), text.rfind(". ", start, end))
            if cut > start + size // 2:
                end = cut + 1
        piece = text[start:end].strip()
        if piece:
            yield piece
        if end >= len(text):
            break
        start = max(end - overlap, start + 1)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("docs_dir")
    parser.add_argument("--chunk-chars", type=int, default=1200)
    parser.add_argument("--overlap", type=int, default=150)
    args = parser.parse_args()

    docs_dir = Path(args.docs_dir).expanduser()
    files = sorted(p for p in docs_dir.rglob("*") if p.is_file() and p.suffix.lower() in SUPPORTED)
    if not files:
        print(f"No {', '.join(sorted(SUPPORTED))} files found in {docs_dir}", file=sys.stderr)
        return 1

    # Imported late: these read their configuration from the environment at import time.
    import common.db_utils as db
    import common.misc_utils as misc_utils
    from common.emb_utils import get_embedder
    from common.misc_utils import get_embedding_endpoint

    misc_utils.create_llm_session(pool_maxsize=4)
    emb = get_embedding_endpoint()
    embedder = get_embedder(emb["emb_model"], emb["emb_endpoint"], emb["max_model_len"])
    store = db.get_vector_store()

    for path in files:
        pieces = list(chunk_text(path.read_text(encoding="utf-8", errors="ignore"), args.chunk_chars, args.overlap))
        doc_id = hashlib.md5(str(path.resolve()).encode()).hexdigest()
        chunks = [
            {
                "page_content": piece,
                "filename": path.name,
                "doc_id": doc_id,
                "type": "text",
                "source": str(path.relative_to(docs_dir)),
                "language": "en",
                "chunk_index": i,
                "total_chunks": len(pieces),
            }
            for i, piece in enumerate(pieces)
        ]
        ok = store.insert_chunks(chunks, embedding=embedder)
        print(f"{'indexed' if ok else 'FAILED '} {path.name}: {len(chunks)} chunks")
        if not ok:
            return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
