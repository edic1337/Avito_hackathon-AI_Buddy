"""Контракты внешних зависимостей RAG.

Protocol описывает нужные методы. Реализации не обязаны наследоваться
от этих классов: достаточно совпадающих сигнатур и поведения.
Здесь нет подключений к БД, загрузки моделей или сетевых запросов.
"""

from collections.abc import AsyncIterator, Sequence
from typing import Protocol

from .models import Chunk, Message


class Embedder(Protocol):
    """Одна модель и размерность для запросов и документов."""

    async def embed_query(self, text: str) -> list[float]:
        """Нормализованный вектор запроса с query-инструкцией модели."""
        ...

    async def embed_documents(self, texts: Sequence[str]) -> list[list[float]]:
        """Нормализованные векторы без query-инструкции, в порядке texts.

        Пустому входу соответствует пустой список результатов.
        """
        ...


class KnowledgeRepository(Protocol):
    """Поиск только по документам, доступным указанной роли и команде.

    Фильтрация выполняется внутри хранилища до выбора top-limit.
    Неизвестная роль должна вызывать ValueError, а не открывать доступ.
    team=None разрешает только документы без ограничения по команде.
    Методы возвращают до limit чанков, от лучшего к худшему; при отсутствии
    совпадений — пустой список. Ошибки подключения не маскируются под [].
    """

    async def search_vector(
        self,
        embedding: Sequence[float],
        *,
        role: str,
        team: str | None,
        limit: int,
    ) -> list[Chunk]:
        """score и cosine_score равны cosine similarity (1 - distance)."""
        ...

    async def search_text(
        self,
        question: str,
        *,
        role: str,
        team: str | None,
        limit: int,
    ) -> list[Chunk]:
        """score — ранг полнотекстового поиска, cosine_score не заполняется.

        Этот score нельзя сравнивать с порогом cosine similarity.
        """
        ...


class LLMClient(Protocol):
    def stream(
        self,
        *,
        system_prompt: str,
        question: str,
        history: Sequence[Message] = (),
    ) -> AsyncIterator[str]:
        """Поток частей ответа; question не должен дублироваться в history.

        Контекст источников уже включён в system_prompt вызывающим кодом.
        Здесь возвращается только текст, без SSE и событий интерфейса.
        Метод объявлен через def, чтобы реализация могла быть async-генератором:
        потребитель использует async for, без предварительного await.
        """
        ...
