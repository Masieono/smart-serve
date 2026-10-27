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
        // line no longer reaches both edges, and an area stretched to the box would paint data
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
 * would be noise. It exists so the span is readable *without hovering* — the old card could only
 * tell you its window by pointing at it, which is a poor way to read a chart.
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
  } = opts;

  // A flat list is one unlabelled group. Normalising here means the body below has a single shape
  // to render, and grouping is a separator row rather than a second table with a second header.
  const sections = groups || [{ label: null, devices }];

  // In the order someone reads a fleet: where it is, what it is, how it is, how old it is.
  const columns = [
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
  ];

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

/** "1 drive" / "3 drives" — a count that reads as a sentence rather than as a number. */
export function countText(n) {
  return n === 1 ? '1 drive' : `${n} drives`;
}

/** Attribute table in the CrystalDiskInfo layout. */
export function attributeTable(attributes) {
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
      el('tbody', {}, attributes.map(attributeRow)),
    ]),
    // Said out loud rather than left to be inferred. A dozen rows reading "Unknown_Attribute"
    // looks like the app failed to read them. It is the drive reporting vendor counters that
    // nobody has published a meaning for, and smartctl declining to invent one either.
    unnamed
      ? el('p', { class: 'muted attr-note', text:
          `${unnamed} of these attributes are vendor specific. smartctl does not recognise this `
          + 'drive model, so it has no name for them and the app cannot say what they mean. Their '
          + 'values are shown exactly as the drive reports them.' })
      : null,
  ]);
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
 * Two additions to the plain CrystalDiskInfo layout, both of which exist to answer "why
 * is this drive unhappy?" without leaving the page: a `?` explaining what the attribute
 * means, and a marker for attributes the *drive itself* reports as failing.
 */
function attributeRow(a) {
  const help = attributeHelp(a.id);
  const flagged = attributeFlagged(a);
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

