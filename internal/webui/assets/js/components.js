// Shared UI components (docs/UI.md §6).
import { el, svg, fmtBytes, fmtTemp, fmtScore, fmtAgo, fmtDate, fmtTime, fmtCell, fmtHours, append, scoreGrade, scoreBand, verdictClass } from './dom.js';
import { icon } from './icons.js';
import { attributeHelp, attributeFlagged } from './attributes.js';

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
/**
 * A reading's timestamp in milliseconds, or null when it has none.
 *
 * The API sends ISO strings; a caller may already hold milliseconds. Normalising in one place means
 * the two cannot disagree about which they are holding.
 */
function timeOf(ts) {
  const n = typeof ts === 'number' ? ts : new Date(ts).getTime();
  return Number.isFinite(n) ? n : null;
}

/** Readings that can be plotted: a real timestamp and a finite value. */
function plottable(points) {
  return (points || [])
    .map((p) => ({
      ts: p.ts,
      at: timeOf(p.ts),
      value: typeof p.value === 'number' ? p.value : Number.NaN,
    }))
    .filter((p) => Number.isFinite(p.at) && Number.isFinite(p.value));
}

/** A series' own extent, for a caller that has no window to draw it against. */
function extentOf(points) {
  const at = points.map((p) => p.at);
  return { from: Math.min(...at), to: Math.max(...at) };
}

/**
 * The x scale for a window, in viewBox units, clamped to the box.
 *
 * One function, used for both the path and the hover marker, so the line and the readout can never
 * disagree about where a reading sits — which they could when the marker was positioned by
 * percentage and the path by index.
 */
function xScale(bounds) {
  const span = bounds.to - bounds.from || 1;
  return (at) => Math.max(0, Math.min(SPARK_WIDTH, ((at - bounds.from) / span) * SPARK_WIDTH));
}

/**
 * The SVG path for a series, positioned by **time** rather than by reading index.
 *
 * That single change is the whole trend rework (`ADR-023`). By index, a 30-day window was drawn
 * edge to edge whether it held two readings or two hundred, a series that began five days ago
 * started at the left edge, and a gap in the data was drawn as a straight line. By time the window
 * *is* the window: gaps are gaps, and a short series sits in the corner it belongs in.
 *
 * `window` is the shared span every sparkline in the card is drawn against. Omitted, a series is
 * drawn across its own extent — which is what a standalone caller wants, and what makes this
 * testable without a card around it.
 */
export function sparkLine(points, height = 40, window = null) {
  const usable = plottable(points);
  if (usable.length < 2) return null;

  const bounds = window || extentOf(usable);
  const x = xScale(bounds);

  const values = usable.map((p) => p.value);
  const min = Math.min(...values);
  const max = Math.max(...values);
  // A perfectly flat series has no range; treating it as 1 renders a straight line
  // instead of dividing by zero into a row of NaN coordinates.
  const spanY = max - min || 1;
  const y = (v) => height - ((v - min) / spanY) * (height - 6) - 3;

  return usable
    .map((p, i) => `${i === 0 ? 'M' : 'L'}${x(p.at).toFixed(1)} ${y(p.value).toFixed(1)}`)
    .join(' ');
}

/**
 * Inline SVG sparkline with a hover readout.
 *
 * Deliberately minimal — no axes, no labels — because the point is the shape. Hovering
 * (or arrowing with the keyboard) gives the exact figure for one reading without
 * putting numbers on the line.
 *
 * `points` are `{ts, value}` in time order; `format(value)` renders the readout.
 */
export function sparkline(points, opts = {}) {
  const { height = 40, band = null, format = (v) => String(v), window = null } = opts;
  const usable = plottable(points);
  if (usable.length < 2) return null;

  const bounds = window || extentOf(usable);
  const x = xScale(bounds);
  const line = sparkLine(usable, height, bounds);
  const stroke = band ? `spark-${band}` : 'spark';

  const marker = el('div', { class: 'spark-marker', hidden: true });
  // Three short lines rather than one long one: a single line wide enough to hold a
  // full timestamp runs most of the way across the card.
  const readoutValue = el('div', { class: 'spark-readout-value' });
  const readoutDate = el('div', { class: 'spark-readout-date' });
  const readoutTime = el('div', { class: 'spark-readout-time' });
  const readout = el('div', { class: 'spark-readout', hidden: true },
    [readoutValue, readoutDate, readoutTime]);
  const plot = el('div', { class: 'spark-plot', tabindex: '0', role: 'img',
    'aria-label': `${usable.length} readings, ${format(usable[0].value)} to ${format(usable[usable.length - 1].value)}` }, [
    svg(
      `<svg class="sparkline" viewBox="0 0 ${SPARK_WIDTH} ${height}" preserveAspectRatio="none" aria-hidden="true">` +
        // Where the shared date axis's labels fall, drawn through every plot in the card. One axis
        // at the bottom of the card is cheap, but a date on its own row is hard to carry up to a
        // particular line; the same three positions in each plot are what make it readable.
        axisGuides(height) +
        // Closed at the ends of the series rather than at the ends of the box: with a time axis the
        // line does not reach both edges, and an area stretched to the box would paint data
        // across a window the drive said nothing about.
        `<path class="spark-area" d="${line} L${x(usable[usable.length - 1].at).toFixed(1)} ${height}` +
          ` L${x(usable[0].at).toFixed(1)} ${height} Z"/>` +
        `<path class="${stroke}" d="${line}" vector-effect="non-scaling-stroke"/>` +
        `</svg>`,
    ),
    marker,
    readout,
  ]);

  const show = (index) => {
    const point = usable[index];
    // Positioned by time, on the same scale the path uses. By index, a reading five days into a
    // 30-day window was marked at the left edge, under the wrong end of the axis.
    const percent = (x(point.at) / SPARK_WIDTH) * 100;
    marker.hidden = false;
    marker.style.left = `${percent}%`;
    readout.hidden = false;
    readout.style.left = `${percent}%`;
    // Anchored at the ends rather than centred, so the readout never runs off the card.
    readout.dataset.edge = index === 0 ? 'start' : index === usable.length - 1 ? 'end' : 'mid';
    readoutValue.textContent = format(point.value);
    readoutDate.textContent = fmtDate(point.ts);
    readoutTime.textContent = fmtTime(point.ts);
  };

  const hide = () => {
    marker.hidden = true;
    readout.hidden = true;
  };

  // The pointer maps to a *time*, and the readout is the reading nearest to it — not to an index.
  // That is what makes a sparse series hover honestly: the gaps are real, and pointing into one
  // should give the reading you are closest to rather than one a fraction of the way through the
  // array.
  const indexFromX = (clientX) => {
    const box = plot.getBoundingClientRect();
    if (!box.width) return usable.length - 1;
    const ratio = Math.max(0, Math.min(1, (clientX - box.left) / box.width));
    const at = bounds.from + ratio * (bounds.to - bounds.from);
    let best = 0;
    let gap = Infinity;
    usable.forEach((p, i) => {
      const distance = Math.abs(p.at - at);
      if (distance < gap) {
        gap = distance;
        best = i;
      }
    });
    return best;
  };

  let cursor = usable.length - 1;
  plot.addEventListener('mousemove', (event) => show(indexFromX(event.clientX)));
  plot.addEventListener('mouseleave', hide);
  plot.addEventListener('focus', () => show(cursor));
  plot.addEventListener('blur', hide);
  plot.addEventListener('keydown', (event) => {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
    event.preventDefault();
    cursor = Math.max(0, Math.min(usable.length - 1, cursor + (event.key === 'ArrowRight' ? 1 : -1)));
    show(cursor);
  });

  return plot;
}

