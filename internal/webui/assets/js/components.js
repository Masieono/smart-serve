// Shared UI components (docs/UI.md §6).
import { el, svg, fmtBytes, fmtTemp, fmtScore, fmtAgo, scoreGrade, scoreBand, verdictClass } from './dom.js';
import { icon } from './icons.js';

const VERDICT_LABEL = { good: 'Good', caution: 'Caution', bad: 'Bad', unknown: 'Unknown' };

/** Verdict pill: icon + word + colour. Colour is never the only signal. */
export function verdictBadge(verdict) {
  const v = verdictClass(verdict);
  const glyph = v === 'good' ? 'check' : v === 'bad' ? 'x' : v === 'caution' ? 'alert' : 'help';
  return el('span', { class: `badge badge-${v}` }, [icon(glyph), VERDICT_LABEL[v]]);
}

/**
 * Circular health gauge (0–100). Colour follows the score band; the number and
 * grade are always shown alongside.
 */
export function healthGauge(score, size = 76) {
  const radius = (size - 10) / 2;
  const circumference = 2 * Math.PI * radius;
  const hasScore = score !== null && score !== undefined && score >= 0;
  const pct = hasScore ? Math.max(0, Math.min(100, score)) : 0;
  const offset = circumference * (1 - pct / 100);
  const band = scoreBand(score);

  const ring = svg(
    `<svg width="${size}" height="${size}" viewBox="0 0 ${size} ${size}" aria-hidden="true">` +
      `<circle class="gauge-track" cx="${size / 2}" cy="${size / 2}" r="${radius}" stroke-width="8"/>` +
      `<circle class="gauge-fill gauge-${band}" cx="${size / 2}" cy="${size / 2}" r="${radius}" stroke-width="8" ` +
      `stroke-dasharray="${circumference.toFixed(2)}" stroke-dashoffset="${offset.toFixed(2)}" ` +
      `transform="rotate(-90 ${size / 2} ${size / 2})"/></svg>`,
  );

  return el('div', { class: 'gauge', style: `width:${size}px;height:${size}px` }, [
    ring,
    el('div', { class: 'gauge-value' }, [
      el('div', {
        class: 'gauge-number',
        style: `font-size:${Math.round(size * 0.26)}px`,
        text: fmtScore(score),
      }),
      el('div', {
        class: 'gauge-grade',
        style: `font-size:${Math.round(size * 0.14)}px`,
        text: scoreGrade(score),
      }),
    ]),
  ]);
}

/**
 * The SVG path for a series, as an "M…L…" string. Pure, so the geometry can be
 * tested without a DOM — and the geometry is the part with edge cases.
 *
 * Returns null for fewer than two readings: a single point is not a trend.
 */
export function sparkLine(values, height = 40) {
  const points = (values || []).filter((v) => typeof v === 'number' && Number.isFinite(v));
  if (points.length < 2) return null;

  const min = Math.min(...points);
  const max = Math.max(...points);
  // A perfectly flat series has no range; treating it as 1 renders a straight line
  // instead of dividing by zero into a row of NaN coordinates.
  const span = max - min || 1;
  const stepX = SPARK_WIDTH / (points.length - 1);
  const y = (v) => height - ((v - min) / span) * (height - 6) - 3;

  return points
    .map((v, i) => `${i === 0 ? 'M' : 'L'}${(i * stepX).toFixed(1)} ${y(v).toFixed(1)}`)
    .join(' ');
}

/**
 * Inline SVG sparkline. Deliberately minimal — no axes, no labels — because the
 * point is the shape and the numbers beside it carry the detail.
 *
 * `band` colours the line by health (good/caution/bad) so a trend is readable at a
 * glance; without it the line uses the neutral data-visualisation colour.
 */
export function sparkline(values, opts = {}) {
  const { height = 40, band = null } = opts;
  const line = sparkLine(values, height);
  if (!line) return null;
  const stroke = band ? `spark-${band}` : 'spark';

  return svg(
    `<svg class="sparkline" viewBox="0 0 ${SPARK_WIDTH} ${height}" preserveAspectRatio="none" aria-hidden="true">` +
      `<path class="spark-area" d="${line} L${SPARK_WIDTH} ${height} L0 ${height} Z"/>` +
      `<path class="${stroke}" d="${line}" vector-effect="non-scaling-stroke"/>` +
      `</svg>`,
  );
}

