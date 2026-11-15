import asyncio
import re
import tempfile
import unittest
from pathlib import Path

from backend.rag.chunking import MarkdownChunker
from backend.rag.documents import load_document, parse_document
from backend.rag.ingestion import prepare_document
from backend.rag.models import Document


class CharacterTokenizer:
    """Строгий тестовый бюджет: один Unicode-символ = один токен."""

    def encode(self, text, *, add_special_tokens=False, truncation=False):
        assert not truncation
        return list(text) + (["BOS", "EOS"] if add_special_tokens else [])


def document(text, title="Doc"):
    return Document(title, text, "demo://test", "synthetic", "intern")


HEADER = """---
title: Demo
source_url: demo://test
origin: synthetic
min_role: buddy
team: backend
tags: [demo, tools]
---
"""


class ChunkingTests(unittest.TestCase):
    def setUp(self):
        self.chunker = MarkdownChunker(CharacterTokenizer(), target_tokens=100, max_tokens=150, overlap_tokens=20)

    def check_budget(self, doc, chunks):
        for chunk in chunks:
            self.assertTrue(chunk.text.strip())
            self.assertLessEqual(self.chunker.count_tokens(chunk.embedding_text(doc.title), special_tokens=True), 150)

    def test_heading_hierarchy_and_separate_sections(self):
        doc = document("# Root\n\nintro\n\n## Access\n\naccess body\n\n### VPN\n\nvpn body\n\n## Meetings\n\nmeeting body")
        chunks = self.chunker.chunk(doc)
        self.assertEqual([c.section for c in chunks], ["Root", "Root > Access", "Root > Access > VPN", "Root > Meetings"])
        self.assertNotIn("access body", chunks[-1].text)
        self.check_budget(doc, chunks)

    def test_setext_heading_and_preamble(self):
        chunks = self.chunker.chunk(document("preamble\n\nHeading\n=======\n\nbody"))
        self.assertEqual([(c.section, c.text) for c in chunks], [("", "preamble"), ("Heading", "body")])

    def test_short_paragraphs_pack_together(self):
        chunks = self.chunker.chunk(document("# A\n\nFirst.\n\nSecond.\n\nThird."))
        self.assertEqual(len(chunks), 1)
        self.assertEqual(chunks[0].text, "First.\n\nSecond.\n\nThird.")

    def test_long_paragraph_coverage_and_cyrillic(self):
        words = [f"номер{i}" for i in range(150)]
        doc = document("# Раздел\n\n" + " ".join(words))
        chunks = self.chunker.chunk(doc)
        self.assertGreater(len(chunks), 5)
        self.check_budget(doc, chunks)
        found = "\n".join(c.text for c in chunks)
        for word in words:
            self.assertIn(word, found)
        self.assertNotIn("\ufffd", found)
        self.assertEqual(chunks, self.chunker.chunk(doc))

    def test_overlap_is_bounded_and_uses_complete_words(self):
        text = " ".join(f"word{i}" for i in range(90))
        doc = document(text)
        chunks = self.chunker.chunk(doc)
        previous_words = chunks[0].text.split()
        next_words = chunks[1].text.split()
        shared = []
        for size in range(1, min(len(previous_words), len(next_words)) + 1):
            if previous_words[-size:] == next_words[:size]:
                shared = next_words[:size]
        self.assertTrue(shared)
        self.assertLessEqual(len(" ".join(shared)), 20)
        self.check_budget(doc, chunks)

    def test_zero_overlap(self):
        chunker = MarkdownChunker(CharacterTokenizer(), target_tokens=80, max_tokens=100, overlap_tokens=0)
        words = [f"word{i}" for i in range(70)]
        chunks = chunker.chunk(document(" ".join(words)))
        self.assertEqual(" ".join(c.text for c in chunks).split(), words)

    def test_fence_heading_is_not_a_section_and_code_is_balanced(self):
        source = "\n".join(f"print('line{i}')" for i in range(30))
        doc = document("# Real\n\n```python\n# not a heading\n" + source + "\n```\n\nAfter.")
        chunks = self.chunker.chunk(doc)
        self.assertEqual({c.section for c in chunks}, {"Real"})
        self.check_budget(doc, chunks)
        code_chunks = [c for c in chunks if "```python" in c.text]
        self.assertGreater(len(code_chunks), 1)
        for c in code_chunks:
            self.assertEqual(c.text.count("```"), 2)
        combined = "\n".join(c.text for c in code_chunks)
        for i in range(30):
            self.assertIn(f"print('line{i}')", combined)

    def test_tilde_and_unclosed_fences_are_closed(self):
        chunks = self.chunker.chunk(document("~~~sql\nSELECT 1;\n"))
        self.assertEqual(chunks[0].text, "~~~sql\nSELECT 1;\n~~~")

    def test_indented_code_with_backticks_gets_safe_fence(self):
        chunks = self.chunker.chunk(document("    x = '```'\n    print(x)\n"))
        self.assertTrue(chunks[0].text.startswith("````\n"))
        self.assertTrue(chunks[0].text.endswith("````"))

    def test_oversized_code_line_is_not_lost(self):
        code = "X" * 500
        doc = document("```\n" + code + "\n```")
        chunks = self.chunker.chunk(doc)
        self.check_budget(doc, chunks)
        self.assertEqual(sum(c.text.count("X") for c in chunks), 500)

    def test_table_headers_repeat_and_rows_are_preserved(self):
        rows = [f"| id{i} | value{i} |" for i in range(30)]
        header = "| ID | Value |\n| --- | --- |"
        doc = document(header + "\n" + "\n".join(rows))
        chunks = self.chunker.chunk(doc)
        self.assertGreater(len(chunks), 1)
        self.check_budget(doc, chunks)
        for chunk in chunks:
            self.assertTrue(chunk.text.startswith(header))
        combined = "\n".join(c.text for c in chunks)
        for row in rows:
            self.assertEqual(combined.count(row), 1)

    def test_table_row_that_cannot_fit_fails_explicitly(self):
        with self.assertRaisesRegex(ValueError, "Строка таблицы"):
            self.chunker.chunk(document("| A | B |\n| --- | --- |\n| x | " + "z" * 300 + " |"))

    def test_list_items_and_nested_list_stay_together_when_possible(self):
        first = "- Parent\n  - Nested A\n  - Nested B"
        rest = "\n".join(f"- Item{i}: " + "detail " * 7 for i in range(12))
        doc = document(first + "\n" + rest)
        chunks = self.chunker.chunk(doc)
        self.assertIn(first, chunks[0].text)
        self.check_budget(doc, chunks)
        for i in range(12):
            self.assertIn(f"Item{i}:", "\n".join(c.text for c in chunks))

    def test_long_word_progress_and_exact_coverage(self):
        doc = document("Ж" * 1000)
        chunks = self.chunker.chunk(doc)
        self.check_budget(doc, chunks)
        self.assertEqual("".join(c.text for c in chunks), "Ж" * 1000)

    def test_reference_link_definition_is_not_dropped(self):
        doc = document("# Links\n\n[guide][ref]\n\n[ref]: https://example.com/guide")
        self.assertIn("[ref]: https://example.com/guide", "\n".join(c.text for c in self.chunker.chunk(doc)))

    def test_prefix_counts_against_budget(self):
        doc = document("# Heading\n\n" + "word " * 80, title="Title " * 5)
        chunks = self.chunker.chunk(doc)
        self.check_budget(doc, chunks)
        with self.assertRaisesRegex(ValueError, "весь бюджет"):
            self.chunker.chunk(document("body", title="t" * 200))

    def test_empty_and_heading_only_documents(self):
        self.assertEqual(self.chunker.chunk(document("")), [])
        self.assertEqual(self.chunker.chunk(document("# A\n\n## B")), [])

    def test_invalid_settings(self):
        for target, maximum, overlap in ((0, 10, 0), (20, 10, 0), (10, 10, 10), (10, 20, -1)):
            with self.subTest(settings=(target, maximum, overlap)), self.assertRaises(ValueError):
                MarkdownChunker(CharacterTokenizer(), target_tokens=target, max_tokens=maximum, overlap_tokens=overlap)


