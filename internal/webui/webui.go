// Package webui serves the embedded web interface (docs/UI.md).
//
// The theme is rendered server-side into <html>, so the correct theme is applied
// before first paint (no flash of the wrong theme).
package webui

import (
	"context"
	"embed"
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
	Name   string // auto | light | dark | custom
	Base   string // light | dark (used by the custom theme)
	Accent string // #rrggbb
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

	basePath := strings.TrimRight(opts.BasePath, "/")
	baseHref := basePath + "/"
	if baseHref == "" {
		baseHref = "/"
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve real files (styles, scripts, icons) directly.
		if name := strings.TrimPrefix(r.URL.Path, "/"); name != "" {
			if info, statErr := fs.Stat(sub, name); statErr == nil && !info.IsDir() {
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
	data := indexData{
		Theme:     normalize(theme.Name, []string{"auto", "light", "dark", "custom"}, "auto"),
		ThemeBase: normalize(theme.Base, []string{"light", "dark"}, "dark"),
		Tint:      normalize(theme.Tint, []string{"neutral", "slate", "warm", "cool"}, "neutral"),
		ThemeVars: template.CSS(themeVars(theme)),
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

// themeVars derives the accent custom properties, including a contrast colour
// chosen for WCAG AA (docs/UI.md §5).
func themeVars(theme Theme) string {
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
