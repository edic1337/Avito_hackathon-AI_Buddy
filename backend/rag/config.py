import os
from dataclasses import dataclass


def _env_bool(key: str, default: bool = False) -> bool:
    val = os.getenv(key)
    if val is None:
        return default
    return val.strip().lower() in ("1", "true", "yes", "on")


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
    def from_env(cls) -> "RagConfig":
        defaults = cls()
        return cls(
            database_url=os.getenv("DATABASE_URL", defaults.database_url),
            embedding_model=os.getenv("EMBEDDING_MODEL", defaults.embedding_model),
            embedding_device=os.getenv("EMBEDDING_DEVICE", defaults.embedding_device),
            embedding_batch_size=int(os.getenv("EMBEDDING_BATCH_SIZE", str(defaults.embedding_batch_size))),
            llm_base_url=os.getenv("LLM_BASE_URL", defaults.llm_base_url),
            llm_model=os.getenv("LLM_MODEL", defaults.llm_model),
            llm_api_key=os.getenv("LLM_API_KEY", ""),
            llm_mock=_env_bool("LLM_MOCK", False),
            top_k=int(os.getenv("TOP_K", str(defaults.top_k))),
            min_similarity=float(os.getenv("MIN_SIMILARITY", str(defaults.min_similarity))),
            context_token_budget=int(os.getenv("CONTEXT_TOKEN_BUDGET", str(defaults.context_token_budget))),
            use_reranker=_env_bool("USE_RERANKER", False),
            use_query_rewrite=_env_bool("USE_QUERY_REWRITE", False),
            tokenizer_model=os.getenv("TOKENIZER_MODEL", os.getenv("EMBEDDING_MODEL", defaults.tokenizer_model)),
            chunk_target_tokens=int(os.getenv("CHUNK_TARGET_TOKENS", str(defaults.chunk_target_tokens))),
            chunk_max_tokens=int(os.getenv("CHUNK_MAX_TOKENS", str(defaults.chunk_max_tokens))),
            chunk_overlap_tokens=int(os.getenv("CHUNK_OVERLAP_TOKENS", str(defaults.chunk_overlap_tokens))),
        )
