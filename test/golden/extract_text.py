import os
from PyPDF2 import PdfReader

def extract_text_from_pdfs(pdf_dir: str) -> str:
    text = []
    for file in os.listdir(pdf_dir):
        if file.endswith(".pdf"):
            reader = PdfReader(os.path.join(pdf_dir, file))
            for page in reader.pages:
                page_text = page.extract_text()
                if page_text:
                    text.append(page_text)
    return "\n".join(text)

if __name__ == "__main__":
    corpus = extract_text_from_pdfs("pdfs")
    with open("corpus.txt", "w", encoding="utf-8") as f:
        f.write(corpus)