// The viewBox width is fixed and the element is stretched to its container, so a
// sparkline fills whatever column it lands in. Exported because anything positioning
// against a sparkline — a test, or a future overlay — needs the same coordinate space.
export const SPARK_WIDTH = 240;

/**
 * How many labels the shared date axis carries.
 *
 * Exported because the axis and the rules drawn through each plot must agree about where the ticks
 * are: two constants that drifted would put a date under the wrong part of a line, which is worse
 * than having no axis at all.
 */
export const AXIS_TICKS = 3;

/** Vertical rules at the positions the shared date axis labels, for one plot. */
function axisGuides(height) {
  const lines = [];
  for (let i = 0; i < AXIS_TICKS; i += 1) {
    const at = (SPARK_WIDTH * i) / (AXIS_TICKS - 1);
    // Half a unit in from the ends: a stroke centred on the viewBox boundary is half clipped.
    const x1 = Math.max(0.5, Math.min(SPARK_WIDTH - 0.5, at));
    lines.push(`<line class="spark-guide" x1="${x1}" y1="0" x2="${x1}" y2="${height}"` +
      ' vector-effect="non-scaling-stroke"/>');
  }
  return lines.join('');
}

/**
 * A labelled trend: current value, the change across the series, and the shape.
 *
 * Rows with fewer than two readings are dropped — a single point is not a trend.
 *
 * `window` is the shared span from the card. It is passed to the sparkline rather than derived
 * here, so every row in the card is drawn against the same axis.
 */
export function trendRow(metric, points, opts = {}) {
  const { window = null } = opts;
  const usable = plottable(points);
  if (usable.length < 2) return null;

  const last = usable[usable.length - 1].value;
  // Measured across the *series*, not across the window: a drive watched for five days of a 30-day
  // window has five days of change, and reporting 30 days of it would invent a trend.
  const change = metric.delta ? metric.delta(last - usable[0].value, usable) : null;

  return el('div', { class: 'trend-row' }, [
    el('div', { class: 'trend-head' }, [
      el('span', { class: 'setting-label', text: metric.label }),
      el('span', { class: 'spacer' }),
      el('span', { class: 'trend-value', text: metric.format(last) }),
      change ? el('span', { class: `trend-delta ${change.tone}`, text: change.text }) : null,
    ]),
    sparkline(usable, { band: metric.band ? scoreBand(last) : null, format: metric.format, window }),
  ]);
}

/**
 * One date axis for the whole card (`ADR-023`).
 *
 * Shared rather than one per sparkline: four sparklines in one window need one axis, and four
 * would be noise. It exists so the span is readable *without hovering*.
 */
