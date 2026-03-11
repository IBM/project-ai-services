from docling.utils import model_downloader
from pathlib import Path
import os

# Use DOCLING_MODELS_PATH environment variable, fallback to /var/docling-models
OUTPUT_DIR = Path(os.getenv("DOCLING_MODELS_PATH", "/var/docling-models"))
OUTPUT_DIR.mkdir(parents=True, exist_ok=True)

print(f"Downloading ds4sd--docling-models (Layout & TableFormer) to: {OUTPUT_DIR}")

model_downloader.download_models(
    output_dir=OUTPUT_DIR,
    with_layout=True,
    with_tableformer=True,
    with_easyocr=False,
    with_code_formula=False,
    with_picture_classifier=False
)

print("Download complete.")