// The viewBox width is fixed and the element is stretched to its container, so a
// sparkline fills whatever column it lands in.
const SPARK_WIDTH = 240;

/**
 * A labelled trend: current value, the change across the window, and the shape.
 * Rows with fewer than two readings are dropped — a single point is not a trend.
 */
export function trendRow(metric, points) {
  const values = points.map((p) => p.value);
  if (values.length < 2) return null;

  const first = values[0];
  const last = values[values.length - 1];
  const change = metric.delta ? metric.delta(last - first, points) : null;

  return el('div', { class: 'trend-row' }, [
    el('div', { class: 'trend-head' }, [
      el('span', { class: 'setting-label', text: metric.label }),
      el('span', { class: 'spacer' }),
      el('span', { class: 'trend-value', text: metric.format(last) }),
      change ? el('span', { class: `trend-delta ${change.tone}`, text: change.text }) : null,
    ]),
    sparkline(values, { band: metric.band ? scoreBand(last) : null }),
  ]);
}

/**
 * Clickable health counts, rendered as a compact inline list rather than a row of
 * boxes: these are a filter control, and four bordered cards spent an entire row on
 * four numbers.
 */
export function healthFilter(counts, active, onFilter) {
  const items = [
    ['bad', 'failing'],
    ['caution', 'caution'],
    ['good', 'healthy'],
    ['unknown', 'unknown'],
  ];
  return el('div', { class: 'health-list', role: 'group', 'aria-label': 'Filter by health' },
    items.map(([key, label]) => {
      const count = counts[key] || 0;
      return el('button', {
        class: 'health-item',
        type: 'button',
        'aria-pressed': active === key ? 'true' : 'false',
        // A count of zero is still worth showing — "0 failing" is the reassurance
        // you opened the app for — but it should not look like a live filter.
        'aria-label': `${count} ${label}`,
        onclick: () => onFilter(active === key ? null : key),
      }, [
        el('span', { class: `dot dot-${key}` }),
        el('span', { class: 'health-count', text: String(count) }),
        el('span', { class: 'health-label', text: label }),
      ]);
    }));
}

/** Friendly transport name; "usb"/"sata"/"nvme" alone are ambiguous. */
export function transportLabel(device) {
  const t = (device.transport || '').toLowerCase();
  if (t === 'usb') return 'USB';
  if (t === 'nvme') return 'NVMe';
  if (t === 'sata') return 'SATA';
  if (t === 'sas') return 'SAS';
  return t || 'unknown';
}

export function deviceCard(device, onOpen, onScan) {
  const title = device.name || device.model || device.device_path || device.serial;
  const mounted = device.mountpoints && device.mountpoints.length
    ? device.mountpoints.join(', ')
    : 'not mounted';

  // Text labels rather than bare icons: drive type and capacity are otherwise
  // ambiguous, and icon sizes vary enough to look inconsistent (docs/UI.md §6).
  const rows = [
    ['Transport', transportLabel(device)],
    ['Capacity', fmtBytes(device.capacity_bytes)],
    ['Temperature', fmtTemp(device.temperature_c)],
    ['Mounted', mounted],
    ['Last scan', fmtAgo(device.last_scan)],
  ];

  return el('article', {
    class: `card device-card${device.is_missing ? ' device-card-missing' : ''}`,
    tabindex: '0',
    role: 'button',
    title: device.is_missing ? 'This device was not present in the last scan' : '',
    onclick: () => onOpen(device.serial),
    onkeydown: (e) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        onOpen(device.serial);
      }
    },
  }, [
    el('div', { class: 'device-card-head' }, [
      el('div', { class: 'device-card-titles' }, [
        el('div', { class: 'device-card-title', text: title, title }),
        el('div', {
          class: 'device-card-model',
          text: `${device.model || '—'}${device.device_path ? ` · ${device.device_path}` : ''}`,
        }),
      ]),
      device.is_missing
        ? el('span', { class: 'badge badge-unknown' }, [icon('help'), 'Not connected'])
        : verdictBadge(device.health),
    ]),
    el('div', { class: 'device-card-body' }, [
      healthGauge(device.score, 88),
      el('div', { class: 'kv' }, rows.flatMap(([label, value]) => [
        el('span', { class: 'kv-label', text: label }),
        el('span', { class: 'kv-value', text: value, title: value }),
      ])),
    ]),
    el('div', { class: 'device-card-foot' }, [
      el('span', {
        class: 'muted device-card-rationale',
        text: device.health_rationale || '',
        title: device.health_rationale || '',
      }),
      el('button', {
        class: 'btn btn-sm btn-ghost',
        type: 'button',
        title: 'Scan this device',
        onclick: (e) => {
          e.stopPropagation();
          onScan();
        },
      }, [icon('scan')]),
    ]),
  ]);
}