export function dateAxis(window, ticks = AXIS_TICKS) {
  const { from, to } = window || {};
  if (!Number.isFinite(from) || !Number.isFinite(to) || to <= from) return null;

  const marks = [];
  for (let i = 0; i < ticks; i += 1) {
    const at = from + ((to - from) * i) / (ticks - 1);
    marks.push(el('span', { class: 'axis-tick', text: fmtDate(new Date(at).toISOString()) }));
  }
  // Decorative: every figure it carries is also in a sparkline's hover readout, and a screen
  // reader announcing three bare dates between four charts is noise rather than information.
  return el('div', { class: 'date-axis', 'aria-hidden': 'true' }, marks);
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

/**
 * Causes that mean the read was *refused* rather than the drive having nothing to say.
 * The server names them (internal/smart/failure.go) because smartctl reports both as the
 * same "Operation not permitted".
 */
export const BLOCKED_CAUSES = new Set(['device_blocked', 'query_blocked']);

/**
 * The early-warning pill: bad sectors are accumulating.
 *
 * Kept separate from the verdict badge because the two answer different questions. The
 * verdict says how the drive looks *now*; this says which way it is going. A drive can be
 * green today and still be the one worth replacing first.
 */
/**
 * A badge for a drive testing itself (`FEATURES.md` A20), or null when none is.
 *
 * The percentage is the drive's own, and it is absent on many drives — they write a self-test log
 * entry only when a test *finishes*, so there is nothing to read while one runs. "Testing" without a
 * figure is the honest answer there, and it is still the answer to the question the badge exists for:
 * which drive is busy.
 *
 * Tone is neutral rather than `good`: a test in progress is neither a fault nor a clean bill of
 * health, and colouring it green would put a reassuring mark on a drive that has not reported yet.
 */
function selfTestBadge(device) {
  const state = device && device.selftest;
  if (!state) return null;
  const kind = String(state.kind || '').trim();
  const pct = typeof state.pct === 'number' ? state.pct : null;
  const label = pct === null ? 'Testing' : `Testing ${pct}%`;
  return el('span', {
    class: 'badge badge-unknown selftest-badge',
    title: `${kind || 'A self-test'} is running on this drive` +
      (pct === null ? ', which does not report progress' : `, ${pct}% done`),
  }, [icon('scan'), label]);
}

function worseningBadge(device) {
  if (!device.worsening) return null;
  return el('span', {
    class: 'badge badge-caution',
    title: `${device.worsening} new sector problems since the start of the window. ` +
      'Sector counts arrive in bursts, so this is a direction rather than a date.',
  }, [icon('alert'), 'Worsening']);
}

/**
 * The status pill for a device card.
 *
 * Four different situations, and conflating them would mislead: the drive is gone, the
 * drive is present but we were not allowed to read it, the drive cannot report S.M.A.R.T.
 * at all, or the drive reported a verdict. "Unknown" is right for none of the first three.
 */
function statusBadge(device) {
  if (device.is_missing) {
    return el('span', { class: 'badge badge-unknown' }, [icon('help'), 'Not connected']);
  }
  if (device.smart_available === false) {
    if (BLOCKED_CAUSES.has(device.smart_cause)) {
      return el('span', {
        class: 'badge badge-caution',
        title: device.smart_error || 'S.M.A.R.T. reads are blocked',
      }, [icon('alert'), 'Blocked']);
    }
    return el('span', {
      class: 'badge badge-unknown',
      title: 'This drive does not report S.M.A.R.T. Most USB flash drives cannot.',
    }, [icon('help'), 'No S.M.A.R.T.']);
  }
  return verdictBadge(device.health);
}

/**
 * What to call a drive on screen: its user name, falling back to what the drive says about itself.
 *
 * One function rather than the same expression in three places, because "the same drive has three
 * different names on three screens" is the kind of thing nobody notices until a screenshot.
 */
export function deviceTitle(device) {
  return device.name || device.model || device.device_path || device.serial || '';
}

export function deviceCard(device, onOpen, onScan) {
  const title = deviceTitle(device);
  const mounted = device.mountpoints && device.mountpoints.length
    ? device.mountpoints.join(', ')
    : 'not mounted';

  // Text labels rather than bare icons: drive type and capacity are otherwise
  // ambiguous, and icon sizes vary enough to look inconsistent (docs/UI.md §6).
  //
  // The bay leads when it is set, because it is the one field that answers "which drive do I
  // pull?" — a card that makes you match serials against a spreadsheet has failed at the only
  // question someone standing in front of the machine has. Omitted entirely when unset: a missing
  // bay is a gap in the bookkeeping rather than a finding about the drive, and a row of dashes
  // would read as the latter.
  const rows = [
    device.bay ? ['Bay', device.bay] : null,
    ['Transport', transportLabel(device)],
    ['Capacity', fmtBytes(device.capacity_bytes)],
    ['Temperature', fmtTemp(device.temperature_c)],
    ['Mounted', mounted],
    ['Last scan', fmtAgo(device.last_scan)],
  ].filter(Boolean);

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
      // A test running right now (`FEATURES.md` A20). On the card because the reported problem was
      // exactly this: start a long test, look at another drive, come back, and the fleet gave no sign
      // of which one was busy — so the app got asked to start a second test it would refuse.
      selfTestBadge(device),
      worseningBadge(device),
      statusBadge(device),
    ]),
    el('div', { class: 'device-card-body' }, [
      // A drive that cannot report S.M.A.R.T. has no score to draw, so the ring is
      // omitted rather than shown permanently empty.
      device.smart_available === false ? null : healthGauge(device.score, 88),
      el('div', { class: 'kv' }, rows.flatMap(([label, value]) => [
        el('span', { class: 'kv-label', text: label }),
        el('span', { class: 'kv-value', text: value, title: value }),
      ])),
    ]),
    // A28. Under the facts and above the rationale: "how full is this drive?" is a question about the
    // drive as a whole, and the fleet is where somebody scans for the one that is filling up. The
    // mountpoint rides along because a bare percentage does not say *what* is full.
    device.storage
      ? el('div', { class: 'device-card-storage' }, [
          usageBar(device.storage.used_pct, {
            label: device.storage.mount || '',
            title: device.storage.total_bytes
              ? `${fmtBytes(device.storage.used_bytes)} of ` +
                `${fmtBytes(device.storage.total_bytes)} used`
              : `${device.storage.used_pct}% used`,
          }),
        ])
      : null,
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

/**
 * The dense fleet table (`FEATURES.md` D8).
 *
 * A card grid is right for four drives and wrong for twenty-four: what a fleet owner wants is one
 * line per drive, ordered by risk, carrying the columns they would otherwise open four screens to
 * collect. The card grid stays for small hosts, and this is the other view of the same list.
 *
 * Rows are built from the same payloads the cards are, and the badges are literally the same
 * functions, so the two views cannot disagree about a drive's state.
 *
 * `onSort` is what makes the headers do something. A table header that looks clickable and is not
 * is worse than a plain label, so a column with no `key` renders as plain text and carries no
 * cursor.
 */
