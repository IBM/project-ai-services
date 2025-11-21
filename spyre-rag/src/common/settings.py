import os
import json
from dataclasses import dataclass
from typing import Optional
from common.misc_utils import get_logger

logger = get_logger("settings")

@dataclass(frozen = True)
class Prompts:
    llm_classify: str
    table_summary: str
    query_vllm:    str
    query_vllm_stream: str

    def __post_init__(self):
        if any(prompt in (None, "") for prompt in (
            self.llm_classify,
            self.table_summary,
            self.query_vllm,
            self.query_vllm_stream,
        )):
            raise ValueError(f"One or more prompt variables are missing or empty.")

    @classmethod
    def from_dict(cls, data: dict):
        if not isinstance(data, dict):
            raise ValueError("Prompts element missing or malformed in the settings")

        return cls(
            llm_classify = data.get("llm_classify"),
            table_summary = data.get("table_summary"),
            query_vllm = data.get("query_vllm"),
            query_vllm_stream = data.get("query_vllm_stream")
        )

@dataclass(frozen=True)
class Settings:
    prompts: Prompts
    score_threshold: float

    def __post_init__(self):
        default_score_threshold = 0.4
        if not (isinstance(self.score_threshold, float) and 0 < self.score_threshold < 1):
            object.__setattr__(self, "score_threshold", default_score_threshold)
            logger.warning(f"Setting score threshold to default '{default_score_threshold}' as it is missing or malformed in the settings")

    @classmethod
    def from_dict(cls, data: dict):
        return cls(
            prompts = Prompts.from_dict(data.get("prompts")),
            score_threshold = data.get("score_threshold")
        )

    @classmethod
    def from_file(cls, path: str):
        try:
            with open(path, "r", encoding="utf-8") as f:
                return cls.from_dict(json.load(f))
        except FileNotFoundError as e:
            raise FileNotFoundError(f"JSON file not found at: {path}") from e
        except json.JSONDecodeError as e:
            raise ValueError(f"Error parsing JSON at {path}") from e

    @classmethod
    def load(cls):
        path = os.getenv("SETTINGS_PATH")
        if not (path and os.path.exists(path)):
            base_dir = os.path.dirname(os.path.abspath(__file__))
            path = os.path.join(base_dir, "..", "settings.json")
            path = os.path.normpath(path)
        return cls.from_file(path)


_settings_instance: Optional[Settings] = None

def get_settings():
    global _settings_instance

    if _settings_instance is None:
        _settings_instance = Settings.load()

    return _settings_instance