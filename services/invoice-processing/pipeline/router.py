"""
Invoice routing and pipeline dispatch stubs.
"""

from typing import Any


def detect_input_type(filename: str) -> str:
    """Detect whether input is PDF or image based on filename extension."""
    lower = filename.lower()
    if lower.endswith(".pdf"):
        return "pdf"
    if lower.endswith((".png", ".jpg", ".jpeg", ".tiff", ".bmp", ".webp")):
        return "image"
    raise ValueError(f"Unsupported file type for invoice processing: {filename}")
