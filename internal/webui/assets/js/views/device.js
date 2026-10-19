// Device detail (docs/UI.md §7.2).
import {
  el, mount, clear, fmtBytes, fmtDateTime, fmtAgo, fmtTemp, fmtHours, fmtInt, fmtScore, fmtCell, fmtMinutes,
  scoreGrade, convertTemp, convertTempDelta, temperatureUnitName,
} from '../dom.js';
import { api } from '../api.js';
import { verdictBadge, healthGauge, attributeTable, errorBox, openDialog, toast, transportLabel, trendRow, dateAxis, helpButton } from '../components.js';
import { nvmeHelp } from '../attributes.js';
import { icon } from '../icons.js';
import { selfTestCard } from './selftest.js';

// Trend windows. The history endpoint takes an absolute `from`, so the window is
// resolved at request time and "30 days" stays 30 days after a reload.
const TREND_RANGES = [
  ['7', '7 days'],
  ['30', '30 days'],
  ['90', '90 days'],
  ['all', 'All'],
];

// Module-level so the choice survives navigating between drives.
let trendRange = '30';

export async function render(container, ctx, serial, opts = {}) {
  // A range change re-renders the same page, and mounting the one-line skeleton collapses the
  // page height: the browser clamps the scroll position to the top, so changing the window
  // throws you out of the section you were reading. Keep what is on screen until the new data
  // arrives, and put the scroll back afterwards.
  const keepScroll = opts.keepScroll === true;
  const y = keepScroll ? currentScrollY() : 0;
  if (!keepScroll) {
    mount(container, el('div', { class: 'skeleton', style: 'width:40%;height:24px' }));
  }
  try {
    const [device, history] = await Promise.all([
      api.device(serial),
      api.history(serial, historyFrom()),
    ]);
    draw(container, ctx, device, history);
    if (keepScroll) restoreScrollY(y);
  } catch (err) {
    mount(container, errorBox(`Could not load device: ${err.message}`));
  }
}

/** The current scroll offset, or 0 where there is no window (the unit tests). */
function currentScrollY() {
  return typeof window === 'undefined' ? 0 : window.scrollY || 0;
}

function restoreScrollY(y) {
  if (typeof window !== 'undefined' && typeof window.scrollTo === 'function') {
    window.scrollTo(0, y);
  }
}

/** The absolute start of the selected window, or null for everything. */
function historyFrom() {
  if (trendRange === 'all') return null;
  const days = Number(trendRange);
  if (!Number.isFinite(days) || days <= 0) return null;
  return new Date(Date.now() - days * 86400000).toISOString();
}

/**
 * What the history says about where the drive is heading.
 *
 * Prose rather than a figure in the stat grid above it. A number in that column reads as
 * a measurement; this is an extrapolation from a handful of readings, and it says so —
 * the caveat is shown inline rather than hidden behind a tooltip, because a caveat nobody
 * reads is not a caveat.
 */
function projectionBlock(p) {
  if (!p || !p.summary) return null;
  return el('div', { class: 'projection' }, [
    el('div', { class: 'row' }, [
      el('span', { class: 'projection-label', text: 'Outlook' }),
      p.worsening
        ? el('span', { class: 'badge badge-caution', title: p.worsening },
            [icon('alert'), 'Worsening'])
        : null,
    ]),
    el('div', { text: p.summary }),
    p.confidence
      ? el('div', { class: 'muted projection-confidence', text: `${p.confidence} confidence` })
      : null,
    p.caveat ? el('p', { class: 'muted projection-caveat', text: p.caveat }) : null,
  ]);
}

