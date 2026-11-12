"""Проверки без скачивания весов: python -m unittest discover -s backend/tests -v"""

import asyncio
import math
import os
import time
import types
import unittest
from unittest.mock import patch

from backend.rag.config import RagConfig
from backend.rag.embeddings import QwenEmbedder
from backend.rag.ingestion import embed_document
from backend.rag.models import TextChunk


class FakeArray:
    def __init__(self, rows):
        self.rows = rows

    def tolist(self):
        return self.rows


class FakeModel:
    def __init__(self):
        self.prompts = {"query": "Instruct: retrieve passages\nQuery: "}
        self.calls = []
        self.active = 0
        self.max_active = 0

    def get_sentence_embedding_dimension(self):
        return 1024

    def encode(self, texts, **kwargs):
        self.active += 1
        self.max_active = max(self.max_active, self.active)
        try:
            time.sleep(0.005)
            self.calls.append((list(texts), kwargs))
            return FakeArray([[1.0] + [0.0] * 1023 for _ in texts])
        finally:
            self.active -= 1


class EmbeddingTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.model = FakeModel()
        self.loads = []

        def load(*args, **kwargs):
            self.loads.append((args, kwargs))
            return self.model

        module = types.ModuleType("sentence_transformers")
        module.SentenceTransformer = load
        self.module_patch = patch.dict("sys.modules", {"sentence_transformers": module})
        self.module_patch.start()
        self.addCleanup(self.module_patch.stop)
        self.embedder = QwenEmbedder(RagConfig(embedding_batch_size=2))

    async def test_query_and_documents_use_different_prompts_and_one_model(self):
        self.assertEqual(self.loads, [])
        query = await self.embedder.embed_query("Как получить доступ?")
        docs = await self.embedder.embed_documents(["Первый текст", "Второй текст"])
        self.assertEqual(len(self.loads), 1)
        self.assertEqual(len(query), 1024)
        self.assertEqual(len(docs), 2)
        self.assertEqual(self.model.calls[0][1]["prompt_name"], "query")
        self.assertEqual(self.model.calls[1][1]["prompt"], "")
        self.assertNotIn("prompt_name", self.model.calls[1][1])
        self.assertTrue(self.model.calls[1][1]["normalize_embeddings"])
        self.assertEqual(self.model.calls[1][1]["batch_size"], 2)
        self.assertEqual(self.loads[0][1]["device"], "cpu")

    async def test_empty_documents_do_not_load_model(self):
        self.assertEqual(await self.embedder.embed_documents([]), [])
        self.assertEqual(self.loads, [])

    async def test_blank_inputs_fail_before_loading(self):
        with self.assertRaises(ValueError):
            await self.embedder.embed_query("  ")
        with self.assertRaises(ValueError):
            await self.embedder.embed_documents(["Текст", "\n"])
        self.assertEqual(self.loads, [])

    async def test_concurrent_calls_do_not_load_twice_or_run_model_in_parallel(self):
        await asyncio.gather(
            self.embedder.embed_query("Вопрос"),
            self.embedder.embed_documents(["Документ"]),
        )
        self.assertEqual(len(self.loads), 1)
        self.assertEqual(self.model.max_active, 1)

    async def test_incompatible_model_fails_before_encoding(self):
        self.model.get_sentence_embedding_dimension = lambda: 384
        with self.assertRaisesRegex(ValueError, "размерность"):
            await self.embedder.embed_query("Вопрос")
        self.assertEqual(self.model.calls, [])

    async def test_missing_query_prompt_fails(self):
        self.model.prompts = {}
        with self.assertRaisesRegex(ValueError, "query-промпт"):
            await self.embedder.embed_query("Вопрос")

    async def test_new_dimension_method_is_preferred(self):
        self.model.get_embedding_dimension = lambda: 1024

        def deprecated():
            raise AssertionError("Deprecated method should not be called")

        self.model.get_sentence_embedding_dimension = deprecated
        vector = await self.embedder.embed_query("Вопрос")
        self.assertEqual(len(vector), 1024)

    async def test_rounding_drift_is_normalized_instead_of_rejected(self):
        # Норма квадратов ~1.0019: прежняя проверка ошибочно отклоняла
        # такой вектор, хотя его можно безопасно нормализовать.
        row = [0.03128] * 1024
        self.model.encode = lambda texts, **kwargs: FakeArray([row.copy() for _ in texts])
        query = await self.embedder.embed_query("Вопрос")
        docs = await self.embedder.embed_documents(["Документ"])
        for vector in (query, docs[0]):
            self.assertAlmostEqual(math.hypot(*vector), 1.0, places=12)
            self.assertAlmostEqual(vector[0], 1 / 32, places=12)

    async def test_renormalization_preserves_direction_and_small_vectors(self):
        for scale in (1.0, 1e-300):
            row = [3 * scale, 4 * scale] + [0.0] * 1022
            self.model.encode = lambda texts, row=row, **kwargs: FakeArray([row.copy() for _ in texts])
            vector = await self.embedder.embed_query("Вопрос")
            self.assertAlmostEqual(vector[0], 0.6, places=12)
            self.assertAlmostEqual(vector[1], 0.8, places=12)
            self.assertAlmostEqual(math.hypot(*vector), 1.0, places=12)

    async def test_bad_vector_outputs_are_rejected(self):
        for rows in ([], [[1.0]], [[math.nan] * 1024], [[math.inf] * 1024], [[0.0] * 1024]):
            with self.subTest(rows_count=len(rows)):
                self.model.encode = lambda *args, rows=rows, **kwargs: FakeArray(rows)
                with self.assertRaises(ValueError):
                    await self.embedder.embed_query("Вопрос")

    async def test_document_prefix_and_vector_alignment(self):
        class RecordingEmbedder:
            async def embed_documents(self, texts):
                self.texts = texts
                return [[1.0, 0.0], [0.0, 1.0]]

        embedder = RecordingEmbedder()
        chunks = [TextChunk("Доступ", "Первый текст"), TextChunk("", "Второй текст")]
        result = await embed_document("Инструкция", chunks, embedder)
        self.assertEqual(embedder.texts, [
            "Инструкция | Доступ\nПервый текст", "Инструкция\nВторой текст",
        ])
        self.assertEqual([item.text for item in result], [item.text for item in chunks])
        self.assertEqual(result[0].embedding, [1.0, 0.0])
        self.assertEqual(result[1].embedding, [0.0, 1.0])
        self.assertEqual(chunks[0].text, "Первый текст")

    async def test_document_empty_chunks_and_invalid_text(self):
        self.assertEqual(await embed_document("Название", [], self.embedder), [])
        with self.assertRaises(ValueError):
            await embed_document("", [], self.embedder)
        with self.assertRaises(ValueError):
            await embed_document("Название", [TextChunk("Раздел", " ")], self.embedder)
        self.assertEqual(self.loads, [])

    async def test_document_embedding_count_mismatch(self):
        class BrokenEmbedder:
            async def embed_documents(self, texts):
                return []

        with self.assertRaisesRegex(ValueError, "Число векторов"):
            await embed_document("Название", [TextChunk("", "Текст")], BrokenEmbedder())


class ConfigTests(unittest.TestCase):
    def test_defaults_and_local_embedding_environment(self):
        with patch.dict(os.environ, {}, clear=True):
            self.assertEqual(RagConfig.from_env(), RagConfig())
        with patch.dict(os.environ, {
            "EMBEDDING_MODEL": "/models/qwen",
            "EMBEDDING_DEVICE": "cuda",
            "EMBEDDING_BATCH_SIZE": "4",
        }, clear=True):
            config = RagConfig.from_env()
            self.assertEqual(config.embedding_model, "/models/qwen")
            self.assertEqual(config.embedding_device, "cuda")
            self.assertEqual(config.embedding_batch_size, 4)

    def test_invalid_batch_size(self):
        with self.assertRaises(ValueError):
            QwenEmbedder(RagConfig(embedding_batch_size=0))


if __name__ == "__main__":
    unittest.main()
