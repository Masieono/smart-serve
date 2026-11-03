// Package webui serves the embedded web interface (docs/UI.md).
//
// The theme is rendered server-side into <html>, so the correct theme is applied
// before first paint (no flash of the wrong theme).
package webui

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"math"
	"net/http"
	"strconv"
	"strings"
)

//go:embed all:assets
var assets embed.FS

// Theme is the persisted appearance configuration.
type Theme struct {
	Name   string // one of ThemeNames
	Base   string // light | dark (used by the custom theme)
	Accent string // #rrggbb (used by the custom theme)
	Tint   string // neutral | slate | warm | cool
}

// DefaultTheme is used when settings are unavailable.
var DefaultTheme = Theme{Name: "auto", Base: "dark", Accent: "#3b82f6", Tint: "neutral"}

// ThemeFunc loads the current theme for a request.
type ThemeFunc func(ctx context.Context) Theme

// Options configures the handler.
type Options struct {
	BasePath string
	Version  string
}

// New returns a handler serving the UI, with SPA fallback so deep links such as
// /device/<serial> survive a refresh.
func New(themeFn ThemeFunc, opts Options) (http.Handler, error) {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		return nil, err
	}
	tmpl, err := template.ParseFS(sub, "index.html")
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServer(http.FS(sub))

	// ETags for the embedded assets, computed once. This is not an optimisation — it is the
	// correctness fix for a real upgrade hazard.
	//
	// `embed.FS` reports a zero modtime, so `http.ServeContent` emits no `Last-Modified` and no
	// `ETag`. With no validator at all, a browser — or, worse, a reverse proxy — is free to serve a
	// cached copy of `js/views/device.js` from before the upgrade. The shell is `no-store`, so the
	// HTML is always fresh, and the result is a *mix*: new HTML, old modules. Because the frontend is
	// ES modules, that mix does not degrade gracefully — an old `components.js` missing a symbol a
	// new `device.js` imports is a hard failure, and "the new card is not there" is the mild version
	// of the symptom.
	//
	// The assets are immutable for a given binary, so a content hash is exactly the right validator:
	// it changes when the assets change, and only then. Paired with `no-cache` the browser keeps its
	// copy and revalidates it, which costs one 304 per module on a LAN and removes the whole class of
	// problem. Hashing at startup rather than per request keeps the request path free of work.
	etags, err := assetETags(sub)
	if err != nil {
		return nil, err
	}

	basePath := strings.TrimRight(opts.BasePath, "/")
	baseHref := basePath + "/"
	if baseHref == "" {
		baseHref = "/"
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve real files (styles, scripts, icons) directly.
		if name := strings.TrimPrefix(r.URL.Path, "/"); name != "" {
			if info, statErr := fs.Stat(sub, name); statErr == nil && !info.IsDir() {
				if etag, ok := etags[name]; ok {
					w.Header().Set("ETag", etag)
					// Revalidate rather than refetch: the browser keeps its copy and asks whether it
					// is still current. `no-store` here would be simpler and would also throw away a
					// perfectly good cache on every navigation.
					w.Header().Set("Cache-Control", "no-cache")
					if etagMatches(r.Header.Get("If-None-Match"), etag) {
						w.WriteHeader(http.StatusNotModified)
						return
					}
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		theme := DefaultTheme
		if themeFn != nil {
			theme = themeFn(r.Context())
		}
		renderIndex(w, tmpl, theme, baseHref, basePath, opts.Version)
	}), nil
}

// assetETags hashes every embedded asset once, keyed by its path within the assets tree (the same
// key the request handler looks up).
//
// A truncated SHA-256 is enough: this is a cache validator on a LAN, not a signature, and a
// collision would have to occur between two builds of the same file to cause harm.
func assetETags(sub fs.FS) (map[string]string, error) {
	etags := map[string]string{}
	err := fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, readErr := fs.ReadFile(sub, path)
		if readErr != nil {
			return readErr
		}
		sum := sha256.Sum256(data)
		etags[path] = `"` + hex.EncodeToString(sum[:16]) + `"`
		return nil
	})
	if err != nil {
		return nil, err
	}
	return etags, nil
}

