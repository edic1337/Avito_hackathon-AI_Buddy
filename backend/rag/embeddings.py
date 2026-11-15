"""Локальная модель эмбеддингов, общая для загрузки базы и поиска."""

import asyncio
import math
import threading
from collections.abc import Sequence
from typing import TYPE_CHECKING

from .config import RagConfig

if TYPE_CHECKING:
    from sentence_transformers import SentenceTransformer


class QwenEmbedder:
    """Реализует Embedder из interfaces.py без обязательного наследования.

    Создавайте один экземпляр при запуске приложения и передавайте его
    в поиск и загрузку документов. Модель загружается при первом запросе.
    На первом запуске нужны интернет и место для весов; затем работает кеш.
    EMBEDDING_MODEL может также указывать на локальную папку модели.
    """

    dimension = 1024

    def __init__(self, config: RagConfig):
        if config.embedding_batch_size < 1:
            raise ValueError("EMBEDDING_BATCH_SIZE должен быть положительным")
        self._config = config
        self._model: SentenceTransformer | None = None
        # Lock внутри рабочего потока защищает модель даже при отмене
        # ожидающей async-задачи: to_thread сам по себе поток не останавливает.
        self._lock = threading.Lock()

    async def embed_query(self, text: str) -> list[float]:
        if not text.strip():
            raise ValueError("Вопрос не должен быть пустым")
        vectors = await asyncio.to_thread(self._encode, [text], True)
        return vectors[0]

    async def embed_documents(self, texts: Sequence[str]) -> list[list[float]]:
        texts = list(texts)
        if not texts:
            return []
        if any(not text.strip() for text in texts):
            raise ValueError("Тексты для эмбеддингов не должны быть пустыми")
        return await asyncio.to_thread(self._encode, texts, False)

    def _load_model(self) -> "SentenceTransformer":
        # Импорт здесь позволяет проверять остальной RAG без torch и весов.
        from sentence_transformers import SentenceTransformer

        model = SentenceTransformer(
            self._config.embedding_model,
            device=self._config.embedding_device,
        )
        # В новых Sentence Transformers метод переименован; поддерживаем
        # также старые версии, не вызывая deprecated-метод на новых.
        get_dimension = getattr(model, "get_embedding_dimension", None)
        if get_dimension is None:
            get_dimension = model.get_sentence_embedding_dimension
        dimension = get_dimension()
        if dimension != self.dimension:
            raise ValueError(
                f"Ожидалась размерность {self.dimension}, получена {dimension}. "
                "Модель должна соответствовать размерности в БД."
            )
        if "query" not in model.prompts:
            raise ValueError("У модели отсутствует Qwen query-промпт")
        return model

    def _encode(self, texts: list[str], is_query: bool) -> list[list[float]]:
        with self._lock:
            if self._model is None:
                self._model = self._load_model()

            # Явный пустой prompt не даёт применить default_prompt к документам.
            prompt_options = {"prompt_name": "query"} if is_query else {"prompt": ""}
            result = self._model.encode(
                texts,
                batch_size=self._config.embedding_batch_size,
                normalize_embeddings=True,
                convert_to_numpy=True,
                show_progress_bar=False,
                **prompt_options,
            )
            vectors = result.tolist()
            if len(vectors) != len(texts):
                raise ValueError("Число векторов не совпадает с числом текстов")
            for index, vector in enumerate(vectors):
                if len(vector) != self.dimension:
                    raise ValueError("Модель вернула вектор неверной размерности")
                if not all(math.isfinite(value) for value in vector):
                    raise ValueError("Модель вернула NaN или бесконечность")
                # normalize_embeddings=True может выполниться в пониженной
                # точности модели. После tolist числа — Python float:
                # нормализуем ещё раз, сохраняя направление вектора.
                # hypot устойчивее суммы квадратов для очень малых чисел.
                norm = math.hypot(*vector)
                if not math.isfinite(norm) or norm == 0:
                    raise ValueError(f"Модель вернула некорректный вектор: L2-норма={norm}")
                vectors[index] = [value / norm for value in vector]
            return vectors
