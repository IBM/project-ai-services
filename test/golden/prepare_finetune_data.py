import json




with open("seed_qa.json", "r", encoding="utf-8") as f:
    questions = json.load(f)

qa_pairs = questions["questions"]

with open("finetune_data.jsonl", "w", encoding="utf-8") as f:
    for qa in qa_pairs:
        record = {
            "messages": [
                {"role": "user", "content": qa["question"]},
                {"role": "assistant", "content": qa["answer"]}
            ]
        }
        f.write(json.dumps(record) + "\n")
