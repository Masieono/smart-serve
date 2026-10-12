// Shared UI components (docs/UI.md §6).
import { el, svg, fmtBytes, fmtTemp, fmtScore, fmtAgo, scoreGrade, verdictClass } from './dom.js';
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

  let band = 'unknown';
  if (hasScore) {
    if (score >= 75) band = 'good';
    else if (score >= 40) band = 'caution';
    else band = 'bad';
  }

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
      el('div', { class: 'gauge-number', text: fmtScore(score) }),
      el('div', { class: 'gauge-grade', text: scoreGrade(score) }),
    ]),
  ]);
}

/** Clickable health counts that filter the device grid. */
export function summaryBar(counts, active, onFilter) {
  const items = [
    ['bad', 'Failing'],
    ['caution', 'Caution'],
    ['good', 'Healthy'],
    ['unknown', 'Unknown'],
  ];
  return el(
    'div',
    { class: 'summary', role: 'group', 'aria-label': 'Filter by health' },
    items.map(([key, label]) =>
      el('button', {
        class: 'summary-item',
        type: 'button',
        'aria-pressed': active === key ? 'true' : 'false',
        onclick: () => onFilter(active === key ? null : key),
      }, [
        el('span', { class: `dot dot-${key}` }),
        el('span', { class: 'summary-count', text: String(counts[key] || 0) }),
        el('span', { class: 'muted', text: label }),
      ]),
    ),
  );
}

export function deviceCard(device, onOpen, onScan) {
  const title = device.name || device.model || device.device_path || device.serial;
  const transportIcon = device.transport === 'usb' ? 'usb' : 'device';
  const mounted = device.mountpoints && device.mountpoints.length
    ? device.mountpoints.join(', ')
    : 'not mounted';

  return el('article', {
    class: 'card device-card',
    tabindex: '0',
    role: 'button',
    onclick: () => onOpen(device.serial),
    onkeydown: (e) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        onOpen(device.serial);
      }
    },
  }, [
    el('div', { class: 'device-card-head' }, [
      el('div', { class: 'stack', style: 'gap:2px;min-width:0;flex:1' }, [
        el('div', { class: 'device-card-title', text: title, title }),
        el('div', { class: 'device-card-model', text: device.model || '—' }),
      ]),
      verdictBadge(device.health),
    ]),
    el('div', { class: 'row' }, [
      healthGauge(device.score, 64),
      el('div', { class: 'stack', style: 'gap:6px;flex:1;min-width:0' }, [
        el('div', { class: 'device-card-stats' }, [
          el('span', {}, [icon(transportIcon), ' ', device.transport || 'unknown']),
          el('span', {}, [icon('device'), ' ', fmtBytes(device.capacity_bytes)]),
          el('span', {}, [icon('alert'), ' ', fmtTemp(device.temperature_c)]),
        ]),
        el('div', { class: 'device-card-stats' }, [el('span', { text: mounted })]),
        el('div', { class: 'muted', style: 'font-size:var(--text-xs)', text: `Scanned ${fmtAgo(device.last_scan)}` }),
      ]),
    ]),
    el('div', { class: 'row' }, [
      el('span', { class: 'muted', style: 'font-size:var(--text-xs)', text: device.health_rationale || '' }),
      el('span', { class: 'spacer' }),
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

/** Confirm dialog; resolves true when confirmed. */
export function confirmDialog(title, message, confirmLabel = 'Confirm') {
  return new Promise((resolve) => {
    let settled = false;
    openDialog(title, (body, close) => {
      body.append(el('p', { text: message }));
      body.append(el('div', { class: 'dialog-foot', style: 'border:0;padding:0' }, [
        el('button', { class: 'btn', type: 'button', text: 'Cancel',
          onclick: () => { settled = true; resolve(false); close(); } }),
        el('button', { class: 'btn btn-danger', type: 'button', text: confirmLabel,
          onclick: () => { settled = true; resolve(true); close(); } }),
      ]));
    });
    // Resolve false if dismissed with Escape.
    setTimeout(() => { if (!settled) resolve(false); }, 0);
  });
}