function draw(container, ctx, data, history) {
  const series = (history && history.series) || [];
  const d = data.device;
  const projection = data.projection || null;
  const snapshot = data.snapshot || null;
  const payload = snapshot && snapshot.attributes ? snapshot.attributes : null;
  const attrs = payload ? payload.attributes || [] : [];
  const nvme = payload ? payload.nvme : null;

  const m = data.metrics || {};
  const powerOnHours = m.power_on_hours !== undefined
    ? m.power_on_hours
    : (snapshot ? snapshot.power_on_hours : null);
  const wearPct = snapshot ? snapshot.wear_pct : null;

  const identity = statGrid([
    // First, because it is the one line that answers "which drive do I pull?" — and the person
    // asking that question is standing in front of a machine, not reading a screen carefully.
    stat('Bay', d.bay),
    stat('Model', d.model),
    stat('Model family', d.model_family),
    stat('Serial', d.serial),
    stat('Firmware', d.firmware),
    stat('Capacity', fmtBytes(d.capacity_bytes)),
    stat('Interface', d.interface_speed),
    stat('Transport', transportLabel(d)),
    stat('Rotation', d.rotation ? `${fmtInt(d.rotation)} rpm` : 'solid state'),
    stat('Removable', yesNo(d.removable)),
    stat('WWN', d.wwn),
    stat('Device path', d.device_path),
  ], 'No identity information was reported for this drive.');

  const usage = statGrid([
    stat('Power-on hours', powerOnHours != null ? `${fmtHours(powerOnHours)} (${fmtInt(powerOnHours)} h)` : null),
    stat('Power cycles', m.power_cycles != null ? fmtInt(m.power_cycles) : null),
    stat('Temperature', fmtTemp(d.temperature_c)),
    stat('Data read', m.data_read_bytes != null ? fmtBytes(m.data_read_bytes) : null),
    stat('Data written', m.data_written_bytes != null ? fmtBytes(m.data_written_bytes) : null),
    stat('Life remaining', lifeText(m.life_left_pct, wearPct)),
    stat('Mountpoints', d.mountpoints && d.mountpoints.length ? d.mountpoints.join(', ') : 'not mounted'),
    // The filesystem version comes from lsblk and is often all there is to say about
    // a drive that reports no S.M.A.R.T.
    stat('Filesystem', [d.fstype, d.fs_version].filter(Boolean).join(' ')),
    stat('First seen', fmtDateTime(d.first_seen)),
    stat('Last scanned', d.last_scan ? `${fmtAgo(d.last_scan)} (${fmtDateTime(d.last_scan)})` : 'never'),
  ], 'This drive reports no usage or media figures.');

  mount(container,
    el('div', { class: 'page-head' }, [
      el('button', { class: 'btn btn-ghost btn-sm', type: 'button', title: 'Back to devices',
        onclick: () => ctx.navigate('/') }, [icon('back')]),
      el('div', { style: 'min-width:0' }, [
        el('h1', { text: d.name || d.model || d.device_path }),
        el('div', { class: 'sub', text: `${d.model || '—'}${d.device_path ? ` · ${d.device_path}` : ''}` }),
      ]),
      el('span', { class: 'spacer' }),
      el('button', { class: 'btn btn-sm', type: 'button', title: 'Rescan just this drive',
        onclick: () => ctx.scanDevice(d.serial) }, [icon('scan'), 'Scan']),
      el('button', { class: 'btn btn-sm', type: 'button', title: 'Export a report',
        onclick: () => exportDialog(d) }, [icon('download'), 'Export']),
      el('button', { class: 'btn btn-sm', type: 'button', onclick: () => editNotes(ctx, d) }, [icon('save'), 'Notes']),
      el('button', { class: 'btn btn-sm btn-ghost', type: 'button',
        title: 'Remove this drive, its history and its notes',
        onclick: () => ctx.forgetDevice(d.serial) }, [icon('trash'), 'Forget']),
    ]),

    el('div', { class: 'card stack' }, [
      el('div', { class: 'row' }, [
        healthGauge(d.score, 112),
        el('div', { class: 'stack', style: 'gap:var(--space-2);flex:1;min-width:0' }, [
          el('div', { class: 'row' }, [
            verdictBadge(d.health),
            el('span', { class: 'muted', text: `${fmtScore(d.score)}/100 · ${scoreGrade(d.score)}` }),
          ]),
          el('div', { text: d.health_rationale || '—' }),
        ]),
      ]),
      d.smart_available ? null : smartProblemNotice(ctx, d),
    ]),

    el('div', { class: 'panel-grid panel-grid-pair' }, [
      // A18 first: "what changed" is the news, and the cards below it are the reference material
      // you read afterwards.
      changesCard(d.changes),

      el('div', { class: 'card stack' }, [
        el('h2', { text: 'Identity' }),
        identity,
      ]),

      el('div', { class: 'card stack' }, [
        el('h2', { text: 'Usage and media' }),
        usage,
        projectionBlock(projection),
      ]),

      // Prose about the drive rather than a column of facts, so it spans the grid
      // instead of taking a cell.
      d.notes ? el('div', { class: 'card stack panel-wide' }, [
        el('h2', { text: 'Notes' }),
        el('p', { text: d.notes }),
      ]) : null,

      trendCard(series, async (next) => {
        trendRange = next;
        // Re-render in place: a window change is not a navigation, so it must not move the
        // page under the pointer.
        await render(container, ctx, d.serial, { keepScroll: true });
      }),

      el('div', { class: 'card stack' }, [
        el('div', { class: 'row' }, [
          el('h2', { text: nvme ? 'NVMe health log' : 'S.M.A.R.T. attributes' }),
          el('span', { class: 'spacer' }),
          el('button', { class: 'btn btn-sm btn-ghost', type: 'button',
            onclick: (e) => toggleRaw(d, e.currentTarget) }, ['Show raw output']),
        ]),
        nvme ? nvmeTable(nvme) : attributeTable(attrs),
        logSummary(d.logs),
        el('div', { id: 'raw-holder' }),
      ]),

      // A19. The only control in the app that changes a drive, so it sits with the log it writes
      // to rather than up in the header with the read-only actions.
      selfTestCard(ctx, d, d.logs),
    ]),
  );
}

