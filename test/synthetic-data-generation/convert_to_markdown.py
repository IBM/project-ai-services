import pdfplumber
import re
from pathlib import Path

INPUT_DIR = Path("Documents")
OUTPUT_DIR = Path("markdown")

OUTPUT_DIR.mkdir(exist_ok=True)

HEADER_FOOTER_PATTERNS = [
    r"^IBM .* Technical Overview$",
    r"^AIX Version$"
    r"^© Copyright IBM Corp\.",
    r"^IBM Redbooks$"
]

def is_noise(line: str) -> bool:
    return any(re.match(p, line) for p in HEADER_FOOTER_PATTERNS)

def clean_line(line: str) -> str:
    line = line.strip()
    line = re.sub(r"\s+", " ", line)
    return line

def heading_level(line: str):
    if re.match(r"^Chapter\s+\d+\.", line):
        return 1
    if re.match(r"^\d+\.\d+\.\d+\s+", line):
        return 3
    if re.match(r"^\d+\.\d+\s+", line):
        return 2
    if re.match(r"^\d+\s+", line):
        return 1
    return None


def split_into_chunks(content: str, chunk_size: int = 6000):
    """Split content into chunks of approximately chunk_size characters"""
    chunks = []
    current_chunk = []
    current_length = 0

    lines = content.split("\n")

    for line in lines:
        line_length = len(line) + 1

        # If adding this line exceeds chunk_size and we have content, save chunk
        if current_length + line_length > chunk_size and current_chunk:
            chunks.append("\n".join(current_chunk))
            current_chunk = [line]
            current_length = line_length
        else:
            current_chunk.append(line)
            current_length += line_length

    if current_chunk:
        chunks.append("\n".join(current_chunk))

    return chunks

def extract_full_markdown(pdf_path: Path) -> str:
    """Extract full markdown content from PDF"""
    md = []
    paragraph_buffer = []

    def flush_paragraph():
        if paragraph_buffer:
            md.append(" ".join(paragraph_buffer))
            paragraph_buffer.clear()

    with pdfplumber.open(pdf_path) as pdf:
        for page in pdf.pages:
            text = page.extract_text(layout=True)
            if not text:
                continue

            for raw in text.split("\n"):
                line = clean_line(raw)

                if not line or is_noise(line):
                    flush_paragraph()
                    continue

                level = heading_level(line)
                if level:
                    flush_paragraph()
                    md.append(f"{'#' * level} {line}")
                    md.append("")
                else:
                    paragraph_buffer.append(line)

    flush_paragraph()
    return "\n".join(md)

def convert_pdf_to_markdown_chunks(pdf_path: Path, chunks_per_pdf: int):
    """Convert PDF to markdown and split into specified number of chunks"""
    full_content = extract_full_markdown(pdf_path)

    if not full_content:
        print(f"Warning: No content extracted from {pdf_path.name}")
        return

    total_length = len(full_content)
    chunk_size = total_length // chunks_per_pdf if chunks_per_pdf > 0 else 6000

    chunk_size = max(chunk_size, 1000)

    chunks = split_into_chunks(full_content, chunk_size)

    for i, chunk in enumerate(chunks, 1):
        output_md = OUTPUT_DIR / f"{pdf_path.stem}_chunk{i}.md"
        output_md.write_text(chunk, encoding="utf-8")
        print(f"  Created {output_md.name} ({len(chunk)} chars)")

    return len(chunks)

def convert_pdf_to_markdown(pdf_path: Path, output_md: Path):
    md = []
    paragraph_buffer = []

    def flush_paragraph():
        if paragraph_buffer:
            md.append(" ".join(paragraph_buffer))
            paragraph_buffer.clear()

    with pdfplumber.open(pdf_path) as pdf:
        for page in pdf.pages:
            text = page.extract_text(layout=True)
            if not text:
                continue

            for raw in text.split("\n"):
                line = clean_line(raw)

                if not line or is_noise(line):
                    flush_paragraph()
                    continue

                level = heading_level(line)
                if level:
                    flush_paragraph()
                    md.append(f"{'#' * level} {line}")
                    md.append("")
                else:
                    paragraph_buffer.append(line)

    flush_paragraph()
    output_md.write_text("\n".join(md), encoding="utf-8")

def batch_convert():
    pdf_files = list(INPUT_DIR.glob("*.pdf"))

    if not pdf_files:
        print(f"No PDFs found in {INPUT_DIR}")
        return

    while True:
        try:
            total_chunks = int(input(f"Enter total number of chunks for {len(pdf_files)} PDF file(s): "))
            if total_chunks <= 0:
                print("Please enter a positive number.")
                continue
            break
        except ValueError:
            print("Please enter a valid number.")

    chunks_per_pdf = total_chunks // len(pdf_files)
    remainder = total_chunks % len(pdf_files)
    print(f"\nDistributing {total_chunks} chunks across {len(pdf_files)} PDF(s)")
    print(f"Base chunks per PDF: {chunks_per_pdf}")
    if remainder > 0:
        print(f"First {remainder} PDF(s) will get 1 extra chunk\n")

    total_created = 0
    for idx, pdf in enumerate(pdf_files):
        # Give extra chunk to first 'remainder' PDFs
        chunks_for_this_pdf = chunks_per_pdf + (1 if idx < remainder else 0)

        print(f"Converting {pdf.name} → {chunks_for_this_pdf} chunk(s)")
        created = convert_pdf_to_markdown_chunks(pdf, chunks_for_this_pdf)
        total_created += created if created else 0

    print(f"\nBatch conversion completed. Created {total_created} chunk files.")


if __name__ == "__main__":
    batch_convert()
