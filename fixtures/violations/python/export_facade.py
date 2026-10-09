"""External-system adapters for retrieval capture and embedding."""

from .postgres import (
    EXECUTOR_ADVISORY_LOCK_KEY,
    PostgresRetrievalRepository,
    RetrievalRepositoryError,
)
from .vertex import (
    VertexEmbeddingAdapter,
    VertexEmbeddingConfig,
    VertexEmbeddingError,
)

__all__ = [
    "EXECUTOR_ADVISORY_LOCK_KEY",
    "PostgresRetrievalRepository",
    "RetrievalRepositoryError",
    "VertexEmbeddingAdapter",
    "VertexEmbeddingConfig",
    "VertexEmbeddingError",
]
