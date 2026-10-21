package knowledge

import (
	_ "embed"
	"fmt"
)

// baseJSON is the table shipped with the binary.
//
// It is **the same file the refresh URL points at** — a contributor edits the shape the app fetches,
// rather than a copy of it — and the format is documented in `docs/KNOWLEDGE.md` so the two cannot
// drift apart in a reader's head.
//
//go:embed base.json
var baseJSON []byte

// Embedded returns the bundled table.
//
// It is parsed on every call rather than cached in a package variable, because this runs a handful of
// times per request at most and a `sync.Once` around a value that is already in memory buys nothing
// but a mutex. A parse failure is impossible in a shipped binary — there is a test that reads this
// exact file — and is returned rather than panicked so a build that somehow lost it degrades to "no
// knowledge" instead of a crash loop.
func Embedded() (Base, error) {
	b, err := Parse(baseJSON)
	if err != nil {
		return Base{}, fmt.Errorf("the bundled knowledge base is unreadable: %w", err)
	}
	return b, nil
}

// EmbeddedBytes returns the bundled table as it is written, for the "download the base" affordance and
// for a test that can then check it round-trips.
func EmbeddedBytes() []byte {
	out := make([]byte, len(baseJSON))
	copy(out, baseJSON)
	return out
}
