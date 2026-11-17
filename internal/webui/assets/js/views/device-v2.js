// The device page (v2), the fleet view's other half (`ADR-032`).
//
// It owns layout. What it reuses is the *primitives*, `sparkline` (hover readout, arrow keys, a
// window drawn against the selected span), `attributeTable`, `diskMap`, `partitionTable`, and the
// data definitions the incumbent page keeps (`TREND_METRICS`, `nvmeSections`). What it does not do
// is adopt the incumbent's *arrangements*: those were built for a two-column card page, and dropped
// onto a full-width page they spread every label and value to opposite edges.
import {
  el, clear, append, mount, holdHeight, fmtBytes, fmtAgo, fmtInt, fmtTemp, scoreBand,
} from '../dom.js';
import { api, appPath } from '../api.js';
import {
  attributeTable, sparkline, dateAxis, helpButton, diskMap, partitionTable,
} from '../components.js';
import { nvmeHelp } from '../attributes.js';
import {
  TREND_METRICS, seriesFor, trendWindow, setTrendRange, currentTrendRange, trendFrom,
  currentDisplayOptions, nvmeSections, parsePenalties, startFreshness,
  highlightPartitionRow, highlightSegment,
} from './device.js';

const STATE_WORD = {
  good: 'Healthy',
  caution: 'Caution',
  bad: 'Failing',
  unknown: 'No reading',
};

const RANGES = [['7', '7d'], ['30', '30d'], ['90', '90d'], ['all', 'All']];

/**
 * The sixth trend.
 *
 * Six fits two rows of three, and this is the metric worth the slot: reallocated sectors is the shape
 * that precedes a failure, and a drive can read "good" while the count climbs. Health score and wear
 * both summarise it after the fact; this is the raw evidence.
 */
const SECTOR_METRIC = {
  key: 'sectors',
  label: 'Bad sectors',
  format: (v) => `${Math.round(v)}`,
  delta: (d) => ({ text: d === 0 ? 'no change' : `${d > 0 ? '+' : ''}${Math.round(d)}`, tone: d > 0 ? 'bad' : 'good' }),
};

const V2_METRICS = [...TREND_METRICS, SECTOR_METRIC];

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
  // the reader where they were, the same trick the shell uses when a scan finishes.
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
  const freshness = el('div', { class: 'v2-freshness' });
  append(content, [
    backLink(ctx),
    head(ctx, content, d),
    freshness,
    facts(d, snapshot),
    diskSection(d),
    trendSection(serial),
    attributeSection(d, payload),
    footer(d),
  ].filter(Boolean));

  if (release) release();
  if (keep) window.scrollTo(0, y);

  // Opening a drive reads it when the stored data is older than the refresh window, and says so
  // while it does. Dropped from the first v2 cut, which quietly made every page show scan-era data
  //, the app's honest-freshness promise (`docs/UI.md` §1) depends on this.
  startFreshness(ctx, d, freshness);
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
    ['Capacity', d.capacity_bytes ? fmtBytes(d.capacity_bytes) : 'n/a'],
    ['Powered on', snapshot && snapshot.power_on_hours != null ? `${fmtInt(snapshot.power_on_hours)} h` : 'n/a'],
    ['Wear', snapshot && snapshot.wear_pct != null ? `${Math.round(snapshot.wear_pct)}%` : 'n/a'],
    ['Temperature', snapshot && snapshot.temperature_c != null ? fmtTemp(snapshot.temperature_c) : 'n/a'],
    ['Mounted at', (d.mountpoints || []).length ? d.mountpoints.join(', ') : 'n/a'],
    ['Device node', d.device_path || 'n/a'],
    ['Bay', d.bay || 'n/a'],
    ['Last scanned', d.last_scan ? fmtAgo(d.last_scan) : 'never'],
  ];
  return el('dl', { class: 'v2-facts' }, rows.map(([k, v]) => el('div', { class: 'v2-fact' }, [
    el('dt', { text: k }),
    el('dd', { text: String(v) }),
  ])));
}

// --- disk space: how full, and by what --------------------------------------

function diskSection(d) {
  const parts = (d.disk_layout && d.disk_layout.partitions) || [];
  const children = [];

  const usage = usageLine(d.storage, parts.length === 0);
  if (usage) children.push(usage);

  if (parts.length) {
    // The table and the map mark each other. `diskLayoutCard` wires this; here the two are laid out
    // by hand so the layout can differ, and the behaviour has to come with them (`docs/UI.md` §6).
    let map = null;
    const table = partitionTable(d.disk_layout, {
      showUsage: showUsage(),
      onHover: (name) => {
        highlightPartitionRow(table, name);
        highlightSegment(map, name);
      },
    });
    map = diskMap(d.disk_layout, {
      onHover: (name) => highlightPartitionRow(table, name),
    });
    if (map) children.push(el('div', { class: 'v2-disk-map' }, [map]));
    if (table) children.push(el('div', { class: 'v2-disk-table' }, [table]));
  } else if (d.disk_layout && !parts.length) {
    children.push(el('div', { class: 'fleet-note', text: 'This drive has no partition table, it holds no filesystems, or nothing readable from inside the container.' }));
  }

  if (!children.length) return null;
  return section('Disk space', children);
}

function usageLine(storage, withBar) {
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
    // The partition map below *is* the bar when there is one; drawing both puts the same figure on
    // screen twice, one line apart.
    withBar ? el('div', { class: 'v2-bar' }, [el('i', { class: pct >= 85 ? 'high' : null, style: `width:${pct}%` })]) : null,
  ].filter(Boolean));
}

