package webui

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

	for _, name := range ThemeNames {
		switch name {
		// The built-ins share selectors: light is grouped with :root, and custom is
		// qualified by its base.
		case "light", "dark":
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
		}
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