export function fleetTable(devices, opts = {}) {
  const {
    sortKey = 'health', ascending = true, onOpen = () => {}, onScan = () => {}, onSort = null,
    groups = null,
    // Selection, for comparing drives (`FEATURES.md` D6). Off unless a caller passes `onSelect`, so
    // every other table keeps the shape it had — a checkbox column on a table nobody compares from
    // is a column of noise.
    selected = new Set(), onSelect = null,
  } = opts;

  // A flat list is one unlabelled group. Normalising here means the body below has a single shape
  // to render, and grouping is a separator row rather than a second table with a second header.
  const sections = groups || [{ label: null, devices }];

  // In the order someone reads a fleet: where it is, what it is, how it is, how old it is.
  const columns = [
    // The select box first, where a checkbox column always goes. It carries no `key`, so it is not
    // sortable and the header logic below skips it.
    onSelect
      ? {
        key: null,
        label: '',
        cell: (d) => el('input', {
          type: 'checkbox',
          checked: selected.has(d.serial),
          'aria-label': `Select ${d.name || d.serial}`,
          // The row opens the drive; the checkbox must not also do that. The press is stopped here
          // rather than in the row, because the row's own handler cannot tell which child was hit.
          onclick: (e) => e.stopPropagation(),
          onchange: (e) => onSelect(d.serial, e.currentTarget.checked),
        }),
      }
      : null,
    { key: 'bay', label: 'Bay', cell: (d) => d.bay || '' },
    { key: 'name', label: 'Drive', cell: deviceTitle },
    { key: 'health', label: 'Verdict', cell: (d) => [statusBadge(d), worseningBadge(d)] },
    { key: 'score', label: 'Score', numeric: true, cell: (d) => fmtScore(d.score) },
    { key: 'transport', label: 'Transport', cell: transportLabel },
    { key: 'model', label: 'Model', cell: (d) => d.model || '' },
    { key: 'capacity', label: 'Capacity', numeric: true, cell: (d) => fmtBytes(d.capacity_bytes) },
    { key: 'temperature', label: 'Temp', numeric: true, cell: (d) => fmtTemp(d.temperature_c) },
    { key: 'power_on_hours', label: 'Age', numeric: true, cell: (d) => fmtHours(d.power_on_hours) },
    { key: 'last_scan', label: 'Last scan', numeric: true, cell: (d) => fmtAgo(d.last_scan) },
    { key: null, label: '', cell: (d) => el('button', {
      class: 'btn btn-sm btn-ghost',
      type: 'button',
      title: 'Scan this device',
      onclick: (e) => {
        e.stopPropagation();
        onScan(d.serial);
      },
    }, [icon('scan')]) },
    // The optional select column is dropped here rather than filtered at each use, so everything
    // below sees a list of real columns and the group separator's colspan is right by construction.
  ].filter(Boolean);

  const header = el('tr', {}, columns.map((c) => {
    const sortable = c.key !== null && onSort !== null;
    const active = sortable && c.key === sortKey;
    const props = { class: c.numeric ? 'num' : '' };
    if (sortable) {
      // The affordance is attached only when the column can actually be sorted. An empty `title`
      // is still a `title` attribute, and `th[title]` is what the stylesheet hangs the pointer
      // cursor on — so passing one unconditionally would make every header look clickable.
      props.title = `Sort by ${c.label.toLowerCase()}`;
      props['aria-sort'] = active ? (ascending ? 'ascending' : 'descending') : 'none';
      props.onclick = () => onSort(c.key);
    }
    return el('th', props, [c.label, active ? (ascending ? ' ↑' : ' ↓') : '']);
  }));

  const rowFor = (d) => el('tr', {
    class: d.is_missing ? 'fleet-row-missing' : '',
    tabindex: '0',
    title: d.is_missing ? 'This device was not present in the last scan' : '',
    onclick: () => onOpen(d.serial),
    onkeydown: (e) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        onOpen(d.serial);
      }
    },
  }, columns.map((c) => el('td', { class: c.numeric ? 'num' : '' }, [c.cell(d)])));

  const body = [];
  for (const section of sections) {
    // The separator spans every column, so the header above stays the only one. A second table
    // per group would repeat an eleven-column header once per pool, which is heavier than the
    // grouping is worth.
    if (section.label) {
      body.push(el('tr', { class: 'fleet-group' }, [
        el('td', { colspan: String(columns.length) }, [
          el('strong', { text: section.label }),
          el('span', { class: 'muted', text: ` · ${countText(section.devices.length)}` }),
        ]),
      ]));
    }
    for (const d of section.devices) body.push(rowFor(d));
  }

  return el('div', { class: 'table-wrap fleet-wrap' }, [
    el('table', { class: 'data fleet-table' }, [
      el('thead', {}, [header]),
      el('tbody', {}, body),
    ]),
  ]);
}

/**
 * Which identifier to offer for a partition: the filesystem's UUID, else the partition table's.
 *
 * A filesystem UUID is what `/etc/fstab` usually wants (`UUID=`), and a partition UUID is the
 * fallback for a partition with no filesystem — a member of a volume group, which is exactly the
 * row somebody is most likely to be copying one for.
 *
 * Returns "" when there is neither, and the caller renders no button rather than a button that
 * copies nothing.
 */
export function partitionUUID(p) {
  return String((p && (p.uuid || p.part_uuid)) || '').trim();
}

/**
 * A sector size, named the way people say it.
 *
 * "512e" and "4Kn" are the terms a reader will find elsewhere, and which one applies is decided by
 * the *pair* of numbers rather than by either alone: 512 logical on 4096 physical is 512e, 4096 on
 * 4096 is 4Kn. A drive where the two match and are 512 is neither, and gets no label rather than a
 * wrong one.
 */
export function sectorLabel(logical, physical) {
  if (!logical || !physical) return '';
  if (logical === 4096 && physical === 4096) return '4Kn';
  if (logical === 512 && physical === 4096) return '512e';
  return '';
}

/**
 * The level at which a usage bar turns amber (`FEATURES.md` A28).
 *
 * A stored setting rather than a constant, because where "nearly full" starts is the user's call —
 * and a *display* threshold, not a verdict: it does not touch the score. The two answer different
 * questions (how much room is left vs how healthy the drive is), so the bar's colour and the health
 * badge never contradict each other; they are not saying the same kind of thing.
 */
let storageWarnPct = 90;

/** Set the amber level. Anything that is not a percentage falls back to the default. */
export function setStorageWarnPct(value) {
  const n = Number(value);
  storageWarnPct = Number.isFinite(n) && n >= 1 && n <= 100 ? n : 90;
}

/** The level in force, for a caller that needs to know (and for the tests). */
export function storageWarnLevel() {
  return storageWarnPct;
}

/**
 * A horizontal usage bar, with the figure beside it.
 *
 * One colour until it crosses the warning level the user set, then amber. The threshold is a stored
 * setting rather than a constant in this file: the browser deciding on its own where "nearly full"
 * begins would be the UI inventing a judgement, and the app has one place for those.
 *
 * Returns null for a value that is not a number, so a caller can render "not measured" itself rather
 * than getting a bar drawn at zero.
 */
