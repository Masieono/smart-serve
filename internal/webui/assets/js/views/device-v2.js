// The device page (v2) — the fleet view's other half (`ADR-032`).
//
// This view owns *layout* and nothing else. The trend chart, the attribute table, the NVMe block and
// the partition map are the components the incumbent page already uses, imported rather than
// reimplemented: they carry behaviour that took work to get right — the hover readout, the range
// semantics, the partition map's cross-highlighting against its table — and a second implementation
// of any of them would drift from the first within one iteration.
import {
  el, clear, append, mount, holdHeight, fmtBytes, fmtAgo, fmtInt, fmtTemp,
} from '../dom.js';
import { api, appPath } from '../api.js';
import { attributeTable } from '../components.js';
import {
  trendCard, trendFrom, currentDisplayOptions, diskLayoutCard, nvmeTable, parsePenalties,
} from './device.js';

const STATE_WORD = {
  good: 'Healthy',
  caution: 'Caution',
  bad: 'Failing',
  unknown: 'No reading',
};

// Module-level so the choice survives navigating between drives, exactly as the incumbent keeps it.
let currentSerial = null;

export function reset() {
  currentSerial = null;
}

export async function render(content, ctx, serial, opts = {}) {
  currentSerial = serial;
  const keep = opts.keepScroll === true;
  const y = keep ? window.scrollY : 0;
  // A re-render empties the container before it awaits, and a page that is briefly short makes the
  // browser clamp the scroll position to the top. Holding the height across the swap is what keeps
  // the reader where they were — the same trick the shell uses when a scan finishes.
  const release = keep ? holdHeight(content) : null;

  if (!keep) {
    clear(content);
    mount(content, el('div', { class: 'skeleton', style: 'width:40%;height:24px' }));
  }

  let data;
  try {
    data = await api.device(serial);
  } catch (err) {
    clear(content);
    append(content, [el('div', { class: 'fleet-empty', text: `Could not read this drive: ${err.message}` })]);
    if (release) release();
    return;
  }

  const d = data.device || {};
  const snapshot = data.snapshot || null;
  const payload = snapshot && snapshot.attributes ? snapshot.attributes : null;

  clear(content);
  append(content, [
    backLink(ctx),
    head(ctx, content, d),
    facts(d, snapshot),
    storageSection(d),
    trendSection(serial),
    attributeSection(d, payload),
    footer(d),
  ].filter(Boolean));

  if (release) release();
  if (keep) window.scrollTo(0, y);
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
          await render(content, ctx, d.serial, { keepScroll: true });
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

// --- storage: how full, then what is on the disk ----------------------------

function storageSection(d) {
  const usage = usageLine(d.storage);
  // The incumbent's partition card, minus its own frame: the map and the table are the point, and
  // they already cross-highlight each other. Reusing the component keeps the map's tooltips, the
  // "outside the container" note and the sector-size details without a second implementation.
  const layout = d.disk_layout ? diskLayoutCard(d.disk_layout, { showUsage: showUsage() }) : null;
  if (layout) {
    layout.className = 'v2-partitions';
    for (const sel of ['h2', '.row']) {
      const node = layout.querySelector(sel);
      if (node && node.remove) node.remove();
    }
  }

  if (!usage && !layout) return null;
  return section('Storage', [usage, layout].filter(Boolean));
}

function usageLine(storage) {
  if (!storage || typeof storage.used_pct !== 'number') return null;
  const pct = Math.max(0, Math.min(100, storage.used_pct));
  return el('div', { class: 'v2-usage' }, [
    el('div', { class: 'v2-usage-line' }, [
      el('b', { text: `${Math.round(pct)}% used` }),
      el('span', {
        text: storage.total_bytes
          ? `of ${fmtBytes(storage.total_bytes)}${storage.mount ? ` on ${storage.mount}` : ''}`
          : '',
      }),
    ]),
  ]);
}

function showUsage() {
  const opts = currentDisplayOptions ? currentDisplayOptions() : {};
  return opts.show_usage !== false;
}

// --- trends -----------------------------------------------------------------

/**
 * The trend card, redrawn in place when the window changes.
 *
 * Redrawing only this host is the whole point: re-rendering the page to change a window threw the
 * reader back to the top of a long page, which is the opposite of what a range picker is for. The
 * chart itself — hover readout, arrow keys, the window drawn against the selected span rather than
 * the data's own extent — is the incumbent's `trendCard`, unchanged.
 */
function trendSection(serial) {
  const host = el('div', { class: 'v2-trend-host' });

  const draw = async () => {
    let hist;
    try {
      hist = await api.history(serial, trendFrom());
    } catch {
      clear(host);
      append(host, [el('div', { class: 'fleet-note', text: 'History could not be read.' })]);
      return;
    }
    const series = (hist && hist.series) || [];
    clear(host);
    const card = trendCard(series, draw);
    if (card) {
      card.className = 'v2-trends-plain';
      append(host, [card]);
    }
  };
  draw();

  return section('Trends', [host]);
}

// --- attributes -------------------------------------------------------------

function attributeSection(d, payload) {
  if (!payload) {
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

  // An NVMe drive has no ATA attribute table; its health lives in a log with its own fields. The
  // incumbent page has always known that; the first cut of this one did not, and told an NVMe owner
  // their drive reported nothing.
  const nvme = payload.nvme;
  const attrs = Array.isArray(payload.attributes) ? payload.attributes : [];
  const blamed = new Set(parsePenalties(d.penalties).map((p) => p.attribute_id).filter(Boolean));

  if (nvme) {
    return section('NVMe health log', [nvmeTable(nvme)], `${attrs.length} attributes reported`);
  }

  if (!attrs.length) {
    return section('Attributes', [
      el('div', { class: 'fleet-empty', text: 'This drive reported no attributes at the last scan.' }),
    ]);
  }

  const flagged = attrs.filter((a) => a.threshold > 0 && a.value <= a.threshold).length;
  return section(
    'Attributes',
    [attributeTable(attrs, { blamed, inDatabase: payload.in_database })],
    `${attrs.length} reported · ${flagged ? `${flagged} at threshold` : 'none at threshold'}`,
  );
}

// --- shell bits -------------------------------------------------------------

function section(title, children, aside = null) {
  return el('section', { class: 'v2-section' }, [
    el('div', { class: 'v2-shead' }, [
      el('h2', { text: title }),
      aside ? el('div', { class: 'v2-aside' }, [el('span', { text: aside })]) : null,
    ].filter(Boolean)),
    ...children.filter(Boolean),
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
