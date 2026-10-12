// Theme application (docs/UI.md §5). The server renders the initial theme into
// <html>, so there is no flash of the wrong theme; this keeps it in sync when the
// user changes it without a reload.

function parseHex(hex) {
  const match = /^#?([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(hex || '');
  if (!match) return null;
  let h = match[1];
  if (h.length === 3) h = h.split('').map((c) => c + c).join('');
  return [parseInt(h.slice(0, 2), 16), parseInt(h.slice(2, 4), 16), parseInt(h.slice(4, 6), 16)];
}

function luminance([r, g, b]) {
  const channel = (c) => {
    const s = c / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
}

/** Text colour that meets contrast against the given accent. */
export function contrastOn(hex) {
  const rgb = parseHex(hex);
  if (!rgb) return '#ffffff';
  return luminance(rgb) > 0.45 ? '#0d1117' : '#ffffff';
}

export function shade(hex, amount) {
  const rgb = parseHex(hex);
  if (!rgb) return hex;
  const adjust = (c) => Math.max(0, Math.min(255, Math.round(c + 255 * amount)));
  return `#${rgb.map((c) => adjust(c).toString(16).padStart(2, '0')).join('')}`;
}

export function withAlpha(hex, alpha) {
  const rgb = parseHex(hex);
  return rgb ? `rgba(${rgb.join(', ')}, ${alpha})` : hex;
}

/** Apply a settings object to the document root. */
export function applyTheme(settings) {
  const root = document.documentElement;
  if (settings.theme) root.dataset.theme = settings.theme;
  if (settings.theme_base) root.dataset.themeBase = settings.theme_base;
  if (settings.theme_surface_tint) root.dataset.themeTint = settings.theme_surface_tint;

  const accent = settings.theme_accent;
  if (accent) {
    root.style.setProperty('--accent', accent);
    root.style.setProperty('--accent-contrast', contrastOn(accent));
    root.style.setProperty('--accent-hover', shade(accent, -0.12));
    root.style.setProperty('--accent-soft', withAlpha(accent, 0.16));
  }
}
