"""Подготовка чанков одного документа к записи в БД.

Полный путь: файл → Document → TextChunk → EmbeddedChunk.
SQL-запись подключается отдельно.
"""

import asyncio
from collections.abc import Sequence
from pathlib import Path
from typing import TYPE_CHECKING

from .interfaces import Embedder
from .documents import load_document
from .models import Document, EmbeddedChunk, TextChunk

if TYPE_CHECKING:
    from .chunking import MarkdownChunker


async def embed_document(
    title: str,
    chunks: Sequence[TextChunk],
    embedder: Embedder,
) -> list[EmbeddedChunk]:
    """Строит векторы готовых чанков одного документа.

    Эмбеддинг получает «Название | Раздел\nТекст», исходный текст сохраняется.
    Результаты соответствуют порядку входных чанков. Нет записи в БД и ID:
    позже вызывающий код сохранит документ и его чанки одной транзакцией.
    """
    chunks = list(chunks)
    if not title.strip():
        raise ValueError("Название документа не должно быть пустым")
    if not chunks:
        return []
    if any(not chunk.text.strip() for chunk in chunks):
        raise ValueError("Документ содержит пустой чанк")

    texts = [chunk.embedding_text(title) for chunk in chunks]
    vectors = await embedder.embed_documents(texts)
    if len(vectors) != len(chunks):
        raise ValueError("Число векторов не совпадает с числом чанков")

    return [
        EmbeddedChunk(section=chunk.section, text=chunk.text, embedding=vector)
        for chunk, vector in zip(chunks, vectors, strict=True)
    ]


async def prepare_document(
    path: str | Path,
    chunker: "MarkdownChunker",
    embedder: Embedder,
) -> tuple[Document, list[EmbeddedChunk]]:
    """Читает Markdown, разбивает его и создаёт эмбеддинги.

    Метаданные возвращаются вместе с чанками, чтобы при сохранении в БД
    не потерялись origin, min_role, team и ссылка на источник.
    """
    document = await asyncio.to_thread(load_document, path)
    chunks = await asyncio.to_thread(chunker.chunk, document)
    embedded = await embed_document(document.title, chunks, embedder)
    return document, embedded
