#!/usr/bin/env python3
"""Seed demos/demo.db with realistic OpenAI proxy log entries for demo recordings."""

import argparse
import json
import os
import sqlite3
import time


SCHEMA = """
CREATE TABLE IF NOT EXISTS openai_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ts_start INTEGER NOT NULL,
    ts_end INTEGER,
    duration_ms INTEGER,
    client_ip TEXT,
    request_method TEXT NOT NULL,
    request_path TEXT NOT NULL,
    upstream_url TEXT NOT NULL,
    status_code INTEGER,
    req_headers_json TEXT,
    resp_headers_json TEXT,
    req_body TEXT,
    req_truncated BOOLEAN DEFAULT 0,
    req_bytes INTEGER,
    resp_body TEXT,
    resp_truncated BOOLEAN DEFAULT 0,
    resp_bytes INTEGER,
    error TEXT,
    parent_id INTEGER REFERENCES openai_logs(id),
    chat_hash TEXT,
    parent_prefix_len INTEGER,
    message_count INTEGER,
    req_text TEXT,
    resp_text TEXT
);

CREATE INDEX IF NOT EXISTS idx_ts_start ON openai_logs(ts_start);
CREATE INDEX IF NOT EXISTS idx_status_code_ts ON openai_logs(status_code, ts_start);
CREATE INDEX IF NOT EXISTS idx_path_ts ON openai_logs(request_path, ts_start);
CREATE INDEX IF NOT EXISTS idx_chat_hash ON openai_logs(chat_hash);
CREATE INDEX IF NOT EXISTS idx_parent_id ON openai_logs(parent_id);

CREATE VIRTUAL TABLE IF NOT EXISTS openai_logs_fts USING fts5(
    req_text,
    resp_text,
    content='openai_logs',
    content_rowid='id'
);

CREATE TRIGGER IF NOT EXISTS openai_logs_ai AFTER INSERT ON openai_logs BEGIN
    INSERT INTO openai_logs_fts(rowid, req_text, resp_text)
    VALUES (new.id, COALESCE(new.req_text, new.req_body), COALESCE(new.resp_text, new.resp_body));
END;

CREATE TRIGGER IF NOT EXISTS openai_logs_ad AFTER DELETE ON openai_logs BEGIN
    INSERT INTO openai_logs_fts(openai_logs_fts, rowid, req_text, resp_text)
    VALUES ('delete', old.id, COALESCE(old.req_text, old.req_body), COALESCE(old.resp_text, old.resp_body));
END;
"""


def sse_chunk(content: str, model: str = "gpt-4o", idx: str = "001") -> str:
    chunk = {
        "id": f"chatcmpl-{idx}",
        "object": "chat.completion.chunk",
        "model": model,
        "choices": [{"delta": {"role": "assistant", "content": content}, "index": 0}],
    }
    return f"data: {json.dumps(chunk)}\n\ndata: [DONE]\n\n"


def sse_reasoning_chunk(reasoning: str, content: str, idx: str = "009") -> str:
    think_chunk = {
        "id": f"chatcmpl-{idx}",
        "object": "chat.completion.chunk",
        "model": "o1",
        "choices": [{"delta": {"reasoning_content": reasoning, "content": ""}, "index": 0}],
    }
    ans_chunk = {
        "id": f"chatcmpl-{idx}",
        "object": "chat.completion.chunk",
        "model": "o1",
        "choices": [{"delta": {"reasoning_content": "", "content": content}, "index": 0}],
    }
    return (
        f"data: {json.dumps(think_chunk)}\n\n"
        f"data: {json.dumps(ans_chunk)}\n\n"
        "data: [DONE]\n\n"
    )


def req_body(model: str, user_msg: str) -> str:
    return json.dumps({
        "model": model,
        "messages": [{"role": "user", "content": user_msg}],
        "stream": True,
    })


