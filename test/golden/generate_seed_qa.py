import os
import json
import re
from dotenv import load_dotenv
from openai import OpenAI

load_dotenv()

def extract_json(text: str):
    """
    Extract JSON from a string that may contain Markdown code fences.
    """
    match = re.search(r"```json\s*(.*?)\s*", text, re.DOTALL)
    if match:
        text = match.group(1)
    print("Text:  ====> ", text)
    return json.loads(text)

client = OpenAI(api_key=os.getenv("OPENAI_API_KEY"))
model = os.getenv("BASE_MODEL")


with open("corpus.txt", "r", encoding="utf-8") as f:
    corpus = f.read()

print("Length of corpus: ", len(corpus))
prompt = f"""
You are a domain expert.

From the following text, generate 50 diverse, factual,
domain-specific question–answer pairs.

Rules:
- Answers must be strictly grounded in the text
- Answers must be 3-4 sentences long.
- Avoid generic questions
- Include edge cases, assumptions, and limitations
- Output valid JSON ONLY
- Do NOT use markdown.
- Do NOT wrap in code fences.
- Do NOT add commentary.
- Return raw JSON only.

Text:
{corpus[:12000]}
"""
print("going to LLM ")
response = client.chat.completions.create(
    model=model,
    messages=[{"role": "user", "content": prompt}],
    temperature=0.3,
    max_tokens=6000
)
print("Response: ====>", response)
try:
    qa_pairs = json.loads(response.choices[0].message.content)
except json.decoder.JSONDecodeError:
    qa_pairs = extract_json(response.choices[0].message.content)

with open("seed_qa.json", "w", encoding="utf-8") as f:
    json.dump(qa_pairs, f, indent=2)
