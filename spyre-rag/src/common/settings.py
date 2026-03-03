import os
import json
from dataclasses import dataclass
from typing import Optional
from common.misc_utils import get_logger

logger = get_logger("settings")

@dataclass(frozen = True)
class Prompts:
    query_vllm_stream: str
    table_summary_and_classify: str
    summarize_system_prompt: str
    summarize_user_prompt_with_length: str
    summarize_user_prompt_without_length: str

    def __post_init__(self):
        if any(prompt in (None, "") for prompt in (
            self.query_vllm_stream,
            self.table_summary_and_classify,
            self.summarize_system_prompt,
            self.summarize_user_prompt_with_length,
            self.summarize_user_prompt_without_length

        )):
            raise ValueError(f"One or more prompt variables are missing or empty.")

    @classmethod
    def from_dict(cls, data: dict):
        if not isinstance(data, dict):
            raise ValueError("Prompts element missing or malformed in the settings")

        return cls(
            query_vllm_stream = data.get("query_vllm_stream"),
            table_summary_and_classify = data.get("table_summary_and_classify"),
            summarize_system_prompt = data.get("summarize_system_prompt"),
            summarize_user_prompt_with_length = data.get("summarize_user_prompt_with_length"),
            summarize_user_prompt_without_length = data.get("summarize_user_prompt_without_length")
        )


@dataclass(frozen = True)
class ContextLengths:
    granite_3_3_8b_instruct: int

    def __post_init__(self):
        if any(prompt in (None, "") for prompt in (
            self.granite_3_3_8b_instruct,
        )):
            raise ValueError(f"One or more context length variables are missing or empty.")

    @classmethod
    def from_dict(cls, data: dict):
        if not isinstance(data, dict):
            raise ValueError("Context length element missing or malformed in the settings")

        return cls(
            granite_3_3_8b_instruct = data.get("ibm-granite/granite-3.3-8b-instruct")
        )


@dataclass(frozen=True)
class TokenToWordRatios:
    en: float

    def __post_init__(self):
        if any(prompt in (None, "") for prompt in (
                self.en,
        )):
            raise ValueError(f"One or more token to word ratio variables are missing or empty.")

    @classmethod
    def from_dict(cls, data: dict):
        if not isinstance(data, dict):
            raise ValueError("Token to word ratio element missing or malformed in the settings")

        return cls(
            en=data.get("en")
        )

