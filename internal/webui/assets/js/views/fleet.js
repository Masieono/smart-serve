// The fleet view (v2), a table rather than a wall of cards (`ADR-032`).
//
// It reads the same `/api/devices` payload the device page does, so the two cannot disagree about a
// drive. What differs is the shape: one row per drive, with the state and the score in one column,
// because a verdict and a number about the same drive are one fact, a separate "healthy" column and
// a separate ring said it twice and cost a column to do it.
import { el, clear, append, fmtBytes, fmtAgo, fmtInt, fmtTemp } from '../dom.js';
import { api } from '../api.js';
import { confirmDialog, openDialog } from '../components.js';
import { COMPARE_MAX_DRIVES, openCompare } from './compare.js';

// The order a fleet is worth reading in: what is failing, then what is worrying, then what we could
// not read, then the healthy drives. Sorting by name would bury the one row that matters.
const RANK = { bad: 0, caution: 1, unknown: 2, good: 3 };

const STATE_WORD = {
  good: 'Healthy',
  caution: 'Caution',
  bad: 'Failing',
  unknown: 'No reading',
};

let lastPayload = null;
let lastCtx = null;
let filter = null;
let query = '';
// Which column the table is ordered by, and which way. Null means the reading order, which is the
// same thing as Health ascending: failing first, then caution, then what we could not read.
let sort = null;
// Whether the table is showing the fleet or the drives taken out of it. Hidden drives are not a
// second list somewhere below; they are the same table with the other half in it.
let showHidden = false;

/** Reset the view's memory. Called when the app mounts, so a reload starts clean. */
export function reset() {
  lastPayload = null;
  lastCtx = null;
  filter = null;
  query = '';
  sort = null;
  showHidden = false;
}

export async function render(content, ctx) {
  clear(content);

  let payload;
  try {
    payload = await api.devices();
  } catch (err) {
    append(content, [
      el('div', { class: 'fleet-empty', text: `Could not read the fleet: ${err.message}` }),
    ]);
    return;
  }

  lastPayload = payload;
  draw(content, ctx, payload);
}

function draw(content, ctx, payload, at = here()) {
  // A redraw belongs to the page it started on. Without this, pressing Hide and then opening a drive
  // let the fleet's own fetch finish and draw its table over the drive page.
  if (at !== here()) return;
  lastCtx = ctx;
  lastPayload = payload;
  const all = payload.devices || [];
  const hiddenCount = all.filter((d) => d.hidden).length;
  // One table with two halves. Showing the fleet and the hidden drives at once would make the counts
  // above it describe neither, and a hidden drive in the middle of the fleet is not hidden.
  const pool = all.filter((d) => Boolean(d.hidden) === showHidden);
  const counts = countStates(pool);
  const rows = orderRows(pool.filter(matchesFilter));

  clear(content);
  append(content, [
    el('div', { class: 'fleet-head' }, [
      el('h1', { text: 'Fleet' }),
      searchBox(),
      compareEntry(ctx, rows),
      el('button', {
        class: 'fleet-scan',
        type: 'button',
        text: 'Scan now',
        onclick: async (event) => {
          const button = event.currentTarget;
          button.disabled = true;
          button.textContent = 'Scanning…';
          try {
            await api.scan();
          } catch (err) {
            button.disabled = false;
            button.textContent = 'Scan now';
            toast(err.message);
          }
          // Nothing else to do: the SSE stream redraws this view when the scan completes.
        },
      }),
    ]),

    el('div', { class: 'fleet-glance' }, [
      ...glanceCounts(counts),
      el('span', { class: 'when', text: whenText(payload.last_scan_ts) }),
    ]),

    rows.length ? table(rows, ctx) : emptyState(all.length, pool.length),

    el('div', { class: 'fleet-below' }, [
      el('div', {}, [
        el('span', { text: `${pool.length} drive${pool.length === 1 ? '' : 's'}` }),
        hiddenCount || showHidden ? hiddenToggle(hiddenCount) : null,
      ].filter(Boolean)),
      // No links here. The shell has a nav with both of these in it, on every page, and two copies of
      // the same destination in one view is clutter that teaches nothing. The count and the hidden
      // toggle stay, because they are about this table rather than about getting somewhere.
    ]),

  ]);
}

// --- pieces -----------------------------------------------------------------

 /**
 * The path this page is on, without assuming a browser.
 *
 * The jsdom-less test harness has no `location`, and a view that throws while looking for one would
 * fail every test for a reason that has nothing to do with the view.
 */
function here() {
  return (typeof location !== 'undefined' && location.pathname) || '';
}

/**
 * Compare two to six drives, attribute by attribute (`FEATURES.md` D6).
 *
 * The view and the endpoint have existed all along with nothing reaching them: no button, no picker,
 * no route. A picker rather than a checkbox column, because the table was just made clean and a
 * permanent column of ticks for a once-a-month action is clutter.
 */
function compareEntry(ctx, rows) {
  return el('button', {
    class: 'fleet-compare',
    type: 'button',
    text: 'Compare',
    title: 'Pick two to six drives to line up attribute by attribute',
    disabled: rows.length < 2,
    onclick: () => pickForComparison(rows, ctx),
  });
}

function pickForComparison(rows, ctx) {
  const chosen = new Set();
  openDialog('Compare drives', (body, close, foot) => {
    body.append(el('p', { class: 'muted', text:
      `Pick two to ${COMPARE_MAX_DRIVES} drives. Attributes are lined up by id, so a count that ` +
      'only one drive has still gets its own row.' }));

    const go = el('button', {
      class: 'btn btn-primary',
      type: 'button',
      text: 'Compare',
      disabled: true,
      onclick: () => {
        const serials = [...chosen];
        close();
        openCompare(ctx, serials);
      },
    });

    const sync = () => {
      go.disabled = chosen.size < 2;
      go.textContent = chosen.size ? `Compare ${chosen.size}` : 'Compare';
    };

    body.append(el('div', { class: 'compare-picker' }, rows.map((d) => {
      const box = el('input', { type: 'checkbox', value: d.serial });
      box.addEventListener('change', () => {
        if (box.checked) {
          if (chosen.size >= COMPARE_MAX_DRIVES) {
            box.checked = false;
            toast(`At most ${COMPARE_MAX_DRIVES} drives can be compared at once`, 'error');
            return;
          }
          chosen.add(d.serial);
        } else {
          chosen.delete(d.serial);
        }
        sync();
      });
      return el('label', { class: 'compare-pick' }, [
        box,
        el('span', { class: 'name', text: displayName(d) }),
        el('span', { class: 'muted', text: [d.bay, d.model].filter(Boolean).join(' · ') }),
      ]);
    })));

    foot.append(
      el('button', { class: 'btn', type: 'button', text: 'Cancel', onclick: close }),
      go,
    );
  }, { dismissOnBackdrop: false });
}

function countStates(devices) {
  const counts = { good: 0, caution: 0, bad: 0, unknown: 0 };
  for (const d of devices) counts[stateOf(d)] += 1;
  return counts;
}

function glanceCounts(counts) {
  const order = ['bad', 'caution', 'good', 'unknown'];
  return order.map((state) => {
    const pressed = filter === state;
    return el('button', {
      class: 'count',
      type: 'button',
      'aria-pressed': pressed ? 'true' : 'false',
      onclick: () => {
        filter = pressed ? null : state;
        draw(document.getElementById('content'), lastCtx, lastPayload);
      },
    }, [
      el('b', { text: String(counts[state]) }),
      el('span', { text: ` ${STATE_WORD[state].toLowerCase()}` }),
    ]);
  });
}

function stateOf(d) {
  if (d.is_missing) return 'unknown';
  const h = d.health;
  return h === 'good' || h === 'caution' || h === 'bad' ? h : 'unknown';
}

function matchesFilter(d) {
  if (filter !== null && stateOf(d) !== filter) return false;
  if (!query) return true;
  const haystack = [d.name, d.bay, d.serial, d.model, d.tags, d.fstype, d.mountpoints]
    .filter(Boolean)
    .join(' ')
    .toLowerCase();
  return haystack.includes(query.toLowerCase());
}

/**
 * Search across name, bay, serial, model, tags and mountpoint.
 *
 * The bay matters more than it looks: hunting a drive by where it physically is is the whole reason
 * the app records one. The `/` shortcut in `app.js` focuses this box by id, so the id is part of the
 * contract between the two.
 */
function searchBox() {
  const box = el('input', {
    id: 'device-search',
    type: 'search',
    class: 'fleet-search',
    placeholder: 'Search name, bay, serial, tag',
    'aria-label': 'Search drives',
    value: query,
  });
  box.addEventListener('input', (event) => {
    query = event.target.value;
    const caret = event.target.selectionStart;
    // Redraw the page and put the caret back: a filter change must not cost the reader their place
    // in the box they are typing into.
    draw(document.getElementById('content'), lastCtx, lastPayload);
    const again = document.getElementById('device-search');
    if (again) {
      again.focus();
      if (caret !== null) again.setSelectionRange(caret, caret);
    }
  });
  return box;
}