/**
 * What changed since the last scan (`FEATURES.md` A18).
 *
 * "Nothing changed" is a real answer rather than an empty panel: a drive scanned twice and found
 * identical is exactly the reassurance someone opened the page for, so the card says so in words
 * instead of rendering nothing.
 *
 * The *server* decides which attribute movements are findings — the drive's own threshold, or one
 * of the lost-storage counters — and reports the rest as a count. That is deliberate: a second
 * opinion here would let this card and the verdict disagree about the same drive.
 */
export function changesCard(c) {
  if (!c) return null;

  const body = [];
  if (c.note) {
    body.push(el('p', { class: 'muted', text: c.note }));
  } else {
    const lines = [];
    if (c.health) lines.push(changeLine('Verdict', c.health, { badge: true }));
    if (c.score) lines.push(changeLine('Score', c.score, { format: (v) => `${fmtScore(v)}/100` }));
    if (c.temperature_c) lines.push(changeLine('Temperature', c.temperature_c, { format: fmtTemp }));
    if (c.wear_pct) lines.push(changeLine('Wear', c.wear_pct, { format: (v) => `${Math.round(v)}% used` }));
    if (c.sectors) lines.push(changeLine('Bad sectors', c.sectors, { format: fmtInt }));

    const attrs = Array.isArray(c.attributes) ? c.attributes : [];
    if (attrs.length) lines.push(attributeChanges(attrs));

    body.push(lines.length
      ? el('div', { class: 'change-list' }, lines)
      : el('p', { class: 'muted', text: 'Nothing changed. Every figure the drive reports is the ' +
          'same as it was at the previous scan.' }));

    if (c.routine) {
      body.push(el('p', { class: 'muted', text: `${c.routine === 1 ? '1 other counter' : `${fmtInt(c.routine)} other counters`} ` +
        'moved, which is ordinary wear rather than a finding. The attribute table below has the detail.' }));
    }
  }

  return el('div', { class: 'card stack panel-wide changes' }, [
    el('div', { class: 'row' }, [
      el('h2', { text: 'Since the last scan' }),
      el('span', { class: 'spacer' }),
      c.from ? el('span', { class: 'muted', text: `previous scan ${fmtAgo(c.from)}` }) : null,
    ]),
    ...body,
  ]);
}

/** One "was → now" line. An absent end is an em dash: not reported is not zero. */
function changeLine(label, change, opts = {}) {
  const show = (value) => {
    if (value === undefined || value === null) return '—';
    if (opts.badge) return verdictBadge(value);
    return opts.format ? opts.format(value) : String(value);
  };
  return el('div', { class: 'change-row' }, [
    el('span', { class: 'change-label', text: label }),
    el('span', { class: 'change-from' }, [show(change.from)]),
    el('span', { class: 'change-arrow', text: '→' }),
    el('span', { class: 'change-to' }, [show(change.to)]),
  ]);
}

/** The counters that moved *and* are a finding, worst first. */
function attributeChanges(attrs) {
  return el('div', { class: 'table-wrap' }, [
    el('table', { class: 'data data-log' }, [
      el('thead', {}, [el('tr', {}, [
        el('th', { class: 'num', text: '#' }),
        el('th', { text: 'Counter' }),
        el('th', { class: 'num', text: 'Was' }),
        el('th', { class: 'num', text: 'Now' }),
        el('th', { text: '' }),
      ])]),
      el('tbody', {}, attrs.map((a) => el('tr', {}, [
        el('td', { class: 'num', text: String(a.id) }),
        el('td', { text: a.name || `Attribute ${a.id}` }),
        el('td', { class: 'num', text: fmtInt(a.from) }),
        el('td', { class: 'num', text: fmtInt(a.to) }),
        el('td', {}, [
          a.appeared ? el('span', { class: 'badge badge-unknown', text: 'new' }) : null,
          a.threshold > 0 && a.value <= a.threshold
            ? el('span', { class: 'badge badge-caution', text: 'at threshold' })
            : null,
        ]),
      ]))),
    ]),
  ]);
}