@dataclass(frozen=True)
class Settings:
    prompts: Prompts
    context_lengths: ContextLengths
    token_to_word_ratios: TokenToWordRatios
    score_threshold: float
    max_concurrent_requests: int
    num_chunks_post_search: int
    num_chunks_post_reranker: int
    llm_max_tokens: int
    temperature: float
    max_input_length: int
    prompt_template_token_count: int
    summarization_coefficient: float
    summarization_prompt_token_count: int
    summarization_temperature: float
    summarization_stop_words: str


    def __post_init__(self):
        default_score_threshold = 0.4
        default_max_concurrent_requests = 32
        default_num_chunks_post_search = 10
        default_num_chunks_post_reranker = 3
        default_llm_max_tokens = 512
        default_temperature = 0.0
        default_max_input_length = 6000
        default_prompt_template_token_count = 250
        default_summarization_coefficient = 0.2
        default_summarization_prompt_token_count = 100
        default_summarization_temperature = 0.2
        default_summarization_stop_words = "Keywords, Note, ***"

        if not (isinstance(self.score_threshold, float) and 0 < self.score_threshold < 1):
            object.__setattr__(self, "score_threshold", default_score_threshold)
            logger.warning(f"Setting score threshold to default '{default_score_threshold}' as it is missing or malformed in the settings")

        if not (isinstance(self.max_concurrent_requests, int) and self.max_concurrent_requests > 0):
            object.__setattr__(self, "max_concurrent_requests", default_max_concurrent_requests)
            logger.warning(
                f"Setting max_concurrent_requests to default '{default_max_concurrent_requests}' as it is missing or malformed in the settings"
            )

        if not (isinstance(self.num_chunks_post_search, int) and 5 < self.num_chunks_post_search <= 15):
            object.__setattr__(self, "num_chunks_post_search", default_num_chunks_post_search)
            logger.warning(f"Setting num_chunks_post_search to default '{default_num_chunks_post_search}' as it is missing or malformed in the settings")

        if not (isinstance(self.num_chunks_post_reranker, int) and 1 < self.num_chunks_post_reranker <= 5):
            object.__setattr__(self, "num_chunks_post_reranker", default_num_chunks_post_reranker)
            logger.warning(f"Setting num_chunks_post_reranker to default '{default_num_chunks_post_reranker}' as it is missing or malformed in the settings")

        if not (isinstance(self.llm_max_tokens, int) and self.llm_max_tokens > 0):
            object.__setattr__(self, "llm_max_tokens", default_llm_max_tokens)
            logger.warning(
                f"Setting llm_max_tokens to default '{default_llm_max_tokens}' as it is missing or malformed in the settings"
            )

        if not (isinstance(self.temperature, float) and 0 <= self.temperature < 1):
            object.__setattr__(self, "temperature", default_temperature)
            logger.warning(f"Setting temperature to default '{default_temperature}' as it is missing or malformed in the settings")

        if not (isinstance(self.max_input_length, int) and 3000 <= self.max_input_length <= 32000):
            object.__setattr__(self, "max_input_length", default_max_input_length)
            logger.warning(f"Setting max_input_length to default '{default_max_input_length}' as it is missing or malformed in the settings")

        if not isinstance(self.prompt_template_token_count, int):
            object.__setattr__(self, "prompt_template_token_count", default_prompt_template_token_count)
            logger.warning(f"Setting prompt_template_token_count to default '{default_prompt_template_token_count}' as it is missing in the settings")

        if not isinstance(self.summarization_coefficient, float):
            object.__setattr__(self, "summarization_coefficient", default_summarization_coefficient)
            logger.warning(f"Setting summarization_coefficient to default '{default_summarization_coefficient}' as it is missing in the settings")

        if not isinstance(self.summarization_prompt_token_count, int):
            object.__setattr__(self, "summarization_prompt_token_count", default_summarization_prompt_token_count)
            logger.warning(f"Setting summarization_prompt_token_count to default '{default_summarization_prompt_token_count}' as it is missing in the settings")

        if not isinstance(self.summarization_temperature, float):
            object.__setattr__(self, "summarization_temperature", default_summarization_temperature)
            logger.warning(f"Setting summarization_temperature to default '{default_summarization_temperature}' as it is missing in the settings")

        if not isinstance(self.summarization_stop_words, str):
            object.__setattr__(self, "summarization_stop_words", default_summarization_stop_words)
            logger.warning(f"Setting summarization_stop_words to default '{default_summarization_stop_words}' as it is missing in the settings")


    @classmethod
    def from_dict(cls, data: dict):
        return cls(
            prompts = Prompts.from_dict(data.get("prompts")),
            context_lengths=ContextLengths.from_dict(data.get("context_lengths")),
            token_to_word_ratios=TokenToWordRatios.from_dict(data.get("token_to_word_ratios")),
            score_threshold = data.get("score_threshold"),
            max_concurrent_requests = data.get("max_concurrent_requests"),
            num_chunks_post_search = data.get("num_chunks_post_search"),
            num_chunks_post_reranker = data.get("num_chunks_post_reranker"),
            llm_max_tokens = data.get("llm_max_tokens"),
            temperature = data.get("temperature"),
            max_input_length = data.get ("max_input_length"),
            prompt_template_token_count = data.get("prompt_template_token_count"),
            summarization_coefficient = data.get("summarization_coefficient"),
            summarization_prompt_token_count = data.get("summarization_prompt_token_count"),
            summarization_temperature = data.get("summarization_temperature"),
            summarization_stop_words = data.get("summarization_stop_words")
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