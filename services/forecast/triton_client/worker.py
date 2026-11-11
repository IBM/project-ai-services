"""
Training worker entry point: ``python -m forecast.triton_client.worker <job_id>``.

The API starts this module as a subprocess, in its own process group. The
training itself (read the job, load the data, train, export and publish the
forecaster) is not implemented yet.
"""
import argparse
import sys


def parse_args(argv: list[str]) -> argparse.Namespace:
    """Parse the command line of the worker."""
    parser = argparse.ArgumentParser(prog="forecast.triton_client.worker", description="Run one training job.")
    parser.add_argument("job_id", help="The jobs.id of the training job to run")
    return parser.parse_args(argv)


def main(argv: list[str]) -> int:
    """Run the job named on the command line and return the exit code."""
    args = parse_args(argv)
    print(f"forecast.triton_client.worker: training for job {args.job_id} is not implemented yet", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