/** "yes" / "no" for a tri-state flag; undefined (not reported) renders no row. */
function yesNo(value) {
  if (value === true) return 'yes';
  if (value === false) return 'no';
  return null;
}

// The `-d` values smartctl accepts for the USB bridges people actually hit.
const TRANSPORT_OVERRIDES = [
  'sat', 'usbjmicron', 'usbcypress', 'usbsunplus', 'usb-storage', 'scsi', 'nvme', 'auto',
];

/**
 * The two ways a container blocks a S.M.A.R.T. read, with the compose lines that fix
 * each. Both arrive as "Operation not permitted" from smartctl, which is why the
 * distinction is made server-side and only the wording lives here.
 */
const BLOCKED_FIXES = {
  device_blocked: {
    title: 'The container cannot open this device',
    body: 'The drive is visible, but this process is not allowed to open its device ' +
      'node, so no S.M.A.R.T. query can be made. In Docker that is the device cgroup: ' +
      'mounting /dev makes the nodes visible without permitting them to be opened.',
    fix: "device_cgroup_rules:\n  - 'a *:* rwm'",
  },
  query_blocked: {
    title: 'The S.M.A.R.T. query was refused',
    body: 'The device opened, but the query itself was denied by a missing capability. ' +
      'Which one depends on the drive: a native NVMe needs CAP_SYS_ADMIN, and a drive ' +
      'behind a USB bridge needs CAP_SYS_RAWIO for its passthrough.',
    fix: 'cap_add:\n  - SYS_ADMIN\n  - SYS_RAWIO',
  },
};

/**
 * Why this drive has no S.M.A.R.T. reading — and the two reasons are not the same thing.
 *
 * A drive that cannot report S.M.A.R.T. is not broken, and a container that is not
 * allowed to read one is not broken either, but only one of them is the drive's problem.
 * Showing "this drive does not report S.M.A.R.T." for a permissions failure sends the
 * reader looking at the hardware instead of at the compose file.
 */
function smartProblemNotice(ctx, d) {
  const blocked = BLOCKED_FIXES[d.smart_cause];
  if (blocked) {
    return el('div', { class: 'notice' }, [
      el('div', { class: 'notice-title' }, [icon('alert'), blocked.title]),
      el('p', { class: 'muted', text: blocked.body }),
      el('div', { class: 'notice-action' }, [
        el('p', { class: 'muted', text: 'Add this to the smart-serve service in your ' +
          'compose file, then recreate the container:' }),
        el('pre', { class: 'notice-fix', text: blocked.fix }),
        el('p', { class: 'muted', text: 'All three gates have to be open, and each one ' +
          'masks the next. See docs/DEPLOYMENT.md.' }),
      ]),
      d.smart_error ? el('p', { class: 'mono muted', text: d.smart_error }) : null,
    ]);
  }

  const transport = el('select', {}, [
    el('option', { value: '', text: 'Auto-detect' }),
    ...TRANSPORT_OVERRIDES.map((v) => el('option', { value: v, text: `-d ${v}` })),
  ]);
  transport.value = d.smart_d || '';

  return el('div', { class: 'notice' }, [
    el('div', { class: 'notice-title' }, [icon('help'), 'This drive does not report S.M.A.R.T.']),
    el('p', { class: 'muted', text: 'Most USB flash drives and card readers do not implement it, ' +
      'and some USB bridges do not pass it through. There is no health reading to give, but the ' +
      'identity, capacity and filesystem above come from the operating system, not from the drive.' }),
    el('div', { class: 'notice-action' }, [
      el('p', { class: 'muted', text: 'If this is a hard drive or SSD inside a USB enclosure, the ' +
        'bridge may just need the right transport. Pick one and rescan to find out.' }),
      el('div', { class: 'row' }, [
        transport,
        el('button', { class: 'btn btn-sm', type: 'button', text: 'Apply & rescan',
          onclick: async (event) => {
            event.currentTarget.disabled = true;
            try {
              await api.setTransport(d.serial, transport.value);
              await api.scanDevice(d.serial);
              toast(transport.value ? `Rescanned with -d ${transport.value}` : 'Transport override cleared', 'good');
              ctx.refresh();
            } catch (err) {
              toast(err.message, 'error');
            } finally {
              event.currentTarget.disabled = false;
            }
          } }),
      ]),
    ]),
    d.smart_error ? el('p', { class: 'mono muted', text: d.smart_error }) : null,
  ]);
}

function stat(label, value) {
  if (value === null || value === undefined) return null;
  const text = String(value).trim();
  if (text === '' || text === '—') return null;
  return el('div', {}, [
    el('span', { class: 'stat-label', text: label }),
    el('span', { class: 'stat-value', text, title: text }),
  ]);
}

