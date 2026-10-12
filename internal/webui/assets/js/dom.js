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
  for (const child of [].concat(children)) {
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

export function fmtTemp(c) {
  return c === null || c === undefined ? '—' : `${c} °C`;
}

export function fmtScore(score) {
  return score === null || score === undefined || score < 0 ? 'n/a' : `${score}`;
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