function showUsage() {
  const opts = currentDisplayOptions ? currentDisplayOptions() : {};
  return opts.show_usage !== false;
}

// --- trends -----------------------------------------------------------------

/**
 * Every metric in one row, each its own small plot.
 *
 * Horizontal because these are read as a set, "is anything drifting?" is asked once, not five times
 *, and because five stacked plots cost about 600px of page for 200px of information. The plot itself
 * is the incumbent's `sparkline`, so the hover readout, the arrow-key stepping and the window drawn
 * against the selected span all survive the rearrangement.
 */
function trendSection(serial) {
  const host = el('div', { class: 'v2-trend-host' });
  const tools = el('div', { class: 'v2-aside' });

  const draw = async () => {
    let series = [];
    try {
      const hist = await api.history(serial, trendFrom());
      series = (hist && hist.series) || [];
    } catch {
      clear(host);
      clear(tools);
      append(host, [el('div', { class: 'fleet-note', text: 'History could not be read.' })]);
      return;
    }

    // Rebuilt with the strip: the pressed state belongs to the current range, and leaving the
    // buttons from the previous draw in place is how the picker ended up looking stuck while the
    // data behind it had already changed.
    clear(tools);
    // How many readings the window actually holds. A two-point series in a thirty-day window draws
    // as a speck at the right edge, which looks like a broken chart until the count explains it.
    tools.append(el('span', { class: 'v2-readings', text: readingsText(series) }));
    tools.append(trendRanges(draw));

    clear(host);
    append(host, [trendStrip(series, draw)]);
  };
  draw();

  return el('section', { class: 'v2-section' }, [
    el('div', { class: 'v2-shead' }, [el('h2', { text: 'Trends' }), tools]),
    host,
  ]);
}

function readingsText(series) {
  const n = Array.isArray(series) ? series.length : 0;
  if (n === 0) return 'no readings in this window';
  return `${n} reading${n === 1 ? '' : 's'}`;
}

function trendRanges(onPick) {
  const active = currentTrendRange();
  return el('div', { class: 'v2-ranges' }, RANGES.map(([value, label]) => el('button', {
    type: 'button',
    'aria-pressed': String(active === value),
    text: label,
    onclick: () => {
      setTrendRange(value);
      onPick();
    },
  })));
}

function trendStrip(series, onRange) {
  const window = trendWindow(series);
  const grid = el('div', { class: 'v2-trend-grid' });

  for (const metric of V2_METRICS) {
    const points = seriesFor(metric.key, series);
    const cell = el('div', { class: 'v2-trend' }, [
      el('div', { class: 'label', text: metric.label }),
    ]);

    if (points.length < 2) {
      cell.append(el('div', { class: 'v2-trend-none', text: 'not enough readings' }));
      grid.append(cell);
      continue;
    }

    const last = points[points.length - 1].value;
    const change = metric.delta ? metric.delta(last - points[0].value, points) : null;
    cell.append(el('div', { class: 'now' }, [
      metric.format(last),
      change ? el('small', { class: change.tone, text: change.text }) : null,
    ].filter(Boolean)));

    const plot = sparkline(points, {
      band: metric.band ? scoreBand(last) : null,
      format: metric.format,
      window,
      // A dot per reading: gaps in scanning become visible, and a two-point series stops looking
      // like a rendering failure.
      dots: true,
    });
    if (plot) cell.append(plot);
    grid.append(cell);
  }

  // A window shorter than a day cannot be labelled with dates: every tick prints the same one, as
  // the "All" range does on a drive scanned twice in an afternoon.
  const spansADay = window && window.to - window.from >= 86400000;
  const axis = spansADay ? dateAxis(window) : null;
  if (axis) {
    const axisRow = el('div', { class: 'v2-trend-axis' }, [axis]);
    grid.append(axisRow);
  }

  return grid;
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

  // An NVMe drive has no ATA attribute table; its health lives in a log with its own fields.
  const nvme = payload.nvme;
  const attrs = Array.isArray(payload.attributes) ? payload.attributes : [];
  const blamed = new Set(parsePenalties(d.penalties).map((p) => p.attribute_id).filter(Boolean));

  if (nvme) {
    return section('NVMe health log', [nvmeGrid(nvme)]);
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

/**
 * The NVMe log as a grid of groups rather than a two-column table.
 *
 * The table it replaces put each label at the left edge and its value at the right, which on a wide
 * screen is a search problem rather than a reading one. Grouped columns keep a label beside its
 * value and use the width for more facts per screen instead of more gap.
 */
function nvmeGrid(n) {
  const groups = nvmeSections(n);
  return el('div', { class: 'v2-nvme' }, groups.map(([title, rows]) => el('div', { class: 'v2-nvme-group' }, [
    el('h3', { text: title }),
    el('dl', {}, rows.map(([key, label, value]) => {
      const help = nvmeHelp(key);
      return el('div', { class: 'v2-nvme-row' }, [
        el('dt', {}, [el('span', { text: label }), help ? helpButton(label, help) : null].filter(Boolean)),
        el('dd', { text: value }),
      ]);
    })),
  ])));
}

// --- shell bits -------------------------------------------------------------

function section(title, children, tools = null) {
  return el('section', { class: 'v2-section' }, [
    el('div', { class: 'v2-shead' }, [
      el('h2', { text: title }),
      tools ? el('div', { class: 'v2-aside' }, [tools]) : null,
    ].filter(Boolean)), ...children.filter(Boolean),
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
