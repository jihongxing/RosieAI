from __future__ import annotations

from contextlib import contextmanager
from pathlib import Path
import re
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

CREATE TABLE IF NOT EXISTS call_transcripts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    call_sid TEXT NOT NULL UNIQUE,
    merchant_id TEXT,
    transcript TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'manual',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_call_transcripts_call_sid ON call_transcripts(call_sid);

CREATE TABLE IF NOT EXISTS call_summaries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    call_sid TEXT NOT NULL UNIQUE,
    merchant_id TEXT,
    summary TEXT,
    customer_name TEXT,
    customer_phone TEXT,
    intent TEXT,
    appointment_time TEXT,
    service TEXT,
    priority TEXT NOT NULL DEFAULT 'normal',
    need_human_followup INTEGER NOT NULL DEFAULT 0,
    raw_result TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_call_summaries_call_sid ON call_summaries(call_sid);
CREATE INDEX IF NOT EXISTS idx_call_summaries_merchant ON call_summaries(merchant_id);

CREATE TABLE IF NOT EXISTS inbox_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    merchant_id TEXT NOT NULL,
    call_sid TEXT NOT NULL UNIQUE,
    item_type TEXT NOT NULL DEFAULT 'call_summary',
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    priority TEXT NOT NULL DEFAULT 'normal',
    status TEXT NOT NULL DEFAULT 'new',
    need_human_followup INTEGER NOT NULL DEFAULT 0,
    digest_status TEXT NOT NULL DEFAULT 'pending',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_inbox_items_merchant ON inbox_items(merchant_id, id);
CREATE INDEX IF NOT EXISTS idx_inbox_items_digest ON inbox_items(merchant_id, digest_status);

