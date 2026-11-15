from dataclasses import dataclass, field
from typing import Literal, Union

RoleType = Literal["intern", "buddy", "manager"]
OriginType = Literal["public", "synthetic", "provided"]
OutcomeType = Literal["answered", "no_answer", "degraded"]

ROLE_RANKS: dict[str, int] = {
    "intern": 1,
    "buddy": 2,
    "manager": 3,
}


def get_role_rank(role: str) -> int:
    """Возвращает числовой уровень доступа роли (по умолчанию 1 - стажёр)."""
    return ROLE_RANKS.get(role.lower().strip(), 1)


@dataclass(frozen=True, slots=True)
class Message:
    role: Literal["user", "assistant"]
    content: str


@dataclass(slots=True)
class Chunk:
    id: int
    doc_id: int
    title: str
    section: str
    text: str
    source_url: str
    origin: OriginType
    score: float
    cosine_score: float = 0.0  # Сохраняем косинус отдельно для порога "в базе нет" (п. 4.8)


@dataclass(slots=True)
class RagRequest:
    question: str
    role: str = "intern"
    team: str | None = None
    history: list[Message] = field(default_factory=list)


# --- События потока (status -> token -> sources -> done / error) ---

@dataclass(frozen=True, slots=True)
class StatusEvent:
    stage: Literal["searching", "generating"]
    type: Literal["status"] = "status"


@dataclass(frozen=True, slots=True)
class TokenEvent:
    text: str
    type: Literal["token"] = "token"


@dataclass(frozen=True, slots=True)
class SourceItem:
    index: int          # Номер источника [1], [2] в ответе
    doc_id: int
    title: str
    section: str
    source_url: str
    origin: OriginType


@dataclass(frozen=True, slots=True)
class SourcesEvent:
    sources: list[SourceItem]
    type: Literal["sources"] = "sources"


@dataclass(frozen=True, slots=True)
class DoneEvent:
    outcome: OutcomeType
    type: Literal["done"] = "done"


@dataclass(frozen=True, slots=True)
class ErrorEvent:
    message: str
    type: Literal["error"] = "error"


Event = Union[StatusEvent, TokenEvent, SourcesEvent, DoneEvent, ErrorEvent]

# Чанки ДО записи в БД. Chunk выше остаётся результатом поиска со score и ID.
@dataclass(frozen=True, slots=True)
class TextChunk:
    section: str
    text: str

    def embedding_text(self, title: str) -> str:
        """Единый формат для подсчёта токенов и построения эмбеддинга."""
        if self.section.strip():
            return f"{title} | {self.section}\n{self.text}"
        return f"{title}\n{self.text}"


@dataclass(frozen=True, slots=True)
class EmbeddedChunk:
    section: str
    text: str
    embedding: list[float]


@dataclass(frozen=True, slots=True)
class Document:
    title: str
    text: str
    source_url: str
    origin: OriginType
    min_role: RoleType
    team: str | None = None
    tags: tuple[str, ...] = ()
    source_path: str = ""
