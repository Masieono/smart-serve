// Package repo holds checks on the repository's own documents.
//
// They live here rather than in someone's shell history because two of them have been broken twice
// by the same accident: a find-and-replace applied to every match instead of the first, which put a
// release's entry under every release that had a `### Security` heading. The changelog is user-facing
// documentation, so the shape of it is testable.
package repo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// changelogPath finds CHANGELOG.md from this file's directory, so the test does not depend on the
// working directory the tool ran it from.
func changelogPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this file")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "CHANGELOG.md")
}

func TestChangelogIsWellFormed(t *testing.T) {
	data, err := os.ReadFile(changelogPath(t))
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}
	if err := checkChangelog(string(data)); err != nil {
		t.Fatalf("CHANGELOG.md: %v", err)
	}
}

// TestTheChangelogCheckCatchesWhatItIsFor keeps the test above honest: a check that has never failed
// is indistinguishable from one that cannot fail.
func TestTheChangelogCheckCatchesWhatItIsFor(t *testing.T) {
	duplicatedHeading := `## [Unreleased]

### Security

- **One entry.**

### Added

- **Something.**

### Security

- **Another entry.**
`
	if err := checkChangelog(duplicatedHeading); err == nil {
		t.Error("two ### Security headings in one release passed the check")
	}

	duplicatedEntry := `## [Unreleased]

### Security

- **The same entry, copied.**

### Added

- **The same entry, copied.**
`
	if err := checkChangelog(duplicatedEntry); err == nil {
		t.Error("the same bullet twice in one release passed the check")
	}

	staleCopy := `## [Unreleased]

### Fixed

- **A fix that belongs to the unreleased work.**

## [1.0.0] — 2026-10-17

### Fixed

- **A fix that belongs to the unreleased work.**
`
	if err := checkChangelog(staleCopy); err == nil {
		t.Error("an entry copied into a released section passed the check")
	}

	fine := `## [Unreleased]

### Added

- **Something new.**

## [1.0.0] — 2026-10-17

### Security

- **Something old.**
`
	if err := checkChangelog(fine); err != nil {
		t.Errorf("a well-formed changelog was rejected: %v", err)
	}
}

// checkChangelog reports a repeated section heading or a repeated entry within one release.
//
// Across releases the same words may legitimately appear; inside one release they mean an edit landed
// in more places than the author meant.
func checkChangelog(text string) error {
	release := "(preamble)"
	headings := map[string]bool{}
	entries := map[string]bool{}
	// An entry may appear once in the whole file: the accident this catches is a replacement that
	// lands in every release heading rather than the one it was aimed at, which leaves each release
	// with a single copy and so is invisible to the per-release check above.
	where := map[string]string{}
	reset := func() {
		headings = map[string]bool{}
		entries = map[string]bool{}
	}

	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "## "):
			release = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			reset()
		case strings.HasPrefix(line, "### "):
			name := strings.TrimSpace(strings.TrimPrefix(line, "### "))
			if headings[name] {
				return &changelogError{"release " + release + " has more than one \"### " + name + "\" section"}
			}
			headings[name] = true
		case strings.HasPrefix(line, "- **"):
			key := line
			if len(key) > 60 {
				key = key[:60]
			}
			if entries[key] {
				return &changelogError{"release " + release + " lists the same entry twice: " + key}
			}
			entries[key] = true
			if other, ok := where[key]; ok {
				return &changelogError{"the same entry appears in " + other + " and " + release + ": " + key}
			}
			where[key] = release
		}
	}
	return nil
}

type changelogError struct{ msg string }

func (e *changelogError) Error() string { return e.msg }