CREATE TABLE IF NOT EXISTS digests (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    merchant_id TEXT NOT NULL,
    digest_type TEXT NOT NULL DEFAULT 'daily',
    item_count INTEGER NOT NULL DEFAULT 0,
    urgent_count INTEGER NOT NULL DEFAULT 0,
    followup_count INTEGER NOT NULL DEFAULT 0,
    spam_count INTEGER NOT NULL DEFAULT 0,
    digest_text TEXT NOT NULL,
    item_ids TEXT NOT NULL DEFAULT '[]',
    status TEXT NOT NULL DEFAULT 'generated',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_digests_merchant ON digests(merchant_id, id);
"""


def normalize_access_number(value: str) -> str:
    return re.sub(r"\D", "", value.strip())


class Database:
    def __init__(self, path: Path) -> None:
        self.path = path

    def init(self) -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        with self.connect() as conn:
            conn.executescript(SCHEMA)
            self._ensure_unique_index(conn, "idx_call_transcripts_call_sid_unique", "call_transcripts", "call_sid")
            self._ensure_unique_index(conn, "idx_call_summaries_call_sid_unique", "call_summaries", "call_sid")
            self._ensure_unique_index(conn, "idx_inbox_items_call_sid_unique", "inbox_items", "call_sid")

    @contextmanager
    def connect(self) -> Iterator[sqlite3.Connection]:
        conn = sqlite3.connect(self.path)
        conn.row_factory = sqlite3.Row
        try:
            yield conn
            conn.commit()
        finally:
            conn.close()

    def _ensure_unique_index(
        self,
        conn: sqlite3.Connection,
        index_name: str,
        table_name: str,
        column_name: str,
    ) -> None:
        rows = conn.execute(
            f"""
            SELECT {column_name}, MIN(id) AS keep_id
            FROM {table_name}
            GROUP BY {column_name}
            HAVING COUNT(*) > 1
            """
        ).fetchall()
        for row in rows:
            conn.execute(
                f"DELETE FROM {table_name} WHERE {column_name} = ? AND id <> ?",
                (row[column_name], row["keep_id"]),
            )
        conn.execute(f"CREATE UNIQUE INDEX IF NOT EXISTS {index_name} ON {table_name}({column_name})")

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
                (merchant_id, merchant_name, normalize_access_number(access_number), transfer_phone),
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
                    normalize_access_number(merchant["access_number"]),
                    merchant.get("original_number"),
                    merchant.get("transfer_phone"),
                    1 if merchant.get("enabled", True) else 0,
                ),
            )

    def find_merchant_by_access_number(self, access_number: str) -> dict[str, Any] | None:
        normalized_access_number = normalize_access_number(access_number)
        with self.connect() as conn:
            row = conn.execute(
                """
                SELECT merchant_id, merchant_name, access_number, original_number, transfer_phone
                FROM merchants
                WHERE access_number = ? AND enabled = 1
                """,
                (normalized_access_number,),
            ).fetchone()
        return dict(row) if row else None

    def find_merchant_by_id(self, merchant_id: str) -> dict[str, Any] | None:
        with self.connect() as conn:
            row = conn.execute(
                """
                SELECT merchant_id, merchant_name, access_number, original_number, transfer_phone
                FROM merchants
                WHERE merchant_id = ? AND enabled = 1
                """,
                (merchant_id,),
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
            existing = conn.execute(
                "SELECT id FROM calls WHERE call_sid = ? ORDER BY id DESC LIMIT 1",
                (call.get("call_sid"),),
            ).fetchone()
            if existing:
                conn.execute(
                    """
                    UPDATE calls
                    SET call_id = ?, merchant_id = ?, from_number = ?, to_number = ?,
                        call_status = ?, direction = ?, raw_payload = ?,
                        updated_at = CURRENT_TIMESTAMP
                    WHERE id = ?
                    """,
                    (
                        call.get("call_id"),
                        call.get("merchant_id"),
                        call.get("from_number"),
                        call.get("to_number"),
                        call.get("call_status"),
                        call.get("direction"),
                        call.get("raw_payload"),
                        existing["id"],
                    ),
                )
                return int(existing["id"])

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
            row = conn.execute(
                "SELECT id FROM calls WHERE call_sid = ? ORDER BY id DESC LIMIT 1",
                (call.get("call_sid"),),
            ).fetchone()
            return int(row["id"] if row else cursor.lastrowid)

    def find_call_by_sid(self, call_sid: str) -> dict[str, Any] | None:
        with self.connect() as conn:
            row = conn.execute(
                """
                SELECT id, call_sid, call_id, merchant_id, from_number, to_number,
                       call_status, direction, created_at
                FROM calls
                WHERE call_sid = ?
                ORDER BY id DESC
                LIMIT 1
                """,
                (call_sid,),
            ).fetchone()
        return dict(row) if row else None

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

    def insert_transcript(self, transcript: dict[str, Any]) -> int:
        with self.connect() as conn:
            cursor = conn.execute(
                """
                INSERT INTO call_transcripts (call_sid, merchant_id, transcript, source)
                VALUES (?, ?, ?, ?)
                ON CONFLICT(call_sid) DO UPDATE SET
                    merchant_id = excluded.merchant_id,
                    transcript = excluded.transcript,
                    source = excluded.source
                """,
                (
                    transcript["call_sid"],
                    transcript.get("merchant_id"),
                    transcript["transcript"],
                    transcript.get("source", "manual"),
                ),
            )
            row = conn.execute(
                "SELECT id FROM call_transcripts WHERE call_sid = ?",
                (transcript["call_sid"],),
            ).fetchone()
            return int(row["id"] if row else cursor.lastrowid)

    def insert_summary(self, summary: dict[str, Any]) -> int:
        with self.connect() as conn:
            cursor = conn.execute(
                """
                INSERT INTO call_summaries (
                    call_sid, merchant_id, summary, customer_name, customer_phone,
                    intent, appointment_time, service, priority, need_human_followup,
                    raw_result
                )
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(call_sid) DO UPDATE SET
                    merchant_id = excluded.merchant_id,
                    summary = excluded.summary,
                    customer_name = excluded.customer_name,
                    customer_phone = excluded.customer_phone,
                    intent = excluded.intent,
                    appointment_time = excluded.appointment_time,
                    service = excluded.service,
                    priority = excluded.priority,
                    need_human_followup = excluded.need_human_followup,
                    raw_result = excluded.raw_result
                """,
                (
                    summary["call_sid"],
                    summary.get("merchant_id"),
                    summary.get("summary"),
                    summary.get("customer_name"),
                    summary.get("customer_phone"),
                    summary.get("intent"),
                    summary.get("appointment_time"),
                    summary.get("service"),
                    summary.get("priority", "normal"),
                    1 if summary.get("need_human_followup") else 0,
                    summary.get("raw_result", "{}"),
                ),
            )
            row = conn.execute(
                "SELECT id FROM call_summaries WHERE call_sid = ?",
                (summary["call_sid"],),
            ).fetchone()
            return int(row["id"] if row else cursor.lastrowid)

    def insert_inbox_item(self, item: dict[str, Any]) -> int:
        with self.connect() as conn:
            cursor = conn.execute(
                """
                INSERT INTO inbox_items (
                    merchant_id, call_sid, item_type, title, body, priority,
                    status, need_human_followup, digest_status
                )
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
                ON CONFLICT(call_sid) DO UPDATE SET
                    merchant_id = excluded.merchant_id,
                    item_type = excluded.item_type,
                    title = excluded.title,
                    body = excluded.body,
                    priority = excluded.priority,
                    status = excluded.status,
                    need_human_followup = excluded.need_human_followup,
                    digest_status = 'pending',
                    updated_at = CURRENT_TIMESTAMP
                """,
                (
                    item["merchant_id"],
                    item["call_sid"],
                    item.get("item_type", "call_summary"),
                    item["title"],
                    item["body"],
                    item.get("priority", "normal"),
                    item.get("status", "new"),
                    1 if item.get("need_human_followup") else 0,
                    item.get("digest_status", "pending"),
                ),
            )
            row = conn.execute(
                "SELECT id FROM inbox_items WHERE call_sid = ?",
                (item["call_sid"],),
            ).fetchone()
            return int(row["id"] if row else cursor.lastrowid)

    def list_inbox_items(self, merchant_id: str, limit: int = 50) -> list[dict[str, Any]]:
        with self.connect() as conn:
            rows = conn.execute(
                """
                SELECT id, merchant_id, call_sid, item_type, title, body, priority,
                       status, need_human_followup, digest_status, created_at
                FROM inbox_items
                WHERE merchant_id = ?
                ORDER BY id DESC
                LIMIT ?
                """,
                (merchant_id, limit),
            ).fetchall()
        return [dict(row) for row in rows]

    def list_pending_digest_items(self, merchant_id: str, limit: int = 100) -> list[dict[str, Any]]:
        with self.connect() as conn:
            rows = conn.execute(
                """
                SELECT id, merchant_id, call_sid, item_type, title, body, priority,
                       status, need_human_followup, digest_status, created_at
                FROM inbox_items
                WHERE merchant_id = ? AND digest_status = 'pending'
                ORDER BY id ASC
                LIMIT ?
                """,
                (merchant_id, limit),
            ).fetchall()
        return [dict(row) for row in rows]

    def insert_digest(self, digest: dict[str, Any]) -> int:
        with self.connect() as conn:
            cursor = conn.execute(
                """
                INSERT INTO digests (
                    merchant_id, digest_type, item_count, urgent_count,
                    followup_count, spam_count, digest_text, item_ids, status
                )
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    digest["merchant_id"],
                    digest.get("digest_type", "daily"),
                    digest.get("item_count", 0),
                    digest.get("urgent_count", 0),
                    digest.get("followup_count", 0),
                    digest.get("spam_count", 0),
                    digest["digest_text"],
                    digest.get("item_ids", "[]"),
                    digest.get("status", "generated"),
                ),
            )
            return int(cursor.lastrowid)

    def mark_inbox_items_digested(self, item_ids: list[int]) -> None:
        if not item_ids:
            return
        placeholders = ",".join("?" for _ in item_ids)
        with self.connect() as conn:
            conn.execute(
                f"""
                UPDATE inbox_items
                SET digest_status = 'digested',
                    updated_at = CURRENT_TIMESTAMP
                WHERE id IN ({placeholders})
                """,
                item_ids,
            )

    def list_digests(self, merchant_id: str, limit: int = 20) -> list[dict[str, Any]]:
        with self.connect() as conn:
            rows = conn.execute(
                """
                SELECT id, merchant_id, digest_type, item_count, urgent_count,
                       followup_count, spam_count, digest_text, item_ids,
                       status, created_at
                FROM digests
                WHERE merchant_id = ?
                ORDER BY id DESC
                LIMIT ?
                """,
                (merchant_id, limit),
            ).fetchall()
        return [dict(row) for row in rows]

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
