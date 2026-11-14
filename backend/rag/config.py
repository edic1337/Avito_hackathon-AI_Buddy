"""Project configuration: defaults < project .env < process environment."""

import os
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping

from dotenv import dotenv_values

PROJECT_ROOT = Path(__file__).resolve().parents[2]


def _env_bool(key: str, default: bool = False, *, values: Mapping[str, str] | None = None) -> bool:
    value = (os.environ if values is None else values).get(key)
    if value is None:
        return default
    return value.strip().lower() in ("1", "true", "yes", "on")


@dataclass(frozen=True, slots=True)
class RagConfig:
    database_url: str = "postgresql://postgres:postgres@localhost:5432/avito_buddy"
    embedding_model: str = "Qwen/Qwen3-Embedding-0.6B"
    embedding_device: str = "cpu"
    embedding_batch_size: int = 8
    llm_base_url: str = "https://api.openai.com/v1"
    llm_model: str = "gpt-4o-mini"
    llm_api_key: str = ""
    llm_mock: bool = False
    top_k: int = 6
    min_similarity: float = 0.45
    context_token_budget: int = 3000
    use_reranker: bool = False
    use_query_rewrite: bool = False
    tokenizer_model: str = "Qwen/Qwen3-Embedding-0.6B"
    chunk_target_tokens: int = 300
    chunk_max_tokens: int = 400
    chunk_overlap_tokens: int = 40

    @classmethod
    def from_env(cls, env_file: str | Path | None = ".env") -> "RagConfig":
        """Load a project-relative .env without modifying os.environ.

        Explicit process variables always win. Pass env_file=None to read
        only process variables (useful for tests and managed deployments).
        Dollar signs in credentials are literal; interpolation is disabled.
        """
        values: dict[str, str] = {}
        if env_file is not None:
            path = Path(env_file)
            if not path.is_absolute():
                path = PROJECT_ROOT / path
            if path.is_file():
                parsed = dotenv_values(path, encoding="utf-8-sig", interpolate=False)
                values.update({key: value for key, value in parsed.items() if value is not None})
        values.update(os.environ)
        defaults = cls()
        config = cls(
            database_url=values.get("DATABASE_URL", defaults.database_url),
            embedding_model=values.get("EMBEDDING_MODEL", defaults.embedding_model),
            embedding_device=values.get("EMBEDDING_DEVICE", defaults.embedding_device),
            embedding_batch_size=int(values.get("EMBEDDING_BATCH_SIZE", str(defaults.embedding_batch_size))),
            llm_base_url=values.get("LLM_BASE_URL", defaults.llm_base_url),
            llm_model=values.get("LLM_MODEL", defaults.llm_model),
            llm_api_key=values.get("LLM_API_KEY", ""),
            llm_mock=_env_bool("LLM_MOCK", values=values),
            top_k=int(values.get("TOP_K", str(defaults.top_k))),
            min_similarity=float(values.get("MIN_SIMILARITY", str(defaults.min_similarity))),
            context_token_budget=int(values.get("CONTEXT_TOKEN_BUDGET", str(defaults.context_token_budget))),
            use_reranker=_env_bool("USE_RERANKER", values=values),
            use_query_rewrite=_env_bool("USE_QUERY_REWRITE", values=values),
            tokenizer_model=values.get("TOKENIZER_MODEL", values.get("EMBEDDING_MODEL", defaults.tokenizer_model)),
            chunk_target_tokens=int(values.get("CHUNK_TARGET_TOKENS", str(defaults.chunk_target_tokens))),
            chunk_max_tokens=int(values.get("CHUNK_MAX_TOKENS", str(defaults.chunk_max_tokens))),
            chunk_overlap_tokens=int(values.get("CHUNK_OVERLAP_TOKENS", str(defaults.chunk_overlap_tokens))),
        )
        if config.embedding_device not in ("cpu", "cuda", "mps"):
            raise ValueError("EMBEDDING_DEVICE must be cpu, cuda or mps")
        if not config.embedding_model.strip() or not config.tokenizer_model.strip():
            raise ValueError("EMBEDDING_MODEL and TOKENIZER_MODEL must not be blank")
        if config.embedding_batch_size < 1:
            raise ValueError("EMBEDDING_BATCH_SIZE must be positive")
        if not 0 <= config.chunk_overlap_tokens < config.chunk_target_tokens <= config.chunk_max_tokens:
            raise ValueError("Need 0 <= CHUNK_OVERLAP_TOKENS < CHUNK_TARGET_TOKENS <= CHUNK_MAX_TOKENS")
        if config.top_k < 1 or config.context_token_budget < 1:
            raise ValueError("TOP_K and CONTEXT_TOKEN_BUDGET must be positive")
        if not -1 <= config.min_similarity <= 1:
            raise ValueError("MIN_SIMILARITY must be between -1 and 1")
        return config