def req_headers() -> str:
    return json.dumps({
        "Content-Type": "application/json",
        "Authorization": "Bearer sk-demo-***",
    })


def resp_headers(status: int = 200) -> str:
    return json.dumps({
        "Content-Type": "text/event-stream",
        "X-Request-Id": "req-demo-001",
    })


def error_resp_body(message: str) -> str:
    return json.dumps({"error": {"message": message, "type": "invalid_request_error"}})


BASE_TS = int(time.time() * 1000) - 3600_000  # 1 hour ago


def ts(offset_s: int) -> int:
    return BASE_TS + offset_s * 1000


ROWS = [
    # (user_prompt, model, status, duration_ms, resp_content, error_msg)
    (
        "What is the capital of France?",
        "gpt-4o",
        200,
        312,
        "The capital of France is Paris.",
        None,
    ),
    (
        "Write a Python function to reverse a string",
        "gpt-4o-mini",
        200,
        891,
        'def reverse_string(s: str) -> str:\n    return s[::-1]',
        None,
    ),
    (
        "Explain quantum entanglement in simple terms",
        "gpt-4o",
        200,
        1543,
        (
            "Quantum entanglement is a phenomenon where two particles become "
            "linked so that measuring one instantly affects the other, no matter "
            "how far apart they are. Einstein called it 'spooky action at a distance'."
        ),
        None,
    ),
    (
        "What is the latest news?",
        "gpt-4o",
        400,
        45,
        None,
        "Invalid request: model does not support this operation",
    ),
    (
        "Summarize the history of computing",
        "gpt-4o-mini",
        200,
        2341,
        (
            "The history of computing spans from mechanical calculators in the 17th "
            "century to modern quantum computers. Key milestones include Babbage's "
            "Analytical Engine (1837), ENIAC (1945), the transistor (1947), "
            "integrated circuits (1958), personal computers (1970s), the internet "
            "(1980s-90s), and smartphones (2007)."
        ),
        None,
    ),
    (
        "What is 2+2?",
        "claude-3-5-sonnet",
        200,
        234,
        "2 + 2 = 4.",
        None,
    ),
    (
        "Translate 'hello' to Spanish",
        "gpt-4o",
        200,
        198,
        "'Hello' in Spanish is 'Hola'.",
        None,
    ),
    (
        "List all prime numbers below 100",
        "gpt-4o",
        500,
        88,
        None,
        "upstream server error: connection reset by peer",
    ),
    (
        "Solve this math problem step by step: What is the 10th Fibonacci number?",
        "o1",
        200,
        3210,
        None,  # will be set to SSE reasoning body below
        None,
    ),
    (
        "List 5 sorting algorithms and their time complexities",
        "gpt-4o-mini",
        200,
        1876,
        (
            "1. Bubble Sort — O(n²)\n"
            "2. Merge Sort — O(n log n)\n"
            "3. Quick Sort — O(n log n) avg\n"
            "4. Heap Sort — O(n log n)\n"
            "5. Insertion Sort — O(n²)"
        ),
        None,
    ),
    (
        "What time is it?",
        "gpt-4o",
        200,
        156,
        "I don't have access to real-time information, so I cannot tell you the current time.",
        None,
    ),
    (
        "Generate a JSON schema for a user object",
        "gpt-4o",
        200,
        1102,
        (
            '{\n  "$schema": "http://json-schema.org/draft-07/schema#",\n'
            '  "type": "object",\n'
            '  "properties": {\n'
            '    "id": {"type": "integer"},\n'
            '    "name": {"type": "string"},\n'
            '    "email": {"type": "string", "format": "email"},\n'
            '    "created_at": {"type": "string", "format": "date-time"}\n'
            '  },\n'
            '  "required": ["id", "name", "email"]\n'
            "}"
        ),
        None,
    ),
]


