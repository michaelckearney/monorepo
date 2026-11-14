-- Chats with Harbor's agent. Times have milliseconds, so the newest chat
-- sorts first even when two change in the same second.

CREATE TABLE chats (
    id         TEXT PRIMARY KEY,
    title      TEXT NOT NULL DEFAULT '',     -- the start of the first message
    working    INTEGER NOT NULL DEFAULT 0,   -- 1 while the agent is on a turn
    draft      TEXT NOT NULL DEFAULT '',     -- the reply so far, as it streams in
    error      TEXT NOT NULL DEFAULT '',     -- why the last turn stopped, if it failed
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now'))
);

-- A chat's messages, in order. A message does not change once it is stored.
-- parts is a JSON array; each part is one of:
--   {"text": "..."}
--   {"toolCall": {"id", "name", "input"}, "ask": true}  -- ask: it needs your permission
--   {"toolResult": {"id", "output"} or {"id", "error"}, "refused": true}
-- A user's message has text, an assistant's has text and tool calls, and a
-- tool message has one tool result. usage is what the provider said the
-- assistant's message used.
CREATE TABLE messages (
    seq        INTEGER PRIMARY KEY,
    chat_id    TEXT NOT NULL REFERENCES chats (id) ON DELETE CASCADE,
    role       TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'tool')),
    parts      TEXT NOT NULL CHECK (json_valid(parts)),
    usage      TEXT CHECK (usage IS NULL OR json_valid(usage)),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now'))
);
CREATE INDEX messages_by_chat ON messages (chat_id, seq);
