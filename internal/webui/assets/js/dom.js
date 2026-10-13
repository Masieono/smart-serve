// Tiny DOM helpers. No framework, no build step (docs/UI.md §10).

/**
 * Create an element.
 * props: class | text | html | dataset | on:{event:fn} | any DOM property |
 *        any attribute (fallback).
 */
export function el(tag, props = {}, children = []) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props)) {
    if (value === null || value === undefined || value === false) continue;
    if (key === 'class') node.className = value;
    else if (key === 'text') node.textContent = value;
    else if (key === 'html') node.innerHTML = value;
    else if (key === 'dataset') Object.assign(node.dataset, value);
    else if (key === 'on') {
      for (const [event, handler] of Object.entries(value)) node.addEventListener(event, handler);
    } else if (key in node) node[key] = value;
    else node.setAttribute(key, value === true ? '' : String(value));
  }
  append(node, children);
  return node;
}

export function append(node, children) {
  // Flatten nested arrays: views commonly pass arrays of nodes (e.g. a grid of
  // cards, table rows, chip lists) and those must be appended, not stringified.
  for (const child of children.flat(Infinity)) {
    if (child === null || child === undefined || child === false) continue;
    node.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
  return node;
}

export function clear(node) {
  while (node.firstChild) node.removeChild(node.firstChild);
  return node;
}

export function mount(node, ...children) {
  return append(clear(node), children);
}

/** Parse an SVG string into an element. */
export function svg(markup) {
  const wrap = document.createElement('div');
  wrap.innerHTML = markup.trim();
  return wrap.firstElementChild;
}

// --- formatters ---------------------------------------------------------------

export function fmtBytes(bytes) {
  if (bytes === null || bytes === undefined) return 'unknown';
  const units = ['B', 'kB', 'MB', 'GB', 'TB', 'PB'];
  let value = Number(bytes);
  let i = 0;
  while (value >= 1000 && i < units.length - 1) {
    value /= 1000;
    i += 1;
  }
  return `${i === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[i]}`;
}

export function fmtDateTime(iso) {
  if (!iso) return '—';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString();
}

export function fmtAgo(iso) {
  if (!iso) return 'never';
  const secs = Math.round((Date.now() - new Date(iso).getTime()) / 1000);
  if (Number.isNaN(secs)) return 'never';
  if (secs < 10) return 'just now';
  if (secs < 60) return `${secs}s ago`;
  const mins = Math.round(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.round(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.round(hours / 24)}d ago`;
}

export function fmtHours(hours) {
  if (hours === null || hours === undefined) return '—';
  const years = hours / 8760;
  if (years >= 1) return `${years.toFixed(1)} yr`;
  const days = hours / 24;
  if (days >= 1) return `${days.toFixed(0)} d`;
  return `${hours} h`;
}

export function fmtScore(score) {
  return score === null || score === undefined || score < 0 ? 'n/a' : `${score}`;
}

// --- temperature ---------------------------------------------------------------

// Celsius is canonical everywhere else: the API, the database, snapshots and
// notification payloads all carry °C, because that is what the drives report. The
// unit is a display preference, and it is applied here and nowhere else.
let currentUnit = 'c';

/** Set the display unit ('c' or 'f'); anything else falls back to Celsius. */
export function setTemperatureUnit(value) {
  currentUnit = String(value ?? '').trim().toLowerCase() === 'f' ? 'f' : 'c';
}

/** The active unit as a lowercase code ('c' | 'f'). */
export function temperatureUnit() {
  return currentUnit;
}

/** The active unit as a single uppercase letter, for labels. */
export function temperatureUnitName() {
  return currentUnit === 'f' ? 'F' : 'C';
}

/**
 * Convert a Celsius reading for display. The stored value is never changed.
 *
 * `targetUnit` is explicit because a settings form may be editing values captured
 * under the *previous* unit while the new one has already been applied.
 */
export function convertTemp(celsius, targetUnit = currentUnit) {
  if (celsius === null || celsius === undefined) return null;
  const n = Number(celsius);
  if (!Number.isFinite(n)) return null;
  return targetUnit === 'f' ? Math.round((n * 9) / 5 + 32) : n;
}

/** A value entered in `fromUnit` back to canonical Celsius, for saving. */
export function toCelsius(displayed, fromUnit = currentUnit) {
  const n = Number(displayed);
  if (!Number.isFinite(n)) return null;
  return fromUnit === 'f' ? Math.round(((n - 32) * 5) / 9) : n;
}

/**
 * A temperature *difference* in the display unit.
 *
 * A delta is a span, not a reading, so it scales without the offset — using
 * convertTemp on it would report a 2 °C rise as a 36 °F rise.
 */
export function convertTempDelta(delta, targetUnit = currentUnit) {
  // Number(null) is 0, so a missing reading would otherwise render as a real
  // "0 °C" change rather than nothing at all.
  if (delta === null || delta === undefined) return null;
  const n = Number(delta);
  if (!Number.isFinite(n)) return null;
  return targetUnit === 'f' ? Math.round((n * 9) / 5) : n;
}

export function fmtTemp(celsius) {
  const value = convertTemp(celsius);
  return value === null ? '—' : `${value} °${temperatureUnitName()}`;
}

/** Thousands separators for raw counters. */
export function fmtInt(value) {
  if (value === null || value === undefined) return '—';
  return Number(value).toLocaleString();
}

/** Colour band for a 0–100 score, used by gauges and sparklines. */
export function scoreBand(score) {
  if (score === null || score === undefined || score < 0) return 'unknown';
  if (score >= 75) return 'good';
  if (score >= 40) return 'caution';
  return 'bad';
}

/** Grade label from the score (docs/SMART_INTERPRETATION.md §8.1). */
export function scoreGrade(score) {
  if (score === null || score === undefined || score < 0) return 'n/a';
  if (score >= 90) return 'Excellent';
  if (score >= 75) return 'Good';
  if (score >= 40) return 'Fair';
  if (score >= 25) return 'Poor';
  return 'Critical';
}

export function verdictClass(verdict) {
  return ['good', 'caution', 'bad'].includes(verdict) ? verdict : 'unknown';
}