export function usageBar(pct, opts = {}) {
  const { label = '', title = '' } = opts;
  // Checked explicitly, because `Number(null)` and `Number('')` are both 0 — and a bar drawn at 0%
  // for a drive whose usage is *unknown* is a confident wrong answer, not a missing one.
  if (pct === null || pct === undefined || pct === '') return null;
  const value = Number(pct);
  if (!Number.isFinite(value)) return null;
  const clamped = Math.max(0, Math.min(100, Math.round(value)));

  return el('div', { class: 'usage' }, [
    el('div', {
      class: 'usage-track',
      role: 'img',
      'aria-label': `${clamped}% used`,
      title: title || `${clamped}% used`,
    }, [el('div', {
      class: clamped >= storageWarnPct ? 'usage-fill usage-fill-warn' : 'usage-fill',
      style: `width:${clamped}%`,
    })]),
    el('span', { class: 'usage-pct', text: `${clamped}%` }),
    label ? el('span', { class: 'usage-label muted', text: label }) : null,
  ]);
}

/**
 * How many of these filesystems are mounted on the host but outside the container.
 *
 * `lsblk` reads usage with `statvfs` on the *mountpoint*, so a host filesystem that is not
 * bind-mounted into the container has no figure to give. Exported so the card can decide whether the
 * explanation is worth showing at all, and so the rule is testable on its own.
 */
export function partitionsOutsideContainer(partitions) {
  return (partitions || []).filter((p) => (p.mountpoints || []).length &&
    !p.use_percent && p.used_bytes === undefined).length;
}

/**
 * A map of the disk: one segment per top-level partition, sized by how much of the disk it takes,
 * with its own fullness filled in from the left (`FEATURES.md` A28).
 *
 * Three things this is careful about, each of which would otherwise make it lie:
 *
 * - **Top-level partitions only.** The partition list is flat and includes logical volumes and
 *   dm-crypt mappings, which live *inside* a partition. Drawing one beside its container would claim
 *   the disk holds more than it does. `top_level` is what the server sends to say which is which.
 * - **Unknown fullness is not zero.** A segment whose filesystem the container cannot see is drawn
 *   with a distinct "not measured" fill. A bar at 0% would read as an empty partition, which is the
 *   opposite of what is known.
 * - **Free space is approximate, and says so.** It is the disk size less the parts that claim it,
 *   which ignores alignment gaps. The segment is hatched rather than solid, because on a visual a
 *   wrong sliver is *visible* — and the tooltip says which kind of number it is.
 *
 * `onHover` is called with a partition name on mouse-over and with null on mouse-out, which is how the
 * table underneath highlights the row the segment stands for. The map is geometry and the table is
 * detail; pointing at one should say which row the other means.
 *
 * Returns null when there is nothing to draw, so a caller can fall back to the table alone.
 */
export function diskMap(layout, opts = {}) {
  const all = (layout && layout.partitions) || [];
  const total = Number((layout && layout.size_bytes) || 0);
  const segments = all.filter((p) => p.top_level && Number(p.size_bytes) > 0);
  if (!total || !segments.length) return null;

  const claimed = segments.reduce((sum, p) => sum + Number(p.size_bytes), 0);
  // Clamped: an extended partition makes its children add up to more than the disk, and a negative
  // width would render as a broken map rather than as the approximation it is.
  const free = Math.max(0, total - claimed);

  // A tooltip of our own rather than the `title` attribute: the browser's own takes about a second to
  // appear, and a map is something you sweep across — a label that arrives after you have moved on is
  // worse than none. It lives inside the map so its position is the map's geometry, not the page's.
  const tip = el('div', { class: 'disk-tooltip', hidden: true });

  const parts = segments.map((p) => {
    const share = (Number(p.size_bytes) / total) * 100;
    const pct = usePercentNumber(p);
    const title = segmentTitle(p);
    const node = el('div', {
      class: 'disk-segment',
      style: `width:${share}%`,
      'data-partition': p.name,
      // Named for a screen reader, since the whole thing is geometry. Deliberately *not* `title`: that
      // would give two tooltips, one of them a second late.
      role: 'img',
      'aria-label': title,
    }, [
      pct === null
        ? el('div', { class: 'disk-segment-fill disk-segment-unmeasured' })
        : el('div', { class: 'disk-segment-fill', style: `width:${pct}%` }),
    ]);
    // Positioned by the segment's own centre, so the label points at the thing it describes.
    hoverable(node, tip, title, share, () => {
      if (opts.onHover) opts.onHover(p.name);
    }, () => {
      if (opts.onHover) opts.onHover(null);
    });
    return node;
  });

  // Cumulative offsets, so a segment's centre can be computed without measuring anything: the map is
  // laid out by percentage width, so where a segment starts is arithmetic rather than geometry.
  let at = 0;
  for (const node of parts) {
    node.dataset.start = String(at);
    at += Number(String(node.style.width).replace('%', '')) || 0;
  }

  if (free > total / 100) {
    // Only when it is a *share* of the disk. Below a percent it is an alignment remainder — the
    // 1.4 MB left over after a partition table, which is real but is not "free space" in any sense a
    // reader would use. The map's own background shows through there, which reads correctly as "the
    // parts do not quite add up", and the figure is still in Details with its caveat.
    const node = el('div', {
      class: 'disk-segment disk-segment-free',
      style: `width:${(free / total) * 100}%`,
      role: 'img',
      'aria-label': `${fmtBytes(free)} free, approximate`,
    });
    hoverable(node, tip, `${fmtBytes(free)} free — approximate: alignment gaps and the backup GPT ` +
      'header are not counted', (free / total) * 100 + at, null, null);
    parts.push(node);
  }

  return el('div', { class: 'disk-map' }, [tip, ...parts]);
}

/**
 * Makes a segment answer a mouse immediately, and mark itself as the one being pointed at.
 *
 * `mouseenter`/`mouseleave` rather than `mouseover`/`mouseout`: the segment has a child (the fill),
 * and the bubbling pair fires again every time the pointer crosses into it — which flickers both the
 * label and the highlighted row.
 */
function hoverable(node, tip, label, share, onEnter, onLeave) {
  node.addEventListener('mouseenter', () => {
    tip.textContent = label;
    tip.hidden = false;
    placeTooltip(tip, (Number(node.dataset.start) || 0) + share / 2);
    node.className = `${node.className} is-hover`;
    if (onEnter) onEnter();
  });
  node.addEventListener('mouseleave', () => {
    tip.hidden = true;
    node.className = node.className.split(/\s+/).filter((c) => c && c !== 'is-hover').join(' ');
    if (onLeave) onLeave();
  });
}

