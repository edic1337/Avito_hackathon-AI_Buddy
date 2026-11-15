"""python -m backend.rag.demo_chunking data/kb/demo_onboarding.md [--embed]"""

import argparse
import asyncio
import json
import math
import os
import tempfile
from dataclasses import asdict
from pathlib import Path

from .chunking import MarkdownChunker
from .config import RagConfig
from .documents import load_document
from .embeddings import QwenEmbedder
from .ingestion import embed_document
from .models import Document, TextChunk


def save_json(path: str | Path, payload: dict) -> Path:
    """Пишет UTF-8 JSON целиком, затем заменяет целевой файл."""
    path = Path(path).resolve()
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(
            mode="w", encoding="utf-8", dir=path.parent, suffix=".tmp", delete=False
        ) as file:
            temporary = Path(file.name)
            json.dump(payload, file, ensure_ascii=False, indent=2, allow_nan=False)
            file.write("\n")
        os.replace(temporary, path)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)
    return path


async def main() -> None:
    from transformers import AutoTokenizer

    parser = argparse.ArgumentParser()
    parser.add_argument("path", nargs="?", default="data/kb/demo_onboarding.md")
    parser.add_argument("--embed", action="store_true", help="Также загрузить модель и создать векторы")
    parser.add_argument("--plain", action="store_true", help="Локальный тест обычного Markdown без YAML, с тестовыми метаданными")
    parser.add_argument("--output", type=Path, help="Сохранить метаданные, чанки и полные эмбеддинги в JSON")
    args = parser.parse_args()
    if args.output is not None:
        if args.output.resolve() == Path(args.path).resolve():
            parser.error("Файл результата не должен совпадать с исходным документом")
        if args.output.is_dir():
            parser.error("--output должен указывать файл, а не папку")
    config = RagConfig.from_env()
    if args.plain:
        path = Path(args.path).resolve()
        if path.suffix.lower() not in (".md", ".markdown"):
            parser.error("Для --plain нужен файл .md или .markdown")
        document = Document(
            title=path.stem,
            text=await asyncio.to_thread(path.read_text, encoding="utf-8-sig"),
            source_url=path.as_uri(),
            origin="synthetic",
            min_role="intern",
            source_path=str(path),
        )
        print("Режим локального теста: метаданные тестовые, origin=synthetic. В БД ничего не записывается.", flush=True)
    else:
        document = await asyncio.to_thread(load_document, args.path)
    print("Загрузка токенизатора...", flush=True)
    tokenizer = await asyncio.to_thread(AutoTokenizer.from_pretrained, config.tokenizer_model)
    chunker = MarkdownChunker(
        tokenizer,
        target_tokens=config.chunk_target_tokens,
        max_tokens=config.chunk_max_tokens,
        overlap_tokens=config.chunk_overlap_tokens,
    )
    chunks = await asyncio.to_thread(chunker.chunk, document)
    print(f"Чанкинг завершён: {len(chunks)} фрагментов.", flush=True)
    if args.embed:
        print("Загрузка модели и вычисление эмбеддингов...", flush=True)
        chunks = await embed_document(document.title, chunks, QwenEmbedder(config))
    if args.output is not None:
        payload = {
            "schema_version": 1,
            "plain_test": args.plain,
            "document": asdict(document),
            "embedding_model": config.embedding_model if args.embed else None,
            "embedding_dimension": QwenEmbedder.dimension if args.embed else None,
            "chunking": {
                "tokenizer_model": config.tokenizer_model,
                "target_tokens": config.chunk_target_tokens,
                "max_tokens": config.chunk_max_tokens,
                "overlap_tokens": config.chunk_overlap_tokens,
            },
            "chunks": [
                {
                    "index": index,
                    **asdict(chunk),
                    "token_count": chunker.count_tokens(
                        TextChunk(chunk.section, chunk.text).embedding_text(document.title),
                        special_tokens=True,
                    ),
                }
                for index, chunk in enumerate(chunks, 1)
            ],
        }
        saved_path = await asyncio.to_thread(save_json, args.output, payload)
        print(f"Результат сохранён: {saved_path}", flush=True)
    print(f"{document.title}: origin={document.origin}, min_role={document.min_role}, team={document.team}")
    print(f"Чанков: {len(chunks)}")
    for index, chunk in enumerate(chunks, 1):
        count = chunker.count_tokens(TextChunk(chunk.section, chunk.text).embedding_text(document.title), special_tokens=True)
        print(f"\n[{index}] {chunk.section or '(без заголовка)'} | {count} токенов")
        print(chunk.text)
        if args.embed:
            print(f"Размерность вектора: {len(chunk.embedding)}")
            print(f"L2-норма: {math.hypot(*chunk.embedding):.6f}")
            print("Начало вектора:", [round(value, 6) for value in chunk.embedding[:6]])


if __name__ == "__main__":
    asyncio.run(main())
