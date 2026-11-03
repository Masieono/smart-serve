package webui

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func themeFunc(theme Theme) ThemeFunc {
	return func(context.Context) Theme { return theme }
}

func TestIndexRendersACustomTheme(t *testing.T) {
	h, err := New(themeFunc(Theme{Name: "custom", Base: "light", Accent: "#ff8800", Tint: "warm"}),
		Options{Version: "1.2.3"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	body := rec.Body.String()
	for _, want := range []string{
		`data-theme="custom"`,
		`data-theme-base="light"`,
		`data-theme-tint="warm"`,
		"--accent:#ff8800",
		`data-version="1.2.3"`,
		"styles/tokens.css",
		"js/app.js",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index missing %q", want)
		}
	}
}

// A named palette owns its own accent. An inline override would win over the
// stylesheet and flatten every theme to the same blue, which is the whole point of
// shipping the palettes.
func TestNamedThemesDoNotTakeTheCustomAccent(t *testing.T) {
	for _, name := range ThemeNames {
		if name == "custom" {
			continue
		}
		h, err := New(themeFunc(Theme{Name: name, Accent: "#ff8800"}), Options{})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		body := rec.Body.String()
		if strings.Contains(body, "--accent:") {
			t.Errorf("theme %q leaked an inline accent override into the shell", name)
		}
		if !strings.Contains(body, `data-theme="`+name+`"`) {
			t.Errorf("theme %q was not rendered into the shell", name)
		}
	}
}

func TestThemeInvalidValuesFallBack(t *testing.T) {
	h, _ := New(themeFunc(Theme{Name: "hotdog", Base: "purple", Accent: "not-a-colour", Tint: "zebra"}), Options{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	for _, want := range []string{`data-theme="auto"`, `data-theme-base="dark"`, `data-theme-tint="neutral"`} {
		if !strings.Contains(body, want) {
			t.Errorf("fallback missing %q in %s", want, body)
		}
	}
	if strings.Contains(body, "--accent:") {
		t.Error("a fallback theme should not emit an accent override")
	}
}

// Every name the picker offers must have a palette in the stylesheet, or choosing it
// silently renders the default colours.
func TestEveryThemeNameHasAPalette(t *testing.T) {
	raw, err := fs.ReadFile(assets, "assets/styles/tokens.css")
	if err != nil {
		t.Fatalf("read tokens.css: %v", err)
	}
	sheet := string(raw)

	// The derived tokens (hover shade, soft variants) live in one :is(...) rule.
	// Extract it so membership is checked against that rule alone, not the sheet as a
	// whole — the palette block would otherwise satisfy the check by itself.
	derived := ""
	if start := strings.Index(sheet, ":is([data-theme="); start >= 0 {
		if end := strings.Index(sheet[start:], ") {"); end >= 0 {
			derived = sheet[start : start+end]
		}
	}
	if derived == "" {
		t.Fatal("could not find the derived-token :is(...) rule in tokens.css")
	}

	for _, name := range ThemeNames {
		switch name {
		// The built-ins are not palettes: `light` and `dark` carry the base tokens
		// (which `auto` resolves to via prefers-color-scheme), and `custom` derives
		// its accent from settings.
		case "auto", "light", "dark":
			if !strings.Contains(sheet, `[data-theme="`+name+`"]`) {
				t.Errorf("tokens.css has no [data-theme=%q] selector", name)
			}
		case "custom":
			if !strings.Contains(sheet, `[data-theme="custom"]`) {
				t.Error(`tokens.css has no [data-theme="custom"] selector`)
			}
		default:
			if !strings.Contains(sheet, `[data-theme="`+name+`"] {`) {
				t.Errorf("tokens.css has no palette block for %q", name)
			}
			if !strings.Contains(derived, `[data-theme="`+name+`"]`) {
				t.Errorf("palette %q is missing from the derived-token rule; "+
					"its accent would have no hover shade or soft variants", name)
			}
		}
	}
}

// A bare `svg { … }` rule overrides the width/height *attributes* of every SVG in
// the document, because CSS beats presentation attributes. That is exactly how the
// health gauge silently shrank to the icon size, so the rule stays scoped to .icon.
func TestStylesheetDoesNotStyleBareSVG(t *testing.T) {
	raw, err := fs.ReadFile(assets, "assets/styles/components.css")
	if err != nil {
		t.Fatalf("read components.css: %v", err)
	}
	for i, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "svg {" || strings.HasPrefix(trimmed, "svg,") || strings.HasPrefix(trimmed, "svg ") {
			t.Errorf("components.css:%d styles a bare svg selector (%q); "+
				"it would override the gauge and sparkline attributes", i+1, trimmed)
		}
	}
}

// Paragraph margins are reset in the stylesheet, so spacing comes from the
// container's gap. Without the reset, two otherwise identical cards space unevenly
// depending on whether they happen to contain a <p>.
func TestStylesheetResetsParagraphMargins(t *testing.T) {
	raw, err := fs.ReadFile(assets, "assets/styles/components.css")
	if err != nil {
		t.Fatalf("read components.css: %v", err)
	}
	sheet := string(raw)
	if !strings.Contains(sheet, "p {\n  margin: 0;\n}") {
		t.Error("components.css no longer resets paragraph margins; cards will space unevenly")
	}
}

// A duplicated name would silently shadow one entry in the picker.
func TestThemeNamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, name := range ThemeNames {
		if seen[name] {
			t.Errorf("duplicate theme name %q", name)
		}
		seen[name] = true
	}
}

func TestContrastIsChosenForReadability(t *testing.T) {
	if got := contrastOn("#f8e71c"); got != "#0d1117" {
		t.Errorf("contrastOn(light accent) = %q, want dark text", got)
	}
	if got := contrastOn("#1b1b1b"); got != "#ffffff" {
		t.Errorf("contrastOn(dark accent) = %q, want white text", got)
	}
}

func TestStaticAssetIsServed(t *testing.T) {
	h, _ := New(themeFunc(DefaultTheme), Options{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/styles/tokens.css", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "--accent") {
		t.Error("tokens.css did not look like the token sheet")
	}
}

// Embedded assets must carry a validator. `embed.FS` reports a zero modtime, so without one a
// browser or a reverse proxy is free to serve a module from before an upgrade — and since the
// frontend is ES modules, a mixed set does not degrade gracefully, it fails outright.
func TestStaticAssetsCarryACacheValidator(t *testing.T) {
	h, _ := New(themeFunc(DefaultTheme), Options{})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/js/views/device.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("an asset was served with no ETag; a stale copy can survive an upgrade")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache (revalidate, do not refetch blindly)", got)
	}
	if !strings.HasPrefix(etag, `"`) || !strings.HasSuffix(etag, `"`) {
		t.Errorf("ETag %q is not a quoted string", etag)
	}
}

// The revalidation path itself: a browser that holds the current copy asks, and is told to keep it.
func TestAMatchingETagGetsNotModified(t *testing.T) {
	h, _ := New(themeFunc(DefaultTheme), Options{})

	first := httptest.NewRecorder()
	h.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/js/components.js", nil))
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag to revalidate with")
	}

	req := httptest.NewRequest(http.MethodGet, "/js/components.js", nil)
	req.Header.Set("If-None-Match", etag)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("a 304 carried %d bytes of body", rec.Body.Len())
	}
}

