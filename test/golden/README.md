# Creating Golden Dataset out of a given knowledge base of pdfs
This is a way to create golden dataset from a given batch of pdfs by using them to finetune an LLM and then generating the golden dataset.

## Steps

Put your knowledge base pdfs in pdfs/ directory
```bash
$ python3 -m venv venv
$ source venv/bin/activate
$ pip install -r requirements.txt 
$ python3 extract_text.py
```
The .env file should contain OPENAI_API_KEY and BASE_MODEL values. Please refer sample.env
This will extract text from the pdfs in the pdfs/ directory and save it into a file called corpus.txt
```bash
python3 generate_seed_qa.py
```
This will create seed QA pairs and save them in a file called seed_qa.json
```bash
python3 prepare_finetune_data.py
```
This script will create a file containing finetuned data in finetune_data.jsonl from the seed QA pair
```bash
python3 finetune.py
```
This script will submit a job to finetune the model BASE_MODEL in .env
```bash
python3 generate_golden_qa.py
```
This script will generate golden QA pairs form the finetuned model and save it in the file golden_dataset.json