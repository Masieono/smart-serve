// The device page (v2) — the fleet view's other half (`ADR-032`).
//
// It reads `GET /api/devices/{serial}`, whose payload is `{device, snapshot, metrics, projection}`,
// plus `/history` for the trends. The attributes live inside `snapshot.attributes` because they are
// a reading rather than current state — the same reason the fleet list does not carry them.
import {
  el, clear, append, fmtBytes, fmtAgo, fmtInt, fmtTemp, fmtCell,
} from '../dom.js';
import { api, appPath } from '../api.js';

const STATE_WORD = {
  good: 'Healthy',
  caution: 'Caution',
  bad: 'Failing',
  unknown: 'No reading',
};

let currentSerial = null;
let currentRange = 30;

export function reset() {
  currentSerial = null;
  currentRange = 30;
}

export async function render(content, ctx, serial) {
  currentSerial = serial;
  clear(content);

  let data;
  try {
    data = await api.device(serial);
  } catch (err) {
    append(content, [
      el('div', { class: 'fleet-empty', text: `Could not read this drive: ${err.message}` }),
    ]);
    return;
  }

  const d = data.device || {};
  const snapshot = data.snapshot || null;
  const attrs = attributesOf(snapshot);
  const usage = storageSection(d);

  clear(content);
  append(content, [
    backLink(ctx),
    head(ctx, content, d),
    facts(d, snapshot),
    ...(usage ? [usage] : []),
    ...(attrs.length ? [attributeSection(d, attrs)] : [noAttributeNotice(d)]),
    trendSection(ctx, content, serial, d),
    footer(d),
  ]);
}

// --- head -------------------------------------------------------------------

function backLink(ctx) {
  return el('a', {
    class: 'v2-back',
    href: appPath('/'),
    text: '← Fleet',
    onclick: (event) => {
      event.preventDefault();
      ctx.navigate('/');
    },
  });
}

function head(ctx, content, d) {
  const state = stateOf(d);
  const name = d.name || d.model || d.serial;

  return el('div', { class: 'v2-head' }, [
    el('div', {}, [
      el('h1', { text: name }),
      el('div', { class: 'v2-sub' }, [
        [d.model, d.transport, d.firmware ? `firmware ${d.firmware}` : ''].filter(Boolean).join(' · '),
        el('span', { class: 'mono', text: d.serial }),
      ]),
      el('div', { class: 'v2-verdict ' + state }, [
        el('i', { class: 'dot' }),
        el('b', { text: d.is_missing ? 'Not connected' : STATE_WORD[state] }),
        d.score >= 0 ? el('span', { text: `score ${d.score} of 100` }) : null,
        d.health_rationale ? el('span', { class: 'why', text: clean(d.health_rationale) }) : null,
      ].filter(Boolean)),
    ]),
    el('div', { class: 'v2-actions' }, [
      el('button', {
        class: 'v2-btn',
        type: 'button',
        text: 'Scan this drive',
        onclick: async (event) => {
          const b = event.currentTarget;
          b.disabled = true;
          b.textContent = 'Reading…';
          try {
            await api.scanDevice(d.serial);
          } catch (err) {
            toast(`Scan failed: ${err.message}`);
          }
          await render(content, ctx, d.serial);
        },
      }),
      el('a', {
        class: 'v2-btn',
        href: api.reportUrl(d.serial, 'html'),
        target: '_blank',
        rel: 'noopener',
        text: 'Export…',
      }),
    ]),
  ]);
}

// --- facts ------------------------------------------------------------------

function facts(d, snapshot) {
  const rows = [
    ['Capacity', d.capacity_bytes ? fmtBytes(d.capacity_bytes) : '—'],
    ['Powered on', snapshot && snapshot.power_on_hours != null ? `${fmtInt(snapshot.power_on_hours)} h` : '—'],
    ['Wear', snapshot && snapshot.wear_pct != null ? `${Math.round(snapshot.wear_pct)}%` : '—'],
    ['Temperature', snapshot && snapshot.temperature_c != null ? fmtTemp(snapshot.temperature_c) : '—'],
    ['Mounted at', (d.mountpoints || []).length ? d.mountpoints.join(', ') : '—'],
    ['Device node', d.device_path || '—'],
    ['Bay', d.bay || '—'],
    ['Last scanned', d.last_scan ? fmtAgo(d.last_scan) : 'never'],
  ];
  return el('dl', { class: 'v2-facts' }, rows.map(([k, v]) => el('div', { class: 'v2-fact' }, [
    el('dt', { text: k }),
    el('dd', { text: String(v) }),
  ])));
}