// The dangerous direction. A browser holding a copy from *before* an upgrade must be given the new
// file, not a 304 — that is the whole bug this validator exists to prevent.
func TestAStaleETagGetsTheNewFile(t *testing.T) {
	h, _ := New(themeFunc(DefaultTheme), Options{})

	req := httptest.NewRequest(http.MethodGet, "/js/components.js", nil)
	req.Header.Set("If-None-Match", `"00000000000000000000000000000000"`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — a stale ETag must not be answered with 304", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Error("the new file was not sent")
	}
}

// `*` means "any current representation", and a weak tag compares equal to its strong form. Both are
// legal spellings a browser or proxy may send, and mishandling either way is a staleness bug.
func TestETagMatchingHandlesTheLegalSpellings(t *testing.T) {
	h, _ := New(themeFunc(DefaultTheme), Options{})
	first := httptest.NewRecorder()
	h.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/styles/components.css", nil))
	etag := first.Header().Get("ETag")

	for _, header := range []string{"*", "W/" + etag, `"other", ` + etag, `"other", W/` + etag} {
		req := httptest.NewRequest(http.MethodGet, "/styles/components.css", nil)
		req.Header.Set("If-None-Match", header)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotModified {
			t.Errorf("If-None-Match: %q gave %d, want 304", header, rec.Code)
		}
	}

	// And a list that genuinely does not contain it must not match.
	req := httptest.NewRequest(http.MethodGet, "/styles/components.css", nil)
	req.Header.Set("If-None-Match", `"aaa", "bbb"`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("an unrelated ETag list gave %d, want 200", rec.Code)
	}
}

