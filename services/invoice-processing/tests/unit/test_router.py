"""
Unit tests for pipeline router stub.
"""

import pytest
from pipeline.router import detect_input_type


def test_detect_input_type_pdf():
    assert detect_input_type("sample_invoice.pdf") == "pdf"
    assert detect_input_type("INVOICE.PDF") == "pdf"


def test_detect_input_type_image():
    assert detect_input_type("receipt.png") == "image"
    assert detect_input_type("photo.JPG") == "image"
    assert detect_input_type("doc.tiff") == "image"
    assert detect_input_type("scan.webp") == "image"


def test_detect_input_type_invalid():
    with pytest.raises(ValueError, match="Unsupported file type"):
        detect_input_type("invoice.txt")
