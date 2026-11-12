"""Из корня проекта: python -m backend.rag.demo_embeddings"""

import asyncio

from .config import RagConfig
from .embeddings import QwenEmbedder
from .ingestion import embed_document
from .models import TextChunk


async def main() -> None:
    embedder = QwenEmbedder(RagConfig.from_env())
    # Синтетические примеры для проверки; это не инструкции Авито.
    chunks = [
        TextChunk("Доступы", "Для доступа к тестовому сервису обратитесь к наставнику."),
        TextChunk("Встречи", "Еженедельная встреча команды проходит по понедельникам."),
    ]
    print("Загрузка модели и создание эмбеддингов...", flush=True)
    embedded = await embed_document("Тестовая инструкция", chunks, embedder)
    query = await embedder.embed_query("У кого попросить доступ к сервису?")
    print(f"Чанков: {len(embedded)}, размерность: {len(query)}")

    # Для нормализованных векторов скалярное произведение равно косинусу.
    ranked = sorted(
        (
            (sum(q * v for q, v in zip(query, chunk.embedding, strict=True)), chunk)
            for chunk in embedded
        ),
        key=lambda item: item[0],
        reverse=True,
    )
    for score, chunk in ranked:
        print(f"{score:.4f} | {chunk.section}: {chunk.text}")


if __name__ == "__main__":
    asyncio.run(main())
