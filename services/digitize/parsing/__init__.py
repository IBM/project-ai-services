"""
Document parsing package.

Format-specific, read-only inspection utilities:
  - pdf.py       — page count, TOC, font-size analysis (pdfplumber / pypdfium2 / pdfminer)
  - docx.py      — page count estimation, TOC extraction, caption recovery
  - converter.py — Docling conversion engine wrapper (convert_doc, convert_document_format, …)
"""
