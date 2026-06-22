"""Database engine, session factory and initialisation helpers."""

from __future__ import annotations

import logging
from contextlib import contextmanager
from typing import Iterator

from sqlalchemy import create_engine, event
from sqlalchemy.orm import Session, declarative_base, sessionmaker

from .config import config

log = logging.getLogger(__name__)

Base = declarative_base()

# SQLite + multithreaded scheduler/web access: allow cross-thread usage and rely
# on WAL mode + a short busy timeout for concurrency.
engine = create_engine(
    config.db_url,
    connect_args={"check_same_thread": False, "timeout": 30},
    future=True,
)


@event.listens_for(engine, "connect")
def _set_sqlite_pragmas(dbapi_conn, _record):  # pragma: no cover - simple setup
    cur = dbapi_conn.cursor()
    cur.execute("PRAGMA journal_mode=WAL")
    cur.execute("PRAGMA synchronous=NORMAL")
    cur.execute("PRAGMA foreign_keys=ON")
    cur.execute("PRAGMA busy_timeout=30000")
    cur.close()


SessionLocal = sessionmaker(bind=engine, autoflush=False, expire_on_commit=False, future=True)


def init_db() -> None:
    """Create the database file/directory and all tables."""
    config.db_path.parent.mkdir(parents=True, exist_ok=True)
    # Import models so they register on the metadata before create_all.
    from . import models  # noqa: F401

    Base.metadata.create_all(bind=engine)
    log.info("Database ready at %s", config.db_path)


def get_db() -> Iterator[Session]:
    """FastAPI dependency yielding a session."""
    db = SessionLocal()
    try:
        yield db
    finally:
        db.close()


@contextmanager
def session_scope() -> Iterator[Session]:
    """Context manager for use inside background jobs."""
    db = SessionLocal()
    try:
        yield db
        db.commit()
    except Exception:
        db.rollback()
        raise
    finally:
        db.close()
