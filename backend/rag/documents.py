"""Чтение Markdown с явными метаданными происхождения и доступа."""

from pathlib import Path
from typing import cast

import yaml

from .models import Document, OriginType, ROLE_RANKS, RoleType


def parse_document(text: str, *, source_path: str = "") -> Document:
    lines = text.removeprefix("\ufeff").splitlines(keepends=True)
    if not lines or lines[0].strip() != "---":
        raise ValueError("В начале документа нужен YAML-блок между строками ---")
    end = next((i for i in range(1, len(lines)) if lines[i].strip() in ("---", "...")), None)
    if end is None:
        raise ValueError("Не найден конец YAML-блока документа")
    header = "".join(lines[1:end])
    try:
        # Не принимаем неоднозначные ACL/происхождение с повторяющимися ключами.
        node = yaml.compose(header, Loader=yaml.SafeLoader)
        if not isinstance(node, yaml.MappingNode):
            raise ValueError("Метаданные должны быть YAML-словарём")
        if any(not isinstance(key, yaml.ScalarNode) for key, _ in node.value):
            raise ValueError("Ключи метаданных должны быть строками")
        keys = [key.value for key, _ in node.value]
        if len(keys) != len(set(keys)) or "<<" in keys:
            raise ValueError("Повторяющиеся ключи и YAML merge в метаданных запрещены")
        metadata = yaml.safe_load(header)
    except yaml.YAMLError as exc:
        raise ValueError(f"Некорректный YAML: {exc}") from exc

    for field in ("title", "source_url", "origin", "min_role"):
        if not isinstance(metadata.get(field), str) or not metadata[field].strip():
            raise ValueError(f"Нужно непустое строковое поле {field}")
    if metadata["origin"] not in ("public", "synthetic", "provided"):
        raise ValueError("origin должен быть public, synthetic или provided")
    if metadata["min_role"] not in ROLE_RANKS:
        raise ValueError("Неизвестный min_role")
    team = metadata.get("team")
    if team is not None and (not isinstance(team, str) or not team.strip()):
        raise ValueError("team должен быть непустой строкой или null")
    tags = metadata.get("tags", [])
    if not isinstance(tags, list) or any(not isinstance(tag, str) or not tag.strip() for tag in tags):
        raise ValueError("tags должен быть списком непустых строк")

    return Document(
        title=metadata["title"].strip(),
        text="".join(lines[end + 1:]).strip("\r\n"),
        source_url=metadata["source_url"].strip(),
        origin=cast(OriginType, metadata["origin"]),
        min_role=cast(RoleType, metadata["min_role"]),
        team=team.strip() if team is not None else None,
        tags=tuple(tag.strip() for tag in tags),
        source_path=source_path,
    )


def load_document(path: str | Path) -> Document:
    path = Path(path)
    if path.suffix.lower() not in (".md", ".markdown"):
        raise ValueError("Пока поддерживаются только .md и .markdown")
    try:
        return parse_document(path.read_text(encoding="utf-8-sig"), source_path=str(path))
    except ValueError as exc:
        raise ValueError(f"{path}: {exc}") from exc
