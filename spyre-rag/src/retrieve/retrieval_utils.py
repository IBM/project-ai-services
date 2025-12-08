import base64


def format_table_html(table_html):
    """
    Ensures that the table HTML is properly formatted.
    This is a basic check to wrap the table inside a <table> tag if it isn't already wrapped.
    """
    if not table_html.startswith("<table"):
        table_html = f"<table>{table_html}</table>"
    return table_html

def show_document_content(retrieved_documents, scores):
    html_content = ""
    
    for idx, (doc, score) in enumerate(zip(retrieved_documents, scores)):
        doc_type = doc.get("type")
        
        # Document Header with Score
        document_header = f'<h4>Document {idx + 1} (Score: {score:.4f}), (Doc: {doc.get("filename")})</h4>'
        html_content += document_header
        
        # If the document is an image
        if doc_type == "image":
            image_path = doc.get("source")
            with open(image_path, "rb") as image_file:
                encoded_string = base64.b64encode(image_file.read()).decode('utf-8')
            image_html = f'<div style="border: 1px solid #ccc; padding: 10px; background-color: #f0f0f0; width: 100%; margin-top: 20px;">'
            image_html += f'<img src="data:image/jpeg;base64,{encoded_string}" alt="Image {doc.get("chunk_id")}" style="width: 50%; height: auto;" />'
            image_summary = f'<p><strong>Image Summary:</strong> {doc.get("page_content")}</p>'
            image_html += f'{image_summary}</div>'
            html_content += image_html

        # If the document is a table
        elif doc_type == "table":
            table_html = doc.get("source")
            if table_html:
                table_html = format_table_html(table_html)  # Ensure proper HTML wrapping
                table_summary = f'<p><strong>Table Summary:</strong> {doc.get("page_content")}</p>'
                html_content += f'<div style="margin-top: 20px; border: 1px solid #ccc; padding: 10px; background-color: #f0f0f0;">{table_html}<br>{table_summary}</div>'

        # If the document is plain text
        elif doc_type == "text":
            converted_doc_string = doc.get("page_content").replace("\n", "<br>")
            html_content += f'<div style="margin-top: 20px; padding: 10px; border: 1px solid #ccc; background-color: #f0f0f0;">{converted_doc_string}</div>'

    return html_content


def retrieve_documents(query, emb_model, emb_endpoint, max_tokens, vectorstore, top_k, mode="hybrid", language='en'):
    results = vectorstore.search(query, emb_model, emb_endpoint, max_tokens, top_k, mode=mode, language=language)

    retrieved_documents = []
    scores = []

    for hit in results:
        doc = {
            "page_content": hit.get("page_content", ""),
            "filename": hit.get("filename", ""),
            "type": hit.get("type", ""),
            "source": hit.get("source", ""),
            "chunk_id": hit.get("chunk_id", "")
        }
        retrieved_documents.append(doc)

        # For dense hits from Milvus, we expect `.score` or `.distance`.
        score = hit.get("rrf_score") or hit.get("score") or hit.get("distance") or 0.0
        scores.append(score)

    return retrieved_documents, scores