/**
 * A details grid. Rows the drive does not report are dropped rather than shown as a
 * bare em dash: a row whose only content is "—" tells you nothing except that the
 * field exists, and a screenful of them reads as broken rather than sparse.
 */
function statGrid(rows, emptyNote) {
  const shown = rows.filter(Boolean);
  if (!shown.length) return el('p', { class: 'muted', text: emptyNote });
  return el('div', { class: 'form-grid' }, shown);
}

/** "96% remaining (4% used)" — prefers the drive's own life figure. */
function lifeText(lifeLeft, wearPct) {
  if (lifeLeft != null) return `${lifeLeft}% remaining`;
  if (wearPct != null) {
    const used = Math.round(wearPct);
    return `${100 - used}% remaining (${used}% used)`;
  }
  return null;
}

// --- trends -------------------------------------------------------------------

// Each metric knows how to render itself and whether a change means anything. A
// rising power-on-hour count is not a health signal — it is just the drive being
// used — so that row reports a rate rather than a coloured delta.
const TREND_METRICS = [
  {
    key: 'score',
    label: 'Health score',
    band: true,
    format: (v) => `${Math.round(v)}/100`,
    delta: (d) => ({ text: signed(d), tone: d > 0 ? 'good' : 'bad' }),
  },
  {
    key: 'temperature_c',
    label: 'Temperature',
    format: (v) => `${convertTemp(v)} °${temperatureUnitName()}`,
    // A delta is a span, so it converts without the offset.
    delta: (d) => ({
      text: signed(convertTempDelta(d), ` °${temperatureUnitName()}`),
      tone: d > 0 ? 'bad' : 'good',
    }),
  },
  {
    key: 'power_on_hours',
    label: 'Power-on hours',
    format: (v) => `${Math.round(v)} h`,
    delta: (_delta, points) => ({ text: usageRate(points), tone: 'neutral' }),
  },
  {
    key: 'wear_pct',
    label: 'Wear',
    format: (v) => `${Math.round(v)}% used`,
    delta: (d) => ({ text: signed(d, '%'), tone: d > 0 ? 'bad' : 'good' }),
  },
];

function trendCard(series, onRange) {
  const selector = el('div', { class: 'row', style: 'gap:var(--space-1)' },
    TREND_RANGES.map(([value, label]) =>
      el('button', {
        class: 'health-item',
        type: 'button',
        'aria-pressed': trendRange === value ? 'true' : 'false',
        onclick: () => onRange(value),
      }, [el('span', { class: 'health-label', text: label })])));

  // One window for every row in the card, and one axis under them all.
  const window = trendWindow(series);
  const rows = TREND_METRICS
    .map((metric) => trendRow(metric, seriesFor(metric.key, series), { window }))
    .filter(Boolean);

  return el('div', { class: 'card stack' }, [
    el('div', { class: 'row' }, [
      el('h2', { text: 'Trend' }),
      el('span', { class: 'spacer' }),
      selector,
    ]),
    rows.length
      ? el('div', {}, [rows, dateAxis(window)])
      : el('p', { class: 'muted', text: series.length
        ? 'Not enough readings in this window yet. A trend needs at least two scans.'
        : 'No history yet. Trends appear once this drive has been scanned a few times.' }),
  ]);
}

/**
 * The span every sparkline in the card is drawn against (`ADR-023`).
 *
 * A fixed range is a *calendar* window: 30 days means the last 30 days whether or not anything was
 * recorded in them, so a drive that stopped being scanned leaves a visible gap rather than a line
 * that quietly ends at the right edge. "All" has no calendar to anchor to, so it spans exactly the
 * history that exists.
 */
function trendWindow(series) {
  const stamps = series.map((s) => new Date(s.ts).getTime()).filter(Number.isFinite);
  const now = Date.now();

  if (trendRange === 'all') {
    // With no history at all there is no span to draw, and an axis needs two ends.
    if (!stamps.length) return { from: now - 86400000, to: now };
    return { from: Math.min(...stamps), to: Math.max(...stamps) };
  }

  const days = Number(trendRange);
  const span = Number.isFinite(days) && days > 0 ? days : 1;
  return { from: now - span * 86400000, to: now };
}

/** One metric's points, dropping snapshots where the drive reported nothing. */
function seriesFor(key, series) {
  return series
    .map((snap) => ({ ts: snap.ts, value: snap[key] }))
    .filter((p) => typeof p.value === 'number' && Number.isFinite(p.value));
}