// etagMatches reports whether an If-None-Match header covers the given ETag.
//
// The header is a comma-separated list, may be `*`, and may carry the weak `W/` prefix on either
// side. Getting this wrong in the lenient direction means a stale asset is served forever, so the
// comparison is exact apart from the weakness marker.
func etagMatches(header, etag string) bool {
	header = strings.TrimSpace(header)
	if header == "" {
		return false
	}
	if header == "*" {
		return true
	}
	want := strings.TrimPrefix(etag, "W/")
	for _, candidate := range strings.Split(header, ",") {
		if strings.TrimPrefix(strings.TrimSpace(candidate), "W/") == want {
			return true
		}
	}
	return false
}

// ThemeNames lists every theme the UI can render. Named palettes live in
// tokens.css; "custom" builds an accent from settings and "auto" follows the OS.
//
// internal/webui/assets/js/views/settings.js must offer the same list — the two are
// pinned together by tests on both sides.
var ThemeNames = []string{
	"auto", "light", "dark",

	"catppuccin-latte", "catppuccin-frappe", "catppuccin-macchiato", "catppuccin-mocha",
	"dracula", "nord",
	"gruvbox-light", "gruvbox-dark",
	"synthwave-light", "synthwave-dark",
	"hacker",
	"solarized-light", "solarized-dark",
	"tokyo-night",
	"one-dark",

	"custom",
}

type indexData struct {
	Theme     string
	ThemeBase string
	Tint      string
	ThemeVars template.CSS
	BaseHref  string
	BasePath  string
	Version   string
}

func renderIndex(w http.ResponseWriter, tmpl *template.Template, theme Theme, baseHref, basePath, version string) {
	name := normalize(theme.Name, ThemeNames, "auto")
	data := indexData{
		Theme:     name,
		ThemeBase: normalize(theme.Base, []string{"light", "dark"}, "dark"),
		Tint:      normalize(theme.Tint, []string{"neutral", "slate", "warm", "cool"}, "neutral"),
		ThemeVars: template.CSS(themeVars(name, theme)),
		BaseHref:  baseHref,
		BasePath:  basePath,
		Version:   version,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func normalize(value string, allowed []string, fallback string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return fallback
}

// themeVars derives the accent custom properties for a custom theme, including a
// contrast colour chosen for WCAG AA (docs/UI.md §5).
//
// A named palette owns its own accent, so nothing is emitted for it: an inline value
// would win over the stylesheet and flatten every theme to the same blue.
func themeVars(name string, theme Theme) string {
	if name != "custom" {
		return ""
	}
	accent := strings.TrimSpace(theme.Accent)
	if _, _, _, ok := parseHex(accent); !ok {
		accent = DefaultTheme.Accent
	}
	return fmt.Sprintf(
		"--accent:%s;--accent-contrast:%s;--accent-hover:%s;--accent-soft:%s",
		accent, contrastOn(accent), shade(accent, -0.12), withAlpha(accent, 0.16),
	)
}

func parseHex(s string) (r, g, b int, ok bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return 0, 0, 0, false
	}
	n, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return int(n>>16) & 0xff, int(n>>8) & 0xff, int(n) & 0xff, true
}

func luminance(r, g, b int) float64 {
	channel := func(c int) float64 {
		s := float64(c) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(r) + 0.7152*channel(g) + 0.0722*channel(b)
}

func contrastOn(hex string) string {
	r, g, b, ok := parseHex(hex)
	if !ok || luminance(r, g, b) > 0.45 {
		return "#0d1117"
	}
	return "#ffffff"
}

func shade(hex string, amount float64) string {
	r, g, b, ok := parseHex(hex)
	if !ok {
		return hex
	}
	adjust := func(c int) int {
		v := math.Max(0, math.Min(255, float64(c)+255*amount))
		return int(v + 0.5)
	}
	return fmt.Sprintf("#%02x%02x%02x", adjust(r), adjust(g), adjust(b))
}

func withAlpha(hex string, alpha float64) string {
	r, g, b, ok := parseHex(hex)
	if !ok {
		return hex
	}
	return fmt.Sprintf("rgba(%d, %d, %d, %.2f)", r, g, b, alpha)
}