function sortForReading(devices) {
  return [...devices].sort((a, b) => {
    const byState = RANK[stateOf(a)] - RANK[stateOf(b)];
    if (byState !== 0) return byState;
    // Within a state, the lower score first: a 40-good is worth reading before a 100-good.
    const sa = typeof a.score === 'number' && a.score >= 0 ? a.score : 101;
    const sb = typeof b.score === 'number' && b.score >= 0 ? b.score : 101;
    if (sa !== sb) return sa - sb;
    return displayName(a).localeCompare(displayName(b));
  });
}

function displayName(d) {
  return d.name || d.model || d.serial || 'Unknown drive';
}

function whenText(ts) {
  if (!ts) return 'Never scanned';
  return `Checked ${fmtAgo(ts)}`;
}

function emptyState(total, shown) {
  const text = total === 0
    ? 'No drives yet, press Scan to look for them.'
    : shown === 0 && showHidden
      ? 'Nothing is hidden.'
      : showHidden ? 'No hidden drives match.' : 'No drives match.';
  return el('div', { class: 'fleet-empty' }, [el('div', { text })]);
}

/**
 * The way into and out of the hidden half, and back again.
 *
 * A number rather than an icon: "3 hidden" says both that something is out of sight and how much,
 * and a reader who has just hidden a drive needs to see it land somewhere.
 */
function hiddenToggle(count) {
  const label = showHidden ? 'Back to the fleet' : `${count} hidden`;
  return el('button', {
    class: 'fleet-hidden-toggle',
    type: 'button',
    'aria-pressed': showHidden ? 'true' : 'false',
    text: label,
    onclick: () => {
      showHidden = !showHidden;
      draw(document.getElementById('content'), lastCtx, lastPayload);
    },
  });
}

// Every column is sortable, including the two that are not numbers. `col` is the width class the
// colgroup uses; `key` is what the comparator switches on.
const COLUMNS = [
  { key: 'drive', label: 'Drive', className: '', col: 'drive' },
  { key: 'health', label: 'Health', className: '', col: 'health' },
  { key: 'usage', label: 'Usage', className: 'usage-cell', col: 'usage' },
  { key: 'temp', label: 'Temp', className: 'r temp-cell', col: 'temp' },
  { key: 'hours', label: 'Powered on', className: 'r age-cell', col: 'age' },
];

function table(devices, ctx) {
  const head = el('thead', {}, [el('tr', {}, [
      ...COLUMNS.map(sortHeader),
      el('th', { class: 'actions-cell' }, [el('span', { class: 'visually-hidden', text: 'Actions' })]),
    ])]);

  const body = el('tbody', {});
  for (const d of devices) {
    append(body, [row(d, ctx)]);
  }

  return el('table', { class: 'fleet-table' }, [
    el('colgroup', {}, [
      ...COLUMNS.map((c) => el('col', { class: c.col })),
      el('col', { class: 'actions' }),
    ]),
    head,
    body,
  ]);
}

/**
 * A header that sorts, and says which way it is sorting.
 *
 * `aria-sort` is on the `th` and the caret is on the button, which is the arrangement a screen reader
 * reads as "Health, column, sorted descending": the button is the control, the header cell is the
 * thing that carries the state.
 */
function sortHeader(col) {
  const current = sortedBy(col.key);
  const aria = current === null ? 'none' : current.dir === 1 ? 'ascending' : 'descending';
  return el('th', { class: col.className, 'aria-sort': aria }, [
    el('button', {
      class: 'fleet-sort',
      type: 'button',
      title: sortTitle(col.label, current),
      onclick: () => {
        sort = current && current.dir === 1 ? { key: col.key, dir: -1 } : { key: col.key, dir: 1 };
        draw(document.getElementById('content'), lastCtx, lastPayload);
      },
    }, [
      el('span', { text: col.label }),
      el('i', { class: current ? `caret ${current.dir === 1 ? 'asc' : 'desc'}` : 'caret', 'aria-hidden': 'true' }),
    ]),
  ]);
}

/** The sort in force for one column: the pending sort when it names this one, else the reading order. */
function sortedBy(key) {
  if (sort === null) return key === 'health' ? { key: 'health', dir: 1 } : null;
  return sort.key === key ? sort : null;
}

function sortTitle(label, current) {
  if (current === null) return `Sort by ${label}`;
  return current.dir === 1 ? `Sort by ${label}, descending` : `Sort by ${label}, ascending`;
}