def build_entries():
    entries = []
    for i, (prompt, model, status, dur_ms, resp_content, error_msg) in enumerate(ROWS):
        row_num = i + 1
        t_start = ts(i * 300)  # 5-minute spacing
        t_end = t_start + dur_ms

        rb = req_body(model, prompt)
        rh = req_headers()
        req_bytes = len(rb.encode())

        if status == 200 and resp_content is not None:
            resp_b = sse_chunk(resp_content, model=model, idx=f"{row_num:03d}")
            resp_bytes = len(resp_b.encode())
            resp_hdr = resp_headers(status)
            err = None
            resp_text = resp_content
        elif row_num == 9:
            # Reasoning row
            reasoning = (
                "Let me compute the Fibonacci sequence:\n"
                "F(1)=1, F(2)=1, F(3)=2, F(4)=3, F(5)=5,\n"
                "F(6)=8, F(7)=13, F(8)=21, F(9)=34, F(10)=55."
            )
            answer = "The 10th Fibonacci number is **55**."
            resp_b = sse_reasoning_chunk(reasoning, answer)
            resp_bytes = len(resp_b.encode())
            resp_hdr = resp_headers(status)
            err = None
            resp_text = answer
        else:
            resp_b = error_resp_body(error_msg) if error_msg else None
            resp_bytes = len(resp_b.encode()) if resp_b else 0
            resp_hdr = None
            err = error_msg
            resp_text = None

        entries.append({
            "ts_start": t_start,
            "ts_end": t_end,
            "duration_ms": dur_ms,
            "client_ip": "127.0.0.1",
            "request_method": "POST",
            "request_path": "/v1/chat/completions",
            "upstream_url": f"https://api.openai.com/v1/chat/completions",
            "status_code": status,
            "req_headers_json": rh,
            "resp_headers_json": resp_hdr,
            "req_body": rb,
            "req_truncated": False,
            "req_bytes": req_bytes,
            "resp_body": resp_b,
            "resp_truncated": False,
            "resp_bytes": resp_bytes,
            "error": err,
            "parent_id": None,
            "chat_hash": None,
            "parent_prefix_len": None,
            "message_count": 1,
            "req_text": prompt,
            "resp_text": resp_text,
        })
    return entries


def seed(db_path: str) -> None:
    os.makedirs(os.path.dirname(db_path) if os.path.dirname(db_path) else ".", exist_ok=True)

    if os.path.exists(db_path):
        os.remove(db_path)

    conn = sqlite3.connect(db_path)
    conn.execute("PRAGMA journal_mode=WAL")
    conn.execute("PRAGMA synchronous=NORMAL")
    conn.executescript(SCHEMA)

    entries = build_entries()
    for e in entries:
        conn.execute(
            """INSERT INTO openai_logs (
                ts_start, ts_end, duration_ms, client_ip,
                request_method, request_path, upstream_url, status_code,
                req_headers_json, resp_headers_json,
                req_body, req_truncated, req_bytes,
                resp_body, resp_truncated, resp_bytes,
                error,
                parent_id, chat_hash, parent_prefix_len, message_count,
                req_text, resp_text
            ) VALUES (
                :ts_start, :ts_end, :duration_ms, :client_ip,
                :request_method, :request_path, :upstream_url, :status_code,
                :req_headers_json, :resp_headers_json,
                :req_body, :req_truncated, :req_bytes,
                :resp_body, :resp_truncated, :resp_bytes,
                :error,
                :parent_id, :chat_hash, :parent_prefix_len, :message_count,
                :req_text, :resp_text
            )""",
            e,
        )

    conn.commit()
    count = conn.execute("SELECT COUNT(*) FROM openai_logs").fetchone()[0]
    fts_count = conn.execute("SELECT COUNT(*) FROM openai_logs_fts").fetchone()[0]
    conn.close()
    print(f"✓ Seeded {count} rows into {db_path} (FTS: {fts_count} rows)")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Seed demo database")
    parser.add_argument("--out", default="demos/demo.db", help="Output DB path")
    args = parser.parse_args()
    seed(args.out)
