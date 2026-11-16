"""Отдельная проверка реального токенизатора, без весов эмбеддинг-модели.

Из корня: python -m backend.tests.check_chunking_qwen
"""

from transformers import AutoTokenizer

from backend.rag.chunking import MarkdownChunker
from backend.rag.config import RagConfig
from backend.rag.models import Document


def main() -> None:
    config = RagConfig.from_env()
    tokenizer = AutoTokenizer.from_pretrained(config.tokenizer_model)
    chunker = MarkdownChunker(tokenizer)
    cases = {
        "long_paragraph": "# Доступы\n\n" + " ".join(f"маркер{i}: уточните порядок получения доступа." for i in range(300)),
        "code": "# Код\n\n```python\n" + "\n".join(f'print("строка{i}")' for i in range(200)) + "\n```",
        "table": "# Справочник\n\n| Сервис | Контакт |\n| --- | --- |\n" + "\n".join(f"| сервис{i} | наставник{i} |" for i in range(180)),
        "list": "# Шаги\n\n" + "\n".join(f"- Шаг {i}: проверьте доступ и сохраните результат проверки." for i in range(180)),
        "long_word": "# Длинная строка\n\n" + "ДлиннаяСтрокаБезПробелов" * 800,
    }
    for name, text in cases.items():
        document = Document("Проверка", text, "demo://stress", "synthetic", "intern")
        chunks = chunker.chunk(document)
        counts = [chunker.count_tokens(c.embedding_text(document.title), special_tokens=True) for c in chunks]
        assert chunks and max(counts) <= 400, (name, counts)
        combined = "\n".join(c.text for c in chunks)
        assert "\ufffd" not in combined
        if name == "code":
            assert all(f'print("строка{i}")' in combined for i in range(200))
            assert all(c.text.count("```") == 2 for c in chunks)
        if name == "table":
            assert all(f"| сервис{i} | наставник{i} |" in combined for i in range(180))
        if name == "long_paragraph":
            assert all(f"маркер{i}:" in combined for i in range(300))
        if name == "long_word":
            assert "".join(c.text for c in chunks) == "ДлиннаяСтрокаБезПробелов" * 800
        print(f"{name}: chunks={len(chunks)}, min_tokens={min(counts)}, max_tokens={max(counts)}")


if __name__ == "__main__":
    main()
