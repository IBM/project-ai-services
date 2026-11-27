"""Training worker: ``python -m forecast.training <job_file> <data_path> <output_dir>``."""
import sys

from forecast.training.main import main

sys.exit(main(sys.argv[1:]))