/**
 * Puts the tooltip under the segment centred at `centre` percent, keeping it inside the map.
 *
 * A label centred on a segment near either edge would hang off the card, so the outer ones are pinned
 * to the edge instead — the pointer is already saying which segment it means, and the label only has
 * to stay readable.
 */
function placeTooltip(tip, centre) {
  if (centre < 15) {
    tip.style.left = '0';
    tip.style.right = 'auto';
    tip.style.transform = 'none';
  } else if (centre > 85) {
    tip.style.left = 'auto';
    tip.style.right = '0';
    tip.style.transform = 'none';
  } else {
    tip.style.left = `${centre}%`;
    tip.style.right = 'auto';
    tip.style.transform = 'translateX(-50%)';
  }
}

/** What a segment says when pointed at, and to a screen reader. */
function segmentTitle(p) {
  const label = String(p.label || p.part_label || '').trim() || p.name;
  const size = fmtBytes(p.size_bytes);
  const pct = usePercentNumber(p);
  const usage = pct === null
    ? (p.mountpoints || []).length
      ? 'usage not visible to the container'
      : 'not mounted'
    : `${pct}% full`;
  const mount = (p.mountpoints || []).length ? ` at ${p.mountpoints[0]}` : '';
  return `${label} — ${size}, ${usage}${mount}`;
}

/** lsblk formats this ("47%"), so the number has to be parsed back out of the string. */
function usePercentNumber(p) {
  const raw = String((p && p.use_percent) || '').replace('%', '').trim();
  if (!raw) return null;
  const n = Number(raw);
  return Number.isFinite(n) ? n : null;
}

/**
 * What to say about a drive with no partition table (`FEATURES.md` A28).
 *
 * "Unpartitioned, or its table could not be read" is true and useless. A disk pulled from a RAID
 * array has no table *because* it is an array member — and `lsblk` already reported what it is as the
 * disk node's own filesystem type. Saying so turns a message that reads like a fault into a
 * description of a drive doing its job.
 *
 * The signature is shown in parentheses beside the plain-language name, because the exact blkid token
 * is what somebody will search for — and it is the thing to quote when asking whether a drive is
 * safe to reuse.
 */
export function noPartitionsNote(layout) {
  const signature = String((layout && layout.whole_disk_fstype) || '').trim();
  if (!signature) {
    return 'This drive has no partition table. It is either unpartitioned or its table could not be ' +
      'read.';
  }

  const member = String((layout && layout.whole_disk_name) || '').trim();
  if (member) {
    return `This drive is a member of ${member} (\`${signature}\`), so it has no partition table of ` +
      'its own — the layout lives in the array, not on the disk. Nothing here is a fault.';
  }

  // A filesystem written straight to the device: no table, but plenty to say.
  return `This drive has no partition table. It holds a \`${signature}\` filesystem written directly ` +
    'to the disk instead.';
}

/**
 * The partition schema (`FEATURES.md` A28), as a table.
 *
 * The label leads and the node name follows it, because a person recognises "media" long before they
 * recognise "/dev/mapper/tank-media" — and the node name is still there for the one who needs it.
 *
 * The partition type sits under the filesystem rather than in a column of its own: the two are
 * answering one question ("what is this partition for?") and side by side they read as a pair. A
 * partition with no filesystem is the case that shows it — "Linux LVM" under a dash is the *reason*
 * there is nothing to mount, where a separate column left the reader to join them up themselves.
 *
 * `showUsage` is the setting that turns the usage column off. The column is dropped rather than
 * emptied: an empty column is a worse way to say "you asked not to see this" than no column.
 */
export function partitionTable(layout, opts = {}) {
  const { showUsage = true } = opts;
  const partitions = (layout && layout.partitions) || [];
  if (!partitions.length) {
    return el('p', { class: 'muted', text: noPartitionsNote(layout) });
  }

  const head = ['Partition', 'Size', 'Filesystem', 'Mounted at'];
  if (showUsage) head.push('Used');
  head.push('');

  const rows = partitions.map((p) => {
    const label = String(p.label || p.part_label || '').trim();
    const cells = [
      el('td', {}, [
        el('span', { class: 'part-name', text: label || p.name }),
        label ? el('span', { class: 'part-node muted', text: p.name }) : null,
        p.read_only
          ? el('span', { class: 'badge badge-caution part-flag', title:
              'The device reports this as read-only — a write-protect switch, or a device that has ' +
              'failed into read-only' }, [icon('alert'), 'Read-only'])
          : null,
      ]),
      el('td', { class: 'num', text: fmtBytes(p.size_bytes) }),
      el('td', {}, [
        el('span', { text: filesystemText(p) }),
        p.part_type ? el('span', { class: 'part-type muted', text: p.part_type }) : null,
      ]),
      el('td', {}, (p.mountpoints || []).length
        ? (p.mountpoints || []).map((m) => el('div', { class: 'part-mount', text: m }))
        : [el('span', { class: 'muted', text: 'not mounted' })]),
    ];

    if (showUsage) cells.push(usageCell(p));

    const uuid = partitionUUID(p);
    cells.push(el('td', { class: 'num' }, uuid
      ? [el('button', {
          class: 'btn btn-sm btn-ghost part-copy',
          type: 'button',
          title: `Copy ${uuid}`,
          onclick: () => copyToClipboard(uuid),
        }, [icon('save'), 'UUID'])]
      : []));

    // `data-partition` is the map's half of the link (`FEATURES.md` A28): the segment carries the same
    // value, so pointing at one marks the other. Keyed by the drive's own name for the partition
    // rather than by row order, because a table with logical volumes in it has more rows than the map
    // has segments — deliberately so.
    return el('tr', {
      class: p.read_only ? 'part-readonly' : null,
      'data-partition': p.name,
    }, cells);
  });

  return el('div', { class: 'table-wrap' }, [
    el('table', { class: 'data part-table' }, [
      el('thead', {}, [el('tr', {}, head.map((h, i) => el('th', {
        text: h, class: i === 1 ? 'num' : '',
      })))]),
      el('tbody', {}, rows),
    ]),
  ]);
}

/** Filesystem and version, or a dash — a partition can hold a volume group and have neither. */
function filesystemText(p) {
  if (!p.fstype) return '—';
  return p.fs_version ? `${p.fstype} ${p.fs_version}` : p.fstype;
}

