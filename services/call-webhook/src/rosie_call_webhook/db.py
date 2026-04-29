from __future__ import annotations

from contextlib import contextmanager
from pathlib import Path
import sqlite3
from typing import Any, Iterator


SCHEMA = """
CREATE TABLE IF NOT EXISTS merchants (
    merchant_id TEXT PRIMARY KEY,
    merchant_name TEXT NOT NULL,
    access_number TEXT NOT NULL UNIQUE,
    original_number TEXT,
    transfer_phone TEXT,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS calls (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    call_sid TEXT,
    call_id TEXT,
    merchant_id TEXT,
    from_number TEXT,
    to_number TEXT,
    call_status TEXT,
    direction TEXT,
    raw_payload TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_calls_call_sid ON calls(call_sid);
CREATE INDEX IF NOT EXISTS idx_calls_to_number ON calls(to_number);

CREATE TABLE IF NOT EXISTS call_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    call_sid TEXT,
    event_type TEXT NOT NULL,
    call_status TEXT,
    raw_payload TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
"""


class Database:
    def __init__(self, path: Path) -> None:
        self.path = path

    def init(self) -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        with self.connect() as conn:
            conn.executescript(SCHEMA)

    @contextmanager
    def connect(self) -> Iterator[sqlite3.Connection]:
        conn = sqlite3.connect(self.path)
        conn.row_factory = sqlite3.Row
        try:
            yield conn
            conn.commit()
        finally:
            conn.close()

    def upsert_default_merchant(
        self,
        merchant_id: str,
        merchant_name: str,
        access_number: str,
        transfer_phone: str | None,
    ) -> None:
        with self.connect() as conn:
            conn.execute(
                """
                INSERT INTO merchants (merchant_id, merchant_name, access_number, transfer_phone)
                VALUES (?, ?, ?, ?)
                ON CONFLICT(merchant_id) DO UPDATE SET
                    merchant_name = excluded.merchant_name,
                    access_number = excluded.access_number,
                    transfer_phone = excluded.transfer_phone,
                    enabled = 1
                """,
                (merchant_id, merchant_name, access_number, transfer_phone),
            )

    def upsert_merchant(self, merchant: dict[str, Any]) -> None:
        with self.connect() as conn:
            conn.execute(
                """
                INSERT INTO merchants (
                    merchant_id, merchant_name, access_number, original_number,
                    transfer_phone, enabled
                )
                VALUES (?, ?, ?, ?, ?, ?)
                ON CONFLICT(merchant_id) DO UPDATE SET
                    merchant_name = excluded.merchant_name,
                    access_number = excluded.access_number,
                    original_number = excluded.original_number,
                    transfer_phone = excluded.transfer_phone,
                    enabled = excluded.enabled
                """,
                (
                    merchant["merchant_id"],
                    merchant["merchant_name"],
                    merchant["access_number"],
                    merchant.get("original_number"),
                    merchant.get("transfer_phone"),
                    1 if merchant.get("enabled", True) else 0,
                ),
            )

    def find_merchant_by_access_number(self, access_number: str) -> dict[str, Any] | None:
        with self.connect() as conn:
            row = conn.execute(
                """
                SELECT merchant_id, merchant_name, access_number, original_number, transfer_phone
                FROM merchants
                WHERE access_number = ? AND enabled = 1
                """,
                (access_number,),
            ).fetchone()
        return dict(row) if row else None

    def list_merchants(self) -> list[dict[str, Any]]:
        with self.connect() as conn:
            rows = conn.execute(
                """
                SELECT merchant_id, merchant_name, access_number, original_number,
                       transfer_phone, enabled, created_at
                FROM merchants
                ORDER BY created_at DESC
                """
            ).fetchall()
        return [dict(row) for row in rows]

    def insert_call(self, call: dict[str, Any]) -> int:
        with self.connect() as conn:
            cursor = conn.execute(
                """
                INSERT INTO calls (
                    call_sid, call_id, merchant_id, from_number, to_number,
                    call_status, direction, raw_payload
                )
                VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    call.get("call_sid"),
                    call.get("call_id"),
                    call.get("merchant_id"),
                    call.get("from_number"),
                    call.get("to_number"),
                    call.get("call_status"),
                    call.get("direction"),
                    call.get("raw_payload"),
                ),
            )
            return int(cursor.lastrowid)

    def insert_event(self, event: dict[str, Any]) -> int:
        with self.connect() as conn:
            cursor = conn.execute(
                """
                INSERT INTO call_events (call_sid, event_type, call_status, raw_payload)
                VALUES (?, ?, ?, ?)
                """,
                (
                    event.get("call_sid"),
                    event.get("event_type"),
                    event.get("call_status"),
                    event.get("raw_payload"),
                ),
            )
            return int(cursor.lastrowid)

    def list_calls(self, limit: int = 50) -> list[dict[str, Any]]:
        with self.connect() as conn:
            rows = conn.execute(
                """
                SELECT id, call_sid, call_id, merchant_id, from_number, to_number,
                       call_status, direction, created_at
                FROM calls
                ORDER BY id DESC
                LIMIT ?
                """,
                (limit,),
            ).fetchall()
        return [dict(row) for row in rows]