// Two different assets must not share a validator, or changing one would be hidden by the other's
// cached copy. Checked over the real tree rather than two hand-picked files.
func TestEveryAssetHasItsOwnETag(t *testing.T) {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	etags, err := assetETags(sub)
	if err != nil {
		t.Fatalf("assetETags: %v", err)
	}
	if len(etags) < 5 {
		t.Fatalf("only %d assets hashed; the walk is not covering the tree", len(etags))
	}

	seen := map[string]string{}
	for path, tag := range etags {
		if prev, dup := seen[tag]; dup {
			t.Errorf("%s and %s share the ETag %s", prev, path, tag)
		}
		seen[tag] = path
	}
}

// The validator has to follow the *content*, or an upgrade that changes a file would leave the old
// one cached. Asserted against a synthetic tree, because the embedded one cannot be edited.
func TestETagsFollowTheContent(t *testing.T) {
	before := fstest.MapFS{"js/app.js": {Data: []byte("console.log(1)")}}
	after := fstest.MapFS{"js/app.js": {Data: []byte("console.log(2)")}}

	first, err := assetETags(before)
	if err != nil {
		t.Fatalf("assetETags: %v", err)
	}
	second, err := assetETags(after)
	if err != nil {
		t.Fatalf("assetETags: %v", err)
	}
	if first["js/app.js"] == second["js/app.js"] {
		t.Error("two different contents produced the same ETag; an upgrade would not be seen")
	}

	// And the same content is stable, or every request would refetch.
	again, err := assetETags(before)
	if err != nil {
		t.Fatalf("assetETags: %v", err)
	}
	if again["js/app.js"] != first["js/app.js"] {
		t.Error("the same content produced two different ETags")
	}
}

// The shell is rendered per request from the current theme, so it must not be cached or validated —
// a 304 there would pin the theme.
func TestTheShellIsNotCachedOrValidated(t *testing.T) {
	h, _ := New(themeFunc(DefaultTheme), Options{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/device/S1", nil))

	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("shell Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("ETag"); got != "" {
		t.Errorf("the shell carried an ETag (%q); a revalidation would pin the theme", got)
	}
}

func TestSPAFallbackServesShell(t *testing.T) {
	h, _ := New(themeFunc(DefaultTheme), Options{})
	for _, path := range []string{"/device/S1", "/schedules", "/settings", "/notifications"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s status = %d, want 200", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `id="app"`) {
			t.Errorf("%s did not return the app shell", path)
		}
	}
}

func TestBasePathIsReflectedInShell(t *testing.T) {
	h, _ := New(themeFunc(DefaultTheme), Options{BasePath: "/smartserve"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `data-base-path="/smartserve"`) {
		t.Errorf("shell missing base path: %s", body)
	}
	if !strings.Contains(body, `<base href="/smartserve/">`) {
		t.Error("shell missing base href")
	}
}
