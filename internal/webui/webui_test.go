package webui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func themeFunc(theme Theme) ThemeFunc {
	return func(context.Context) Theme { return theme }
}

func TestIndexRendersTheme(t *testing.T) {
	h, err := New(themeFunc(Theme{Name: "dark", Base: "light", Accent: "#ff8800", Tint: "warm"}),
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
		`data-theme="dark"`,
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

func TestThemeInvalidValuesFallBack(t *testing.T) {
	h, _ := New(themeFunc(Theme{Name: "hotdog", Base: "purple", Accent: "not-a-colour", Tint: "zebra"}), Options{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	for _, want := range []string{`data-theme="auto"`, `data-theme-base="dark"`, `data-theme-tint="neutral"`, "--accent:#3b82f6"} {
		if !strings.Contains(body, want) {
			t.Errorf("fallback missing %q in %s", want, body)
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
