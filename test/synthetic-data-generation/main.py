import os
import argparse
import glob
from sdg_hub.core.flow import Flow
from datasets import Dataset


def read_md_files(folder):
    """Read Markdown files in a folder

    Args:
        folder (String): String containing path to the folder

    Returns:
        list: List of dictionaries. Each item contains the document and associated metedata info
    """
    files = glob.glob(os.path.join(folder, "*md"))

    documents = []
    for each_file in files:
        try:
            with open(each_file, "r", encoding="utf-8") as f:
                content = f.read()
                document = {"filename": each_file, "document": content}
                documents.append(document)
        except Exception as e:
            print(f"Warning: Could not read file {each_file}: {e}")
    return documents


def main():
    parser = argparse.ArgumentParser(
        description="Run an Agri SDG text generation flow."
    )

    parser.add_argument(
        "--flow_yaml",
        type=str,
        default="sdg_flow.yaml",
        help="Path to the YAML file defining the flow.",
    )

    parser.add_argument(
        "--input_dir",
        type=str,
        default="markdown/",
        help="Directory containing Markdown files.",
    )

    parser.add_argument(
        "--output_dir",
        type=str,
        default="output/",
        help="Directory to save output files.",
    )

    parser.add_argument(
        "--model",
        type=str,
        default="text-completion-openai/meta-llama/llama-3-3-70b-instruct",
        help="Model identifier to use for the flow.",
    )

    parser.add_argument(
        "--api_base",
        type=str,
        default="http://127.0.0.1:8080/v1/",
        help="Base URL of the API.",
    )

    parser.add_argument(
        "--api_key", type=str, default="abcd", help="API key for authentication."
    )

    args = parser.parse_args()

    # Load a flow
    flow = Flow.from_yaml(args.flow_yaml)

    # Discover recommended models


    # Configure model at runtime
    flow.set_model_config(
        model=args.model,
        api_base=args.api_base,
        api_key=args.api_key,
    )
    all_documents = read_md_files(args.input_dir)

    dataset = Dataset.from_list(all_documents)

    # Execute the complete flow
    result = flow.generate(dataset, checkpoint_dir=f"{args.output_dir}/checkpoint_dir")
    result.to_json(f"{args.output_dir}/golden_dataset.jsonl", force_ascii=False)


if __name__ == "__main__":
    main()
