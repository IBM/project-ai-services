import json
import csv
import argparse
import os

parser = argparse.ArgumentParser(description='Convert JSONL file to CSV with question, answer, and source_link columns')
parser.add_argument('input_file', help='Path to input JSONL file')
parser.add_argument('output_file', help='Path to output CSV file')
args = parser.parse_args()

with open(args.output_file, 'w', newline='', encoding='utf-8') as csvfile:
    writer = csv.writer(csvfile)

    writer.writerow(['golden_question', 'golden_answer', 'source_link'])

    with open(args.input_file, 'r', encoding='utf-8') as jsonlfile:
        for line in jsonlfile:
            item = json.loads(line)

            question = item.get('question', '')
            answer = item.get('answer', [{}])
            if not type(answer) is list or len(answer) == 0:
                answer = [{}]
                print("Warning: missing answer for ", question)
            answer_content = answer[0].get('content', '')

            full_path = item.get('filename', '')
            filename = full_path.rsplit('/', 1)[-1]
            if question and answer_content and filename:
                writer.writerow([question, answer_content, filename])

print(f"Successfully converted {args.input_file} to {args.output_file}")