/**
 * The usage cell.
 *
 * Three cases, and they are genuinely different answers:
 *
 * - **Nothing mounted**: nothing to measure. A dash, because "not visible" would send somebody to
 *   their compose file for no reason — and the mount column already says it is not mounted.
 * - **Mounted, but the container cannot see it**: `lsblk` reads usage with `statvfs` on the
 *   *mountpoint*, so a host filesystem that is not bind-mounted into the container has no figure to
 *   give. "Not visible" is the true answer, and a dash would be mistaken for a bug.
 * - **Mounted and visible**: the figure.
 */
function usageCell(p) {
  if (!(p.mountpoints || []).length) {
    return el('td', { class: 'num muted', text: '—' });
  }
  const bar = usageBar(usePercentNumber(p));
  if (!bar) {
    return el('td', { class: 'num' }, [
      el('span', {
        class: 'muted',
        title: 'The container cannot see this mountpoint, so there is no usage figure to read. ' +
          'Mount the host filesystem read-only to see it.',
        text: 'not visible',
      }),
    ]);
  }
  const used = p.used_bytes !== undefined ? fmtBytes(p.used_bytes) : '';
  const total = p.fs_size_bytes !== undefined ? fmtBytes(p.fs_size_bytes) : '';
  return el('td', { class: 'num' }, [bar
    ? el('div', { title: used && total ? `${used} of ${total}` : '' }, [bar])
    : null]);
}

/**
 * The one line that explains a column of "not visible" rows, for the Details disclosure.
 *
 * It lives in the disclosure with the other explanation-shaped content, and the cell itself carries the
 * short version as a tooltip.
 *
 * Returned as a plain string so the caller can put it wherever the card's layout wants it.
 */
export function outsideContainerNote(partitions) {
  const hidden = partitionsOutsideContainer(partitions);
  if (!hidden) return '';
  const count = hidden === 1 ? '1 of these filesystems is' : `${hidden} of these filesystems are`;
  return `${count} mounted on the host but not inside this container, so the usage cannot be ` +
    'read. Add the mount roots read-only to the compose file — for example `- /mnt:/mnt:ro` — and ' +
    'the figures appear.';
}

/** Copy a value, saying so either way. Silently succeeding is indistinguishable from not working. */
function copyToClipboard(value) {
  if (typeof navigator === 'undefined' || !navigator.clipboard) {
    toast('Copying needs a secure context (https or localhost)', 'error');
    return;
  }
  navigator.clipboard.writeText(value)
    .then(() => toast('UUID copied', 'good'))
    .catch(() => toast('Could not copy to the clipboard', 'error'));
}

/** "1 drive" / "3 drives" — a count that reads as a sentence rather than as a number. */
export function countText(n) {
  return n === 1 ? '1 drive' : `${n} drives`;
}

/** Attribute table in the CrystalDiskInfo layout. */
export function attributeTable(attributes, opts = {}) {
  // The attribute ids the verdict read (`FEATURES.md` A12). Passed in rather than inferred, because
  // "the score dropped 30 points" and "the drive says this row is at its own threshold" are
  // different statements, and the table is the only place both are visible at once.
  const blamed = opts.blamed || null;
  // Whether smartctl knows this model (`A28`), from the same blob the attributes came in. Tri-state:
  // true, false, or absent when smartctl did not say.
  const inDatabase = opts.inDatabase;
  if (!attributes || !attributes.length) {
    return el('p', { class: 'muted', text: 'No ATA attribute table was reported for this device.' });
  }
  const head = ['ID', 'Attribute', 'Value', 'Worst', 'Thresh', 'Raw'];
  const unnamed = attributes.filter((a) => attributeName(a) === null).length;
  return el('div', { class: 'table-wrap' }, [
    el('table', { class: 'data' }, [
      el('thead', {}, [
        el('tr', {}, head.map((h, i) => el('th', { text: h, class: i === 0 || i > 1 ? 'num' : '' }))),
      ]),
      el('tbody', {}, attributes.map((a) => attributeRow(a, blamed))),
    ]),
    vendorNote(unnamed, inDatabase),
  ]);
}

/**
 * The note under the attribute table, when some rows have no name.
 *
 * A dozen rows reading "Vendor specific" looks like the app failed to read them. It is the drive
 * reporting counters nobody has published a meaning for, and smartctl declining to invent one — but
 * *why* smartctl has no name differs, and `in_smartctl_database` (`FEATURES.md` A28) is what tells the
 * three cases apart:
 *
 * - **false**: smartctl does not have this model at all, so it has no names for *any* of them.
 * - **true**: it knows the model and these particular counters are vendor-private.
 * - **absent**: it did not say, so the note states what is observable and claims nothing more. This is
 *   the wording the table used for all three before the field existed.
 */
function vendorNote(unnamed, inDatabase) {
  if (!unnamed) return null;
  const count = `${unnamed} of these attributes`;
  let text;
  if (inDatabase === false) {
    text = `${count} are vendor specific. smartctl does not have this drive model in its database, ` +
      'so it has no names for any of its counters and the app cannot say what they mean. Their values ' +
      'are shown exactly as the drive reports them.';
  } else if (inDatabase === true) {
    text = `${count} are vendor specific. smartctl recognises this drive, but these particular ` +
      'counters have no published meaning — the vendor keeps them private. Their values are shown ' +
      'exactly as the drive reports them.';
  } else {
    text = `${count} are vendor specific. smartctl has no name for them, so the app cannot say what ` +
      'they mean. Their values are shown exactly as the drive reports them.';
  }
  return el('p', { class: 'muted attr-note', text });
}

/**
 * The name to show for an attribute, or null when the drive's is a placeholder.
 *
 * smartctl names anything it cannot identify `Unknown_Attribute`. On a drive it does not
 * recognise that is a dozen rows, and the placeholder reads as a fault in the app rather than
 * as a property of the drive.
 */
function attributeName(a) {
  const name = String((a && a.name) || '').trim();
  return !name || name === 'Unknown_Attribute' ? null : name;
}