class DocumentTests(unittest.TestCase):
    def test_frontmatter_fields_and_body(self):
        doc = parse_document("\ufeff" + HEADER + "\n# Title\n\nBody\n", source_path="demo.md")
        self.assertEqual(doc.title, "Demo")
        self.assertEqual(doc.text, "# Title\n\nBody")
        self.assertEqual(doc.origin, "synthetic")
        self.assertEqual(doc.min_role, "buddy")
        self.assertEqual(doc.team, "backend")
        self.assertEqual(doc.tags, ("demo", "tools"))
        self.assertEqual(doc.source_path, "demo.md")

    def test_invalid_metadata(self):
        inputs = [
            "# Missing metadata", "---\ntitle: Unclosed",
            "---\n- list\n---\nBody",
            HEADER.replace("origin: synthetic", "origin: unknown"),
            HEADER.replace("min_role: buddy", "min_role: admin"),
            HEADER.replace("title: Demo", "title: 123"),
            HEADER.replace("team: backend", "team: []"),
            HEADER.replace("tags: [demo, tools]", "tags: demo"),
            HEADER.replace("title: Demo", "title: Demo\ntitle: Other"),
            HEADER.replace("title: Demo", "title: ["),
        ]
        for source in inputs:
            with self.subTest(source=source), self.assertRaises(ValueError):
                parse_document(source)

    def test_no_implicit_public_access(self):
        with self.assertRaisesRegex(ValueError, "min_role"):
            parse_document(HEADER.replace("min_role: buddy\n", ""))
        with self.assertRaisesRegex(ValueError, "origin"):
            parse_document(HEADER.replace("origin: synthetic\n", ""))

    def test_loader_preserves_leading_code_indentation(self):
        doc = parse_document(HEADER + "\n    print('hello')\n")
        self.assertTrue(doc.text.startswith("    "))
        chunker = MarkdownChunker(CharacterTokenizer())
        self.assertTrue(chunker.chunk(doc)[0].text.startswith("```\n"))

    def test_load_utf8_bom_and_reject_unsupported_format(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "doc.md"
            path.write_text(HEADER + "Body", encoding="utf-8-sig")
            self.assertEqual(load_document(path).text, "Body")
            with self.assertRaisesRegex(ValueError, "только"):
                load_document(Path(directory) / "doc.pdf")


class PreparationTests(unittest.IsolatedAsyncioTestCase):
    async def test_file_to_chunks_to_vectors_preserves_metadata_and_order(self):
        class RecordingEmbedder:
            async def embed_documents(self, texts):
                self.texts = texts
                return [[float(index)] for index in range(len(texts))]

        embedder = RecordingEmbedder()
        chunker = MarkdownChunker(CharacterTokenizer(), target_tokens=100, max_tokens=150, overlap_tokens=10)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "doc.md"
            path.write_text(HEADER + "# One\n\nBody one\n\n# Two\n\nBody two", encoding="utf-8")
            doc, embedded = await prepare_document(path, chunker, embedder)
        self.assertEqual(doc.min_role, "buddy")
        self.assertEqual(doc.team, "backend")
        self.assertEqual(doc.origin, "synthetic")
        self.assertEqual([c.section for c in embedded], ["One", "Two"])
        self.assertEqual([c.embedding for c in embedded], [[0.0], [1.0]])
        self.assertEqual(embedder.texts, ["Demo | One\nBody one", "Demo | Two\nBody two"])


if __name__ == "__main__":
    unittest.main()
