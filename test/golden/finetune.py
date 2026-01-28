import os
from dotenv import load_dotenv
from openai import OpenAI

load_dotenv()

client = OpenAI(api_key=os.getenv("OPENAI_API_KEY"))
base_model = os.getenv("BASE_MODEL")

file = client.files.create(
    file=open("finetune_data.jsonl", "rb"),
    purpose="fine-tune"
)

job = client.fine_tuning.jobs.create(
    training_file=file.id,
    model=base_model,
    hyperparameters={
        "n_epochs": 3
    }
)

print("Fine-tuning started:")
print(job.id)
