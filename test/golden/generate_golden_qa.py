import os
import json
from dotenv import load_dotenv
from openai import OpenAI

load_dotenv()

client = OpenAI(api_key=os.getenv("OPENAI_API_KEY"))

FINE_TUNED_MODEL = "ft:gpt-4o-mini-2024-07-18:personal::CuYD6qgg"

prompt = """
Generate exactly 50 question–answer pairs with answers of 3-4 lines 
STRICTLY grounded in the domain of IBM Power Virtual Server.

RULES:
- Every answer MUST be directly supported by the source knowledge used during training
- If the answer cannot be inferred from that knowledge, reply: "Unknown"
- NEVER invent facts
- Return ONLY RAW JSON.
- Do NOT use markdown.
- Do NOT use code fences.
- Do NOT add commentary.

Format:
[
  {
      "question": "What are the minimum supported versions for AIX and Linux on IBM Power Virtual Server?",
      "answer": "The minimum supported versions for AIX on IBM Power Virtual Server are IBM AIX 7.1 and 7.2. For Linux, the supported distributions include CentOS-Stream-8, SUSE Linux Enterprise Server 12 (minimum level SP4 + Kernel 4.12.14-95.54.1), SUSE Linux Enterprise Server 15 (minimum level SP1 + Kernel 4.12.14-197.45-default), and Red Hat Enterprise Linux versions 8.1 to 8.6. Users must ensure they are using these specific versions to guarantee compatibility."
    },
    {
      "question": "What is the significance of the 'Notices' section in the IBM Power Virtual Server guide?",
      "answer": "The 'Notices' section provides important legal information regarding the use, duplication, or disclosure of the product and its supporting information. It is particularly relevant for U.S. Government users, as it outlines restrictions based on GSA ADP Schedule contracts with IBM Corporation. Users are advised to read this section carefully before utilizing the product to understand their rights and limitations."
    },
    {
      "question": "What are the key features of the IBM Power Virtual Server?",
      "answer": "Key features of the IBM Power Virtual Server include workspaces and instances for organizing resources, compute options for processing power, and server placement groups for optimizing performance. Additionally, it offers VM pinning for resource allocation, a shared processor pool for efficient CPU usage, and robust storage solutions. Networking capabilities and high availability options further enhance its functionality for enterprise environments."
    },
]
"""

response = client.chat.completions.create(
    model=FINE_TUNED_MODEL,
    messages=[{"role": "user", "content": prompt}],
    temperature=0.1,
    max_tokens=8000
)
print("response=======>", response)
golden_qa = json.loads(response.choices[0].message.content)

with open("golden_dataset.json", "w", encoding="utf-8") as f:
    json.dump(golden_qa, f, indent=2)

print("Golden dataset created: golden_dataset.json")