/** "+3" / "-2 °C" — always signed, so the direction is never ambiguous. */
function signed(delta, unit = '') {
  const rounded = Math.round(delta * 10) / 10;
  return `${rounded > 0 ? '+' : ''}${rounded}${unit}`;
}

/** Hours per day across the window: the useful reading of a rising counter. */
function usageRate(points) {
  const hours = points[points.length - 1].value - points[0].value;
  const days = (new Date(points[points.length - 1].ts) - new Date(points[0].ts)) / 86400000;
  if (!(days > 0) || hours <= 0) return 'no change';
  return `+${(hours / days).toFixed(1)} h/day`;
}

// --- export -------------------------------------------------------------------

// Exports a report in any of the formats the API renders. A single "Report" link
// silently picked one of four, which is a choice the user should be making.
const EXPORT_FORMATS = [
  ['txt', 'Plain text', 'CrystalDiskInfo-style summary, and the format to attach to a warranty claim.'],
  ['html', 'Web page', 'A self-contained page you can print or email. Opens without the app.'],
  ['csv', 'Spreadsheet', 'Every field and attribute as name/value rows.'],
  ['json', 'JSON', 'The full structure, for scripting.'],
];

function exportDialog(device) {
  openDialog('Export report', (body, close, foot) => {
    body.append(el('p', { class: 'muted',
      text: `Download ${device.name || device.model || device.serial} as:` }));
    body.append(el('div', { class: 'export-list' },
      EXPORT_FORMATS.map(([format, label, help]) =>
        el('a', {
          class: 'export-option',
          href: api.reportUrl(device.serial, format),
          download: '',
          // Close after the click has been handled: removing the anchor
          // synchronously can cancel the download in some browsers.
          onclick: () => setTimeout(close, 0),
        }, [
          el('span', { class: 'export-label', text: label }),
          el('span', { class: 'export-help', text: help }),
          icon('download'),
        ]))));
    foot.append(el('button', { class: 'btn', type: 'button', text: 'Close', onclick: close }));
  });
}

// Exported so the grouping can be pinned by a test: the structure is the fix for the
// density, and a refactor back to a flat list would silently undo it.
export function nvmeTable(n) {
  // Grouped rather than one flat list of sixteen rows. The log mixes counters that
  // describe the drive's health, its lifetime, its workload and its errors, and in a flat
  // list all four read alike — which is what made this the densest panel on the page.
  // Each row carries the API field name alongside its label: the label is for reading,
  // the field name is the key into the help table (`attributes.js`), so rewording a label
  // cannot silently detach its explanation.
  const sections = [
    ['Health', [
      ['critical_warning', 'Critical warning', fmtInt(n.critical_warning)],
      ['available_spare', 'Available spare', percent(n.available_spare)],
      ['available_spare_threshold', 'Spare threshold', percent(n.available_spare_threshold)],
      ['percentage_used', 'Percentage used', percent(n.percentage_used)],
    ]],
    ['Lifetime', [
      ['power_on_hours', 'Power-on hours', fmtInt(n.power_on_hours)],
      ['power_cycles', 'Power cycles', fmtInt(n.power_cycles)],
      ['unsafe_shutdowns', 'Unsafe shutdowns', fmtInt(n.unsafe_shutdowns)],
      ['controller_busy_time', 'Controller busy time', fmtMinutes(n.controller_busy_time)],
      ['warning_temp_time', 'Warning temp time', fmtMinutes(n.warning_temp_time)],
      ['critical_comp_time', 'Critical comp time', fmtMinutes(n.critical_comp_time)],
    ]],
    ['Workload', [
      ['data_units_read', 'Data units read', dataUnits(n.data_units_read)],
      ['data_units_written', 'Data units written', dataUnits(n.data_units_written)],
      ['host_reads', 'Host read commands', fmtInt(n.host_reads)],
      ['host_writes', 'Host write commands', fmtInt(n.host_writes)],
    ]],
    ['Errors', [
      ['media_errors', 'Media errors', fmtInt(n.media_errors)],
      ['num_err_log_entries', 'Error log entries', fmtInt(n.num_err_log_entries)],
    ]],
  ];

  const body = [];
  for (const [title, rows] of sections) {
    body.push(el('tr', { class: 'section' }, [
      el('th', { colspan: '2', text: title }),
    ]));
    for (const [key, label, value] of rows) {
      const help = nvmeHelp(key);
      body.push(el('tr', {}, [
        el('td', {}, [
          el('span', { text: label }),
          help ? helpButton(label, help) : null,
        ]),
        el('td', { class: 'num', text: fmtCell(value) }),
      ]));
    }
  }

  return el('div', { class: 'table-wrap' }, [
    el('table', { class: 'data data-log' }, [
      el('thead', {}, [el('tr', {}, [el('th', { text: 'Field' }), el('th', { class: 'num', text: 'Value' })])]),
      el('tbody', {}, body),
    ]),
  ]);
}