/**
 * One attribute row.
 *
 * Three additions to the plain CrystalDiskInfo layout, all of which exist to answer "why is this
 * drive unhappy?" without leaving the page: a `?` explaining what the attribute means, a marker for
 * attributes the *drive itself* reports as failing, and a marker for attributes the **verdict read**
 * (`FEATURES.md` A12).
 *
 * The two markers are different claims and are worded as such. "At threshold" is the drive's own
 * alarm; "counted" is this app saying the row is part of why the score is what it is. A row can be
 * either, both, or neither, and the tooltips say which.
 */
function attributeRow(a, blamed = null) {
  const help = attributeHelp(a.id);
  const flagged = attributeFlagged(a);
  const counted = Boolean(blamed && blamed.has(a.id));
  const name = attributeName(a);
  return el('tr', { class: flagged ? 'attr-flagged' : null }, [
    el('td', { class: 'num', text: fmtCell(a.id) }),
    el('td', {}, [
      name
        ? el('span', { text: name })
        : el('span', { class: 'muted', text: 'Vendor specific' }),
      help ? helpButton(name || `attribute ${a.id}`, help) : null,
      flagged
        ? el('span', {
            class: 'badge badge-caution attr-flag',
            title: 'The drive reports this attribute at or below its own threshold',
          }, [icon('alert'), 'At threshold'])
        : null,
      counted
        ? el('span', {
            class: 'badge badge-unknown attr-counted',
            title: 'This attribute is part of the score — see the breakdown above the table',
          }, ['Counted'])
        : null,
    ]),
    el('td', { class: 'num', text: fmtCell(a.value) }),
    el('td', { class: 'num', text: fmtCell(a.worst) }),
    el('td', { class: 'num', text: fmtCell(a.threshold) }),
    el('td', { class: 'num', text: fmtCell(a.raw_string || a.raw_value) }),
  ]);
}

/**
 * The `?` that opens an attribute's explanation.
 *
 * A button rather than a bare `title` attribute: hover-only help is unusable on the
 * mobile layout, and this is the feature's whole point. Labelled with the attribute name
 * so it reads as "what is Reallocated Sector Count?" rather than as a bare question mark.
 */
export function helpButton(name, help) {
  return el('button', {
    class: 'attr-help',
    type: 'button',
    'aria-label': `What is ${name}?`,
    title: help.what,
    onclick: () => showHelp(name, help),
  }, ['?']);
}

/** Explain one field: what it is, and how to read it. */
export function showHelp(name, help) {
  openDialog(name, (body, close, foot) => {
    // `append` from dom.js, *not* the native method. The native `Element.append` stringifies
    // anything that is not a Node — including `null` — into a text node, so an attribute with
    // no `watch` line rendered the literal word "null" under its explanation. The helper
    // drops nulls instead, which is the whole reason it exists.
    append(body, [
      el('p', { text: help.what }),
      help.watch ? el('p', { class: 'muted', text: help.watch }) : null,
    ]);
    foot.append(el('button', { class: 'btn', type: 'button', text: 'Close', onclick: close }));
  });
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

/**
 * Modal dialog. `render(body, close, foot)` fills the scrollable body and the fixed
 * footer.
 *
 * The footer is a separate slot rather than something the caller appends to the body
 * so action buttons stay visible however long the content gets: a Close button below
 * the fold is indistinguishable from having no way out at all.
 *
 * `dismissOnBackdrop` is off for a dialog holding unsaved input. A stray click beside a
 * half-filled form then costs nothing but the click, and Cancel and Escape are both still
 * there — which is the better half of that trade, because a closed form costs the form.
 */
export function openDialog(title, render, { dismissOnBackdrop = true } = {}) {
  const body = el('div', { class: 'dialog-body' });
  const foot = el('div', { class: 'dialog-foot' });
  const dialog = el('dialog', { 'aria-label': title }, [
    el('div', { class: 'dialog-head' }, [el('h2', { text: title })]),
    body,
    foot,
  ]);
  dialog.addEventListener('close', () => dialog.remove());

  // A press that starts on the backdrop dismisses; a drag that starts inside the dialog does
  // not, however far it travels before it is released.
  //
  // The click lands on the dialog element itself, because its children cover the rest of the
  // box, so the coordinates are checked too — but coordinates alone cannot tell the two apart.
  // Selecting text in a field, or dragging a number spinner, ends with a click whose target is
  // the dialog (the nearest common ancestor of press and release) and whose coordinates are
  // outside it, which is exactly what a backdrop click looks like. That is what closed the
  // schedule form out from under a half-filled one, so the *press* is what decides.
  let pressedBackdrop = false;
  dialog.addEventListener('pointerdown', (event) => {
    pressedBackdrop = isBackdropClick(dialog, event);
  });
  dialog.addEventListener('click', (event) => {
    const onBackdrop = pressedBackdrop && isBackdropClick(dialog, event);
    pressedBackdrop = false;
    if (onBackdrop && dismissOnBackdrop) dialog.close();
  });

  const close = () => dialog.close();
  render(body, close, foot);
  document.body.append(dialog);
  dialog.showModal();
  return { dialog, close };
}

/**
 * Whether a press should be read as a press on the modal backdrop.
 *
 * A press *inside* the dialog's box is never one, even when it lands on the dialog element
 * itself — the padding between the head, body and foot is dialog, not backdrop, and closing
 * on a click there is the same lost form by a shorter route.
 */
export function isBackdropClick(dialog, event) {
  if (event.target !== dialog) return false;
  const box = dialog.getBoundingClientRect();
  return event.clientX < box.left || event.clientX > box.right
    || event.clientY < box.top || event.clientY > box.bottom;
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

    const { dialog } = openDialog(title, (body, close, foot) => {
      body.append(el('p', { text: message }));
      foot.append(
        el('button', { class: 'btn', type: 'button', text: 'Cancel',
          onclick: () => { settle(false); close(); } }),
        el('button', { class: 'btn btn-danger', type: 'button', text: confirmLabel,
          onclick: () => { settle(true); close(); } }),
      );
    });

    // Escape, a backdrop click, or any other dismissal closes the dialog without a
    // button press. Listening for `close` is the only reliable signal. Settling on a
    // timer instead resolves the promise before the user has even seen the dialog, so
    // every caller's `if (!(await confirmDialog(...))) return` bails out and the
    // confirmed action silently never happens.
    dialog.addEventListener('close', () => settle(false));
  });
}

