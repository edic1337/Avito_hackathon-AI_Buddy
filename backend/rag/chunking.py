"""Структурный чанкинг Markdown с реальным токенным бюджетом.

Разбор синтаксиса выполняет markdown-it-py. Токенизатор передаётся снаружи:
для работы — AutoTokenizer Qwen, для unit-тестов — маленькая тестовая реализация.
"""

import re
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any

from markdown_it import MarkdownIt

from .models import Document, TextChunk


@dataclass(frozen=True)
class _Block:
    text: str
    kind: str = "paragraph"
    content: str = ""
    fence: str = ""
    info: str = ""
    items: tuple[str, ...] = ()


class MarkdownChunker:
    def __init__(
        self,
        tokenizer: Any,
        *,
        target_tokens: int = 300,
        max_tokens: int = 400,
        overlap_tokens: int = 40,
    ):
        if not 0 <= overlap_tokens < target_tokens <= max_tokens:
            raise ValueError("Нужно 0 <= overlap_tokens < target_tokens <= max_tokens")
        self.tokenizer = tokenizer
        self.target_tokens = target_tokens
        self.max_tokens = max_tokens
        self.overlap_tokens = overlap_tokens
        self._parser = MarkdownIt("commonmark").enable("table")

    def count_tokens(self, text: str, *, special_tokens: bool = False) -> int:
        return len(self.tokenizer.encode(text, add_special_tokens=special_tokens, truncation=False))

    def chunk(self, document: Document) -> list[TextChunk]:
        if not document.title.strip():
            raise ValueError("Документу нужно название")
        result = []
        for section, blocks in self._sections(document.text):
            if not blocks:
                continue

            def fits(text: str, limit: int = self.max_tokens) -> bool:
                full = TextChunk(section, text).embedding_text(document.title)
                return self.count_tokens(full, special_tokens=True) <= limit

            if not fits("x"):
                raise ValueError(f"Название и путь раздела {section!r} занимают весь бюджет")

            pieces = []
            for block in blocks:
                pieces.extend(self._split_block(block, fits))
            current: list[_Block] = []

            def joined(items: list[_Block]) -> str:
                return "\n\n".join(item.text for item in items)

            for piece in pieces:
                text = joined(current)
                full = TextChunk(section, text).embedding_text(document.title)
                if current and (
                    self.count_tokens(full, special_tokens=True) >= self.target_tokens
                    or not fits(joined(current + [piece]))
                ):
                    result.append(TextChunk(section, text))
                    overlap = self._overlap(current[-1])
                    current = [_Block(overlap)] if overlap and fits(overlap + "\n\n" + piece.text) else []
                current.append(piece)
            if current:
                result.append(TextChunk(section, joined(current)))

        # Последняя проверка применяется к точной строке, которую получит embedder.
        for chunk in result:
            if self.count_tokens(chunk.embedding_text(document.title), special_tokens=True) > self.max_tokens:
                raise ValueError("Чанк превысил токенный бюджет")
        return result

    def _sections(self, text: str) -> list[tuple[str, list[_Block]]]:
        lines = text.splitlines(keepends=True)
        tokens = self._parser.parse(text)
        headings: list[tuple[int, str]] = []
        section = ""
        blocks: list[_Block] = []
        sections = []
        cursor = 0
        for index, token in enumerate(tokens):
            if token.level != 0 or token.map is None or token.nesting == -1:
                continue
            start, end = token.map
            # В частности, сохраняем определения ссылок [name]: URL,
            # которые парсер не выводит отдельными block-токенами.
            gap = "".join(lines[cursor:start]).strip()
            if gap:
                blocks.append(_Block(gap, "other"))
            cursor = end
            if token.type == "heading_open":
                if blocks:
                    sections.append((section, blocks))
                blocks = []
                level = int(token.tag[1:])
                heading = tokens[index + 1].content.strip()
                while headings and headings[-1][0] >= level:
                    headings.pop()
                headings.append((level, heading))
                section = " > ".join(title for _, title in headings)
                continue
            raw = "".join(lines[start:end]).rstrip("\r\n")
            if not raw.strip():
                continue
            if token.type in ("fence", "code_block"):
                # Для indented code используем ограждение; для fenced — исходный маркер.
                marker = token.markup if token.type == "fence" else "```"
                if token.type == "code_block":
                    marker = "`" * max(3, max((len(m.group()) + 1 for m in re.finditer(r"`+", token.content)), default=3))
                blocks.append(_Block(raw, "code", token.content, marker, token.info))
            elif token.type == "table_open":
                blocks.append(_Block(raw, "table"))
            elif token.type in ("bullet_list_open", "ordered_list_open"):
                items = []
                for child in tokens[index + 1:]:
                    if child.level == 0:
                        break
                    if child.type == "list_item_open" and child.level == 1 and child.map:
                        a, b = child.map
                        items.append("".join(lines[a:b]).rstrip("\r\n"))
                blocks.append(_Block(raw, "list", items=tuple(items)))
            else:
                kind = "paragraph" if token.type == "paragraph_open" else "other"
                blocks.append(_Block(raw, kind))
        tail = "".join(lines[cursor:]).strip()
        if tail:
            blocks.append(_Block(tail, "other"))
        if blocks:
            sections.append((section, blocks))
        return sections

    def _split_block(self, block: _Block, fits: Callable[..., bool]) -> list[_Block]:
        if block.kind == "code":
            def wrap(code: str) -> str:
                return f"{block.fence}{block.info}\n{code}" + ("" if code.endswith("\n") else "\n") + block.fence

            return [_Block(part, "code") for part in self._split_text(block.content, lambda s: fits(wrap(s)), wrap, lines_only=True)]
        if fits(block.text):
            return [block]
        if block.kind == "table":
            lines = block.text.splitlines()
            header = "\n".join(lines[:2])
            result = []
            current = header
            for row in lines[2:]:
                if not fits(header + "\n" + row):
                    raise ValueError("Строка таблицы вместе с шапкой превышает бюджет; упростите таблицу или увеличьте CHUNK_MAX_TOKENS")
                if not fits(current + "\n" + row):
                    result.append(_Block(current, "table"))
                    current = header
                current += "\n" + row
            if current != header:
                result.append(_Block(current, "table"))
            elif not result:
                raise ValueError("Шапка таблицы превышает токенный бюджет")
            return result
        if block.kind == "list" and block.items:
            result = []
            for item in block.items:
                if fits(item):
                    result.append(_Block(item, "list"))
                else:
                    result.extend(_Block(part, "list") for part in self._split_text(item, fits, lambda s: s))
            return result

        # Для длинного текста целимся в target, оставляя место под перекрытие.
        limit = self.target_tokens if fits("x", self.target_tokens) else self.max_tokens
        return [
            _Block(part, block.kind)
            for part in self._split_text(block.text, lambda s: fits(s, limit), lambda s: s)
        ]

    def _split_text(
        self,
        text: str,
        fits: Callable[[str], bool],
        wrap: Callable[[str], str],
        *,
        lines_only: bool = False,
    ) -> list[str]:
        result = []
        while text:
            if not lines_only and not text.strip():
                break
            if fits(text):
                result.append(wrap(text))
                break
            # Режем исходные символы, а не декодируем куски токенов:
            # так не ломаются Unicode-символы на границе byte-level токенов.
            low, high, end = 1, len(text), 0
            while low <= high:
                middle = (low + high) // 2
                if fits(text[:middle]):
                    end, low = middle, middle + 1
                else:
                    high = middle - 1
            if end == 0:
                raise ValueError("Недостаточный бюджет для содержимого блока")
            candidates = [m.end() for m in re.finditer(r"\n", text[:end])]
            if not lines_only and not candidates:
                candidates = [m.end() for m in re.finditer(r"[.!?…](?:[\"»)]*)\s+", text[:end])]
            if not lines_only and not candidates:
                candidates = [m.end() for m in re.finditer(r"\s+", text[:end])]
            natural = [position for position in candidates if position >= end // 2]
            if natural and fits(text[:natural[-1]]):
                end = natural[-1]
            result.append(wrap(text[:end]))
            text = text[end:]
        # Пустой кодовый блок сохраняет свои ограждения.
        return result or [wrap("")]

    def _overlap(self, block: _Block) -> str:
        if block.kind != "paragraph" or self.overlap_tokens == 0:
            return ""
        tail = ""
        for match in reversed(list(re.finditer(r"\S+", block.text))):
            candidate = block.text[match.start():].strip()
            if self.count_tokens(candidate) > self.overlap_tokens:
                break
            tail = candidate
        return tail