/**
 * What the drive's own logs hold (`FEATURES.md` A20).
 *
 * The counts, not the entries — and that limit is deliberate. Neither drive available to
 * this project has ever logged an error, and smartctl emits no entry list at all when a log
 * is empty, so the shape of a populated one has never been observed. A panel that rendered
 * nothing when a drive *does* have something to say would be worse than one that admits the
 * limit and points at the raw output, which holds the whole document verbatim.
 *
 * A count of zero is worth showing: "this drive has never complained" is the answer to the
 * question that sent someone looking, and leaving it out would leave them wondering.
 */
export function logSummary(logs) {
  if (!logs) return null;

  const rows = [];
  if (logs.error_count !== undefined) {
    rows.push(['Error log', logged(logs.error_count, 'entry', 'entries')]);
  }
  if (logs.self_test_count !== undefined) {
    rows.push(['Self-test log', logged(logs.self_test_count, 'result', 'results')]);
  }
  if (logs.self_test_supported === false) {
    rows.push(['Self-test', 'not supported by this drive']);
  }
  const timings = [];
  if (logs.short_minutes !== undefined) timings.push(`short ≈ ${fmtInt(logs.short_minutes)} min`);
  if (logs.extended_minutes !== undefined) timings.push(`extended ≈ ${fmtInt(logs.extended_minutes)} min`);
  if (timings.length) rows.push(['Self-test timing', timings.join(' · ')]);

  const errorEntries = Array.isArray(logs.error_entries) ? logs.error_entries : [];
  const selfTestEntries = Array.isArray(logs.self_test_entries) ? logs.self_test_entries : [];
  // An error entry that says nothing but *which* entry it is gets a sentence rather than a
  // table: a table of nothing but numbers adds nothing the count has not already said.
  const detailed = errorEntries.filter(
    (e) => e.hours !== undefined || e.lba !== undefined || (e.status || '') !== '',
  );

  if (!rows.length && !logs.note && !selfTestEntries.length && !errorEntries.length) return null;

  return el('div', { class: 'smart-logs' }, [
    el('h3', { text: 'Drive logs' }),
    el('div', { class: 'table-wrap' }, [
      el('table', { class: 'data data-log' }, [
        el('tbody', {}, rows.map(([k, v]) => el('tr', {}, [
          el('td', { text: k }),
          el('td', { class: 'num', text: v }),
        ]))),
      ]),
    ]),
    selfTestEntries.length ? selfTestTable(selfTestEntries) : null,
    detailed.length
      ? errorTable(detailed)
      : (errorEntries.length
        ? el('p', { class: 'muted', text: 'The drive records that an error happened but reports '
            + 'nothing about it beyond which entry it is.' })
        : null),
    // smartctl's own words, quoted rather than paraphrased: when a log could not be read,
    // its message is usually the whole explanation.
    logs.note ? el('p', { class: 'muted', text: `smartctl: ${logs.note}` }) : null,
    el('p', { class: 'muted', text: 'The full smartctl document is in the raw output.' }),
  ]);
}

/** The self-test log: what was run, how it ended, and when. */
function selfTestTable(entries) {
  return el('div', { class: 'table-wrap' }, [
    el('table', { class: 'data data-log' }, [
      el('thead', {}, [el('tr', {}, [
        el('th', { text: 'Self-test' }),
        el('th', { text: 'Result' }),
        el('th', { class: 'num', text: 'At' }),
      ])]),
      el('tbody', {}, entries.map((e) => el('tr', {}, [
        el('td', { text: e.type || '—' }),
        el('td', {}, [selfTestResult(e)]),
        el('td', { class: 'num', text: e.hours === undefined ? '—' : `${fmtInt(e.hours)} h` }),
      ]))),
    ]),
  ]);
}

/**
 * One self-test result.
 *
 * Three cases, not a boolean. A test in progress has a percentage and no verdict, and an absent
 * verdict is not a failure — rendering it as one would be the worst possible reading of the log.
 */
function selfTestResult(e) {
  if (e.remaining !== undefined) {
    return el('span', {}, [icon('scan'), `In progress, ${e.remaining}% left`]);
  }
  if (e.passed === true) {
    return el('span', { class: 'log-pass' }, [icon('check'), e.status || 'Passed']);
  }
  if (e.passed === false) {
    return el('span', { class: 'log-fail' }, [icon('x'), e.status || 'Failed']);
  }
  return el('span', { class: 'muted', text: e.status || '—' });
}