// --- usage ------------------------------------------------------------------

function storageSection(d) {
  const s = d.storage;
  if (!s || typeof s.used_pct !== 'number') return null;
  const pct = Math.max(0, Math.min(100, s.used_pct));
  const high = pct >= 85;
  return section('Usage', [
    el('div', { class: 'v2-usage' }, [
      el('div', { class: 'v2-usage-line' }, [
        el('b', { text: `${Math.round(pct)}% used` }),
        el('span', {
          text: s.total_bytes ? `of ${fmtBytes(s.total_bytes)}${s.mount ? ` on ${s.mount}` : ''}` : '',
        }),
      ]),
      el('div', { class: 'v2-bar' }, [
        el('i', { class: high ? 'high' : null, style: `width:${pct}%` }),
      ]),
    ]),
  ]);
}

// --- attributes -------------------------------------------------------------

function attributesOf(snapshot) {
  const payload = snapshot && snapshot.attributes;
  if (!payload || !Array.isArray(payload.attributes)) return [];
  return payload.attributes;
}

function isFlagged(a) {
  return a.threshold > 0 && a.value <= a.threshold;
}

function attributeSection(d, attrs) {
  const flagged = attrs.filter(isFlagged).length;
  const body = el('tbody', {});
  for (const a of attrs) {
    append(body, [el('tr', { class: isFlagged(a) ? 'flagged' : null }, [
      el('td', { class: 'id', text: String(a.id) }),
      el('td', { class: 'name', text: String(a.name).replace(/_/g, ' ') }),
      el('td', { class: 'r', text: String(a.value) }),
      el('td', { class: 'r', text: a.raw_string || String(a.raw_value ?? '') }),
      el('td', { class: 'r', text: String(a.worst) }),
      el('td', { class: 'r' }, [
        el('span', { text: String(a.threshold) }),
        isFlagged(a) ? el('span', { class: 'flagword', text: ' at limit' }) : null,
      ].filter(Boolean)),
    ])]);
  }

  return section(
    'Attributes',
    [el('table', { class: 'v2-table v2-attr' }, [
      el('colgroup', {}, [
        el('col', { class: 'id' }), el('col', { class: 'name' }), el('col', { class: 'v' }),
        el('col', { class: 'raw' }), el('col', { class: 'worst' }), el('col', { class: 'th' }),
      ]),
      el('thead', {}, [el('tr', {}, [
        el('th', { text: 'ID' }),
        el('th', { text: 'Attribute' }),
        el('th', { class: 'r', text: 'Value' }),
        el('th', { class: 'r', text: 'Raw' }),
        el('th', { class: 'r', text: 'Worst' }),
        el('th', { class: 'r', text: 'Threshold' }),
      ])]),
      body,
    ])],
    `${attrs.length} reported · ${flagged ? `${flagged} at threshold` : 'none at threshold'}`,
  );
}

function noAttributeNotice(d) {
  return section('Attributes', [
    el('div', { class: 'fleet-empty' }, [
      el('div', {
        text: d.smart_available
          ? 'This drive reported no attributes at the last scan.'
          : 'This drive does not report S.M.A.R.T. data, so there are no attributes to show.',
      }),
      d.smart_error ? el('div', { class: 'v2-mono-note', text: clean(d.smart_error) }) : null,
    ].filter(Boolean)),
  ]);
}

// --- trends -----------------------------------------------------------------