/** Attribute table in the CrystalDiskInfo layout. */
export function attributeTable(attributes) {
  if (!attributes || !attributes.length) {
    return el('p', { class: 'muted', text: 'No ATA attribute table was reported for this device.' });
  }
  const head = ['ID', 'Attribute', 'Value', 'Worst', 'Thresh', 'Raw'];
  return el('div', { class: 'table-wrap' }, [
    el('table', { class: 'data' }, [
      el('thead', {}, [
        el('tr', {}, head.map((h, i) => el('th', { text: h, class: i === 0 || i > 1 ? 'num' : '' }))),
      ]),
      el('tbody', {}, attributes.map((a) =>
        el('tr', {}, [
          el('td', { class: 'num', text: String(a.id) }),
          el('td', { text: a.name }),
          el('td', { class: 'num', text: String(a.value) }),
          el('td', { class: 'num', text: String(a.worst) }),
          el('td', { class: 'num', text: String(a.threshold) }),
          el('td', { class: 'num', text: a.raw_string || String(a.raw_value) }),
        ]),
      )),
    ]),
  ]);
}

export function emptyState(title, body, action) {
  return el('div', { class: 'empty' }, [
    el('h2', { text: title }),
    body ? el('p', { text: body }) : null,
    action || null,
  ]);
}

export function skeletonCards(count = 3) {
  return el('div', { class: 'card-grid' }, Array.from({ length: count }, () =>
    el('div', { class: 'card stack' }, [
      el('div', { class: 'skeleton', style: 'width:60%;height:18px' }),
      el('div', { class: 'skeleton', style: 'width:40%' }),
      el('div', { class: 'skeleton', style: 'width:100%;height:64px' }),
    ])));
}

export function errorBox(message) {
  return el('div', { class: 'error-box', role: 'alert', text: message });
}

// --- toasts -------------------------------------------------------------------

export function toast(message, kind = 'info') {
  const host = document.getElementById('toasts');
  if (!host) return;
  const node = el('div', { class: `toast toast-${kind}` }, [
    icon(kind === 'error' ? 'alert' : 'check'),
    el('span', { text: message }),
  ]);
  host.append(node);
  setTimeout(() => node.remove(), kind === 'error' ? 7000 : 3500);
}

// --- dialog -------------------------------------------------------------------

/** Modal dialog. `render` receives the body element and a close() callback. */
export function openDialog(title, render) {
  const body = el('div', { class: 'dialog-body' });
  const dialog = el('dialog', { 'aria-label': title }, [
    el('div', { class: 'dialog-head' }, [el('h2', { text: title })]),
    body,
  ]);
  dialog.addEventListener('close', () => dialog.remove());
  const close = () => dialog.close();
  render(body, close);
  document.body.append(dialog);
  dialog.showModal();
  return { dialog, close };
}

/** Confirm dialog; resolves true when confirmed, false on cancel or dismissal. */
export function confirmDialog(title, message, confirmLabel = 'Confirm') {
  return new Promise((resolve) => {
    let settled = false;
    const settle = (value) => {
      if (settled) return;
      settled = true;
      resolve(value);
    };

    const { dialog } = openDialog(title, (body, close) => {
      body.append(el('p', { text: message }));
      body.append(el('div', { class: 'dialog-foot', style: 'border:0;padding:0' }, [
        el('button', { class: 'btn', type: 'button', text: 'Cancel',
          onclick: () => { settle(false); close(); } }),
        el('button', { class: 'btn btn-danger', type: 'button', text: confirmLabel,
          onclick: () => { settle(true); close(); } }),
      ]));
    });

    // Escape, or any other dismissal, closes the dialog without a button press.
    // Listening for `close` is the only reliable signal. Settling on a timer
    // instead resolves the promise before the user has even seen the dialog, so
    // every caller's `if (!(await confirmDialog(...))) return` bails out and the
    // confirmed action silently never happens.
    dialog.addEventListener('close', () => settle(false));
  });
}