/** The error log, for the drives that report more than which entry it was. */
function errorTable(entries) {
  return el('div', { class: 'table-wrap' }, [
    el('table', { class: 'data data-log' }, [
      el('thead', {}, [el('tr', {}, [
        el('th', { class: 'num', text: '#' }),
        el('th', { text: 'Detail' }),
        el('th', { class: 'num', text: 'At' }),
      ])]),
      el('tbody', {}, entries.map((e) => el('tr', {}, [
        el('td', { class: 'num', text: fmtCell(e.number) }),
        el('td', { text: e.status || (e.lba === undefined ? '—' : `LBA ${fmtInt(e.lba)}`) }),
        el('td', { class: 'num', text: e.hours === undefined ? '—' : `${fmtInt(e.hours)} h` }),
      ]))),
    ]),
  ]);
}

/** "none logged", or "3 entries logged" — a count of zero is a finding, not an absence. */
function logged(n, singular, plural) {
  if (n === 0) return 'none logged';
  return `${fmtInt(n)} ${n === 1 ? singular : plural} logged`;
}

// NVMe reports volume in "data units" of 1,000 × 512-byte sectors, so the raw
// counter means nothing on its own. Show the count and the humanised size.
function dataUnits(count) {
  if (count === null || count === undefined) return '—';
  return `${fmtInt(count)} (${fmtBytes(Number(count) * 512000)})`;
}

// A percentage that may be absent. Interpolating straight into a template rendered a
// missing field as "undefined%", which is worse than admitting the drive did not say.
function percent(value) {
  return value === null || value === undefined ? '—' : `${value}%`;
}

async function toggleRaw(device, button) {
  const holder = document.getElementById('raw-holder');
  if (!holder) return;
  if (holder.dataset.open === 'true') {
    clear(holder);
    holder.dataset.open = 'false';
    button.textContent = 'Show raw output';
    return;
  }
  button.disabled = true;
  try {
    const raw = await api.raw(device.serial);
    let pretty = raw;
    try {
      pretty = JSON.stringify(JSON.parse(raw), null, 2);
    } catch {
      /* leave the raw text as-is */
    }
    holder.append(el('pre', {
      class: 'mono',
      style: 'max-height:320px;overflow:auto;background:var(--surface-2);padding:var(--space-3);border-radius:var(--radius-sm)',
    }, [pretty]));
    holder.dataset.open = 'true';
    button.textContent = 'Hide raw output';
  } catch (err) {
    toast(err.message, 'error');
  } finally {
    button.disabled = false;
  }
}

function editNotes(ctx, device) {
  openDialog('Device notes', (body, close, foot) => {
    const name = el('input', { type: 'text', value: device.name || '', placeholder: 'e.g. boot, backup pool' });
    const bay = el('input', { type: 'text', value: device.bay || '', placeholder: 'e.g. bay 4, front top, USB port 2' });
    const tags = el('input', { type: 'text', value: device.tags || '', placeholder: 'e.g. critical,os' });
    const notes = el('textarea', { value: device.notes || '', placeholder: 'Anything worth remembering about this drive' });
    const hidden = el('input', { type: 'checkbox', checked: Boolean(device.hidden) });

    body.append(
      el('div', { class: 'field' }, [el('label', { text: 'Name' }), name]),
      el('div', { class: 'field' }, [el('label', { text: 'Bay' }), bay,
        el('p', { class: 'muted field-hint', text: 'Where the drive physically is. A serial ' +
          'identifies a drive; a bay finds it, which is the question at 3am.' })]),
      el('div', { class: 'field' }, [el('label', { text: 'Tags' }), tags]),
      el('div', { class: 'field' }, [el('label', { text: 'Notes' }), notes]),
      el('label', { class: 'field-row' }, [hidden, el('span', { text: 'Hide from the dashboard' })]),
    );

    foot.append(
      el('button', { class: 'btn', type: 'button', text: 'Cancel', onclick: close }),
      el('button', {
        class: 'btn btn-primary',
        type: 'button',
        text: 'Save',
        onclick: async (event) => {
          event.currentTarget.disabled = true;
          try {
            await api.setNotes(device.serial, {
              name: name.value.trim(),
              bay: bay.value.trim(),
              tags: tags.value.trim(),
              notes: notes.value,
              hidden: hidden.checked,
            });
            toast('Notes saved', 'good');
            close();
            ctx.refresh();
          } catch (err) {
            toast(err.message, 'error');
            event.currentTarget.disabled = false;
          }
        },
      }),
    );
  });
}