function trendSection(ctx, content, serial, d) {
  const card = section('Trends', [
    el('div', { class: 'v2-trends', id: 'v2-trends' }, [el('div', { class: 'fleet-note', text: 'Loading history…' })]),
  ], null, rangeButtons(async (days) => {
    currentRange = days;
    await render(content, ctx, serial);
  }));

  // Loaded after the shell is on screen: the page is useful without the chart, so nothing waits on it.
  api.history(serial, fromParam(currentRange)).then((hist) => {
    const host = document.getElementById('v2-trends');
    if (!host) return;
    clear(host);
    const series = (hist && hist.series) || [];
    if (series.length < 2) {
      append(host, [el('div', { class: 'fleet-note', text: 'Not enough history yet — trends appear once this drive has been scanned a few times.' })]);
      return;
    }
    append(host, [
      spark('Health score', series, 'score', (v) => String(v), d),
      spark('Temperature', series, 'temperature_c', (v) => fmtTemp(v), d),
      spark('Powered on', series, 'power_on_hours', (v) => `${fmtInt(v)} h`, d),
      spark('Wear', series, 'wear_pct', (v) => `${Math.round(v)}%`, d),
    ]);
  }).catch(() => {
    const host = document.getElementById('v2-trends');
    if (host) {
      clear(host);
      append(host, [el('div', { class: 'fleet-note', text: 'History could not be read.' })]);
    }
  });

  return card;
}

function fromParam(days) {
  if (days === 'all') return null;
  const d = new Date();
  d.setDate(d.getDate() - days);
  return d.toISOString();
}

function rangeButtons(onPick) {
  const ranges = [[7, '7d'], [30, '30d'], [90, '90d'], ['all', 'all']];
  return el('div', { class: 'v2-ranges' }, ranges.map(([v, label]) => el('button', {
    type: 'button',
    'aria-pressed': String(currentRange === v),
    text: label,
    onclick: () => onPick(v),
  })));
}

function spark(label, series, key, fmt, d) {
  const points = series
    .map((p) => ({ ts: p.ts, v: p[key] }))
    .filter((p) => p.v !== null && p.v !== undefined);

  if (points.length < 2) {
    return el('div', { class: 'v2-trend' }, [
      el('div', { class: 'label', text: label }),
      el('div', { class: 'fleet-note', text: 'no readings in this window' }),
    ]);
  }

  const values = points.map((p) => p.v);
  const min = Math.min(...values);
  const max = Math.max(...values);
  const span = max - min || 1;
  const w = 200;
  const h = 40;
  const coords = points.map((p, i) => {
    const x = (i / (points.length - 1)) * w;
    const y = h - ((p.v - min) / span) * (h - 4) - 2;
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  }).join(' ');

  const last = points[points.length - 1].v;
  const first = points[0].v;
  const delta = last - first;

  return el('div', { class: 'v2-trend' }, [
    el('div', { class: 'label', text: label }),
    el('div', { class: 'now' }, [
      fmt(last),
      el('small', { text: delta === 0 ? 'no change' : `${delta > 0 ? '+' : ''}${Math.round(delta * 10) / 10}` }),
    ]),
    el('div', { class: 'chart' }, [
      el('span', { class: 'min', text: fmt(min) }),
      el('span', { class: 'max', text: fmt(max) }),
      html(`<svg viewBox="0 0 ${w} ${h}" preserveAspectRatio="none" aria-hidden="true"><polyline points="${coords}" fill="none" vector-effect="non-scaling-stroke" stroke-width="1.4"/></svg>`),
    ]),
  ]);
}

/** An inline SVG, which `el` cannot build — the chart is decoration around the numbers. */
function html(markup) {
  const wrap = document.createElement('span');
  wrap.innerHTML = markup;
  return wrap.firstElementChild;
}

// --- shell bits -------------------------------------------------------------

function section(title, children, aside = null, tools = null) {
  return el('section', { class: 'v2-section' }, [
    el('div', { class: 'v2-shead' }, [
      el('h2', { text: title }),
      el('div', { class: 'v2-aside' }, [tools, aside ? el('span', { text: aside }) : null].filter(Boolean)),
    ]),
    ...children,
  ]);
}

function footer(d) {
  return el('div', { class: 'v2-foot' }, [
    el('div', {
      text: d.last_scan
        ? `Read from ${d.device_path || 'the drive'} ${fmtAgo(d.last_scan)}`
        : 'Never scanned',
    }),
  ]);
}

function stateOf(d) {
  if (d.is_missing) return 'unknown';
  const h = d.health;
  return h === 'good' || h === 'caution' || h === 'bad' ? h : 'unknown';
}

function clean(s) {
  return String(s || '').replace(/^error:\s*/i, '');
}

function toast(message) {
  const host = document.getElementById('toasts');
  if (!host) return;
  const node = el('div', { class: 'toast', text: message });
  host.append(node);
  setTimeout(() => node.remove(), 6000);
}