/**
 * The rows, in the order asked for.
 *
 * The reading order is the base, so ties fall back to "what is worst first" rather than to whatever
 * the API happened to send. A drive with no value for the sorted column goes last in **both**
 * directions: sorting by temperature descending is a question about temperatures, and a drive that
 * does not report one is not the warmest drive in the fleet.
 */
export function orderRows(devices, sortSpec = sort) {
  const base = sortForReading(devices);
  if (sortSpec === null) return base;
  const { key, dir } = sortSpec;

  return base.sort((a, b) => {
    if (key === 'health') {
      const byState = RANK[stateOf(a)] - RANK[stateOf(b)];
      if (byState !== 0) return dir * byState;
      return dir * (scoreOf(a) - scoreOf(b));
    }
    const va = columnValue(key, a);
    const vb = columnValue(key, b);
    const na = va === null;
    const nb = vb === null;
    if (na || nb) return na && nb ? 0 : na ? 1 : -1;
    if (typeof va === 'number' && typeof vb === 'number') return dir * (va - vb);
    return dir * String(va).localeCompare(String(vb), undefined, { numeric: true });
  });
}

function scoreOf(d) {
  return typeof d.score === 'number' && d.score >= 0 ? d.score : 101;
}

function columnValue(key, d) {
  switch (key) {
    case 'drive':
      return displayName(d);
    case 'usage':
      return typeof d.used_pct === 'number' ? d.used_pct : null;
    case 'temp':
      return typeof d.temperature_c === 'number' ? d.temperature_c : null;
    case 'hours':
      return typeof d.power_on_hours === 'number' ? d.power_on_hours : null;
    default:
      return null;
  }
}

function row(d, ctx) {
  const state = stateOf(d);
  const classes = [];
  if (state === 'bad' || state === 'caution' || d.is_missing) classes.push('attention');
  if (d.is_missing) classes.push('missing');
  if (d.hidden) classes.push('is-hidden');

  const sub = [];
  if (d.name) sub.push(d.model || '');
  if (d.transport) sub.push(d.transport);
  if (d.capacity_bytes) sub.push(fmtBytes(d.capacity_bytes));
  if (d.mountpoints && d.mountpoints.length) sub.push(d.mountpoints[0]);

  return el('tr', {
    class: classes.join(' ') || null,
    tabindex: '0',
    onclick: () => ctx.navigate(`/device/${encodeURIComponent(d.serial)}`),
    onkeydown: (event) => {
      if (event.key === 'Enter' || event.key === ' ') {
        event.preventDefault();
        ctx.navigate(`/device/${encodeURIComponent(d.serial)}`);
      }
    },
  }, [
    el('td', { class: 'drive' }, [
      el('div', { class: 'name', text: displayName(d) }),
      el('div', { class: 'sub', text: sub.filter(Boolean).join(' · ') }),
    ]),
    el('td', {}, [healthCell(d, state)]),
    el('td', { class: 'usage-cell' }, [usageCell(d)]),
    el('td', { class: 'r temp-cell' }, [
      el('span', {
        class: d.temperature_c === null || d.temperature_c === undefined ? 'fleet-num dim' : 'fleet-num',
        text: d.temperature_c === null || d.temperature_c === undefined ? 'n/a' : fmtTemp(d.temperature_c),
      }),
    ]),
    el('td', { class: 'r age-cell' }, [
      el('span', {
        class: d.power_on_hours === null || d.power_on_hours === undefined ? 'fleet-num dim' : 'fleet-num',
        text: d.power_on_hours === null || d.power_on_hours === undefined
          ? 'n/a'
          : `${fmtInt(d.power_on_hours)} h`,
      }),
    ]),
    el('td', { class: 'actions-cell' }, rowActions(d, ctx)),
  ]);
}

/**
 * Hide, unhide, and forget.
 *
 * Hiding is the answer to clutter: a drive that is out of the fleet for a while, a spare, a card
 * reader. Forgetting is the answer to a drive that was never in it: somebody plugging in a run of
 * drives to pull their S.M.A.R.T. and not wanting twelve rows of it afterwards. Forgetting is only
 * offered for a drive that is not connected, because for a connected one the next scan would undo it
 * and the button would look broken; the dialog says as much.
 */
