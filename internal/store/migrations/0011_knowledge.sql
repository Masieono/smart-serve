-- 0011_knowledge.sql — the two stored layers of the knowledge base (`FEATURES.md` A21).
--
-- The base has three layers: the table bundled with the binary, a copy fetched from a URL, and the
-- user's own overrides. Only the last two need storing, and they are the *same kind of thing* — a
-- knowledge document — so they are two rows in one table rather than two tables. `kind` is the
-- primary key, which is what keeps it at two rows: this is a small, hand-edited document, not a
-- collection to query.
--
-- The payload is the JSON exactly as it arrived. Stored verbatim rather than shredded into columns
-- because the file *is* the format: the overrides editor hands the user a copy of it to edit, the
-- refresh stores what it fetched, and a column-per-field schema would be a third representation to
-- keep in step with the other two.
--
-- `source_url` and `fetched_at` describe where the cache came from, so the app can say how old it is
-- and whether the URL has since changed. They are empty for overrides, which have no source.

CREATE TABLE knowledge_docs (
    kind       TEXT PRIMARY KEY,
    payload    TEXT NOT NULL,
    source_url TEXT NOT NULL DEFAULT '',
    fetched_at INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL
);
