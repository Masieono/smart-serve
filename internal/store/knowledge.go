package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Kinds of stored knowledge document (`FEATURES.md` A21). Two rows, one table.
const (
	// KnowledgeCache is a copy fetched from a URL, the layer between the bundled table and the user's
	// own edits.
	KnowledgeCache = "cache"
	// KnowledgeOverrides is what the user typed, which wins over everything else.
	KnowledgeOverrides = "overrides"
)

// KnowledgeDoc is one stored knowledge document.
type KnowledgeDoc struct {
	// Payload is the document exactly as it arrived, verbatim.
	Payload string
	// SourceURL and FetchedAt describe a fetched copy. Empty for overrides, which have no source.
	SourceURL string
	FetchedAt time.Time
}

// GetKnowledgeDoc returns a stored document, or nil when there is none.
func (s *Store) GetKnowledgeDoc(ctx context.Context, kind string) (*KnowledgeDoc, error) {
	var (
		doc       KnowledgeDoc
		fetchedAt int64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT payload, source_url, fetched_at FROM knowledge_docs WHERE kind = ?`, kind).
		Scan(&doc.Payload, &doc.SourceURL, &fetchedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// A zero `fetched_at` means "this layer was not fetched", which is what an override is. Setting
	// the time anyway would have every override claim it was retrieved in 1970.
	if fetchedAt != 0 {
		doc.FetchedAt = time.UnixMilli(fetchedAt)
	}
	return &doc, nil
}

// PutKnowledgeDoc stores a document, replacing whatever was there.
//
// The payload is written verbatim, and the caller is expected to have *parsed* it first: storing
// something the app cannot read would replace a working layer with a broken one, and the failure
// would only show up on the next page load.
func (s *Store) PutKnowledgeDoc(ctx context.Context, kind, payload, sourceURL string) error {
	now := time.Now().UnixMilli()
	fetchedAt := int64(0)
	if kind == KnowledgeCache {
		fetchedAt = now
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO knowledge_docs (kind, payload, source_url, fetched_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(kind) DO UPDATE SET
			payload = excluded.payload,
			source_url = excluded.source_url,
			fetched_at = excluded.fetched_at,
			updated_at = excluded.updated_at`,
		kind, payload, sourceURL, fetchedAt, now)
	if err != nil {
		return fmt.Errorf("store knowledge document %q: %w", kind, err)
	}
	return nil
}

// ClearKnowledgeDoc removes a stored document, which is how the fetched layer is discarded when the
// URL changes or the fetch is reverted.
func (s *Store) ClearKnowledgeDoc(ctx context.Context, kind string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM knowledge_docs WHERE kind = ?`, kind)
	return err
}