function rowActions(d, ctx) {
  const buttons = [
    el('button', {
      class: 'row-action',
      type: 'button',
      text: showHidden ? 'Unhide' : 'Hide',
      'aria-label': `${showHidden ? 'Show' : 'Hide'} ${displayName(d)}${showHidden ? ' in the fleet again' : ' in the fleet'}`,
      onclick: (event) => {
        event.stopPropagation();
        setHidden(d, !showHidden, ctx);
      },
    }),
  ];
  if (d.is_missing) {
    buttons.push(el('button', {
      class: 'row-action danger',
      type: 'button',
      text: 'Forget',
      'aria-label': `Forget ${displayName(d)} and everything recorded about it`,
      onclick: (event) => {
        event.stopPropagation();
        forget(d, ctx);
      },
    }));
  }
  return buttons;
}

/**
 * The whole notes object, not just the field being changed.
 *
 * `PUT /api/devices/{serial}/notes` replaces the lot: an omitted field is cleared, which is what the
 * endpoint documents and what the device page's editor relies on. Sending `{ hidden }` on its own
 * therefore wiped the drive's name, notes, tags and bay the first time anybody pressed Hide. Every
 * value the endpoint owns is carried back from the row, and `bay` defaults to '' because the list
 * omits it when it is empty.
 */
function notesBody(d, hidden) {
  return {
    name: d.name ?? '',
    notes: d.notes ?? '',
    tags: d.tags ?? '',
    bay: d.bay ?? '',
    hidden,
  };
}

async function setHidden(d, hidden, ctx) {
  try {
    await api.setNotes(d.serial, notesBody(d, hidden));
    await reload(ctx);
  } catch (err) {
    toast(err.message);
  }
}

async function forget(d, ctx) {
  const ok = await confirmDialog(
    'Forget this drive?',
    `${displayName(d)} and every scan recorded for it will be removed. It is not connected now; if it ` +
    'comes back, the next scan will find it again as a new drive.',
    'Forget',
  );
  if (!ok) return;
  try {
    await api.forgetDevice(d.serial);
    await reload(ctx);
  } catch (err) {
    toast(err.message);
  }
}

/** Re-read the fleet and redraw, so the counts and the table agree after a hide or a forget. */
async function reload(ctx) {
  const at = here();
  try {
    lastPayload = await api.devices();
    if (at !== here()) return;
    // Unhiding the last hidden drive leaves an empty half and a toggle that leads nowhere. The reader
    // wanted the drive back in the fleet, so go there rather than to "Nothing is hidden."
    if (showHidden && !(lastPayload.devices || []).some((d) => d.hidden)) showHidden = false;
    draw(document.getElementById('content'), ctx, lastPayload);
  } catch (err) {
    toast(err.message);
  }
}

/**
 * State and score in one cell. The score is the interpreter's 0-100, and it sits beside the word
 * rather than in a column of its own because a verdict and the number behind it are the same fact
 * told twice, `docs/UI.md` §7.1 keeps both, and this is the compact form of that.
 */
function healthCell(d, state) {
  const score = typeof d.score === 'number' && d.score >= 0 ? d.score : null;
  const word = d.is_missing ? 'Not connected' : STATE_WORD[state];

  const children = [
    el('div', { class: 'fleet-health ' + state }, [
      el('i', { class: 'dot' }),
      el('span', { class: 'word', text: word }),
      score === null ? null : el('span', { class: 'score', text: String(score) }),
    ]),
  ];

  // One line of explanation when the state does not explain itself. "No reading" over a USB bridge is
  // the common case, but a drive the interpreter has *called* failing is the one a reader will
  // actually act on, and the reason is the actionable part.
  if (state !== 'good' && d.health_rationale) {
    children.push(el('span', { class: 'fleet-why', text: shortReason(d.health_rationale) }));
  }
  return el('div', {}, children.filter(Boolean));
}

function shortReason(rationale) {
  // smartctl's message, with the leading `error: ` noise dropped. The full text lives on the
  // device page, which is one click away and has room for it.
  return String(rationale).replace(/^error:\s*/i, '');
}

function usageCell(d) {
  const pct = typeof d.used_pct === 'number' ? d.used_pct : null;
  if (pct === null) {
    return el('span', { class: 'fleet-num dim', text: 'n/a' });
  }
  const capped = Math.max(0, Math.min(100, pct));
  return el('div', { class: 'fleet-usage' }, [
    el('span', { class: 'track' }, [
      el('i', { class: capped >= 85 ? 'high' : null, style: `width:${capped}%` }),
    ]),
    el('span', { class: 'pct', text: `${Math.round(pct)}%` }),
  ]);
}

function toast(message) {
  const host = document.getElementById('toasts');
  if (!host) return;
  const node = el('div', { class: 'toast', text: message });
  host.append(node);
  setTimeout(() => node.remove(), 6000);
}
