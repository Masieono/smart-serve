// The fleet view (v2), a table rather than a wall of cards (`ADR-032`).
//
// It reads the same `/api/devices` payload the device page does, so the two cannot disagree about a
// drive. What differs is the shape: one row per drive, with the state and the score in one column,
// because a verdict and a number about the same drive are one fact, a separate "healthy" column and
// a separate ring said it twice and cost a column to do it.
import { el, clear, append, fmtBytes, fmtAgo, fmtInt, fmtTemp } from '../dom.js';
import { api, appPath } from '../api.js';

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

/** Reset the view's memory. Called when the app mounts, so a reload starts clean. */
export function reset() {
  lastPayload = null;
  lastCtx = null;
  filter = null;
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

function draw(content, ctx, payload) {
  lastCtx = ctx;
  const devices = (payload.devices || []).filter((d) => !d.hidden);
  const counts = countStates(devices);
  const rows = sortForReading(devices.filter(matchesFilter));

  clear(content);
  append(content, [
    el('div', { class: 'fleet-head' }, [
      el('h1', { text: 'Fleet' }),
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

    rows.length ? table(rows, ctx) : emptyState(devices.length),

    el('div', { class: 'fleet-below' }, [
      el('div', { text: `${devices.length} drive${devices.length === 1 ? '' : 's'}` }),
      el('div', { class: 'links' }, [
        el('a', { href: appPath('/schedules'), text: 'Schedules', onclick: nav(ctx, '/schedules') }),
        el('a', { href: appPath('/settings'), text: 'Settings', onclick: nav(ctx, '/settings') }),
      ]),
    ]),

  ]);
}

// --- pieces -----------------------------------------------------------------

function nav(ctx, path) {
  return (event) => {
    event.preventDefault();
    ctx.navigate(path);
  };
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
  return filter === null || stateOf(d) === filter;
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

function emptyState(total) {
  return el('div', { class: 'fleet-empty' }, [
    el('div', {
      text: total === 0
        ? 'No drives yet, press Scan to look for them.'
        : 'No drives match that filter.',
    }),
  ]);
}

function table(devices, ctx) {
  const head = el('thead', {}, [
    el('tr', {}, [
      el('th', { text: 'Drive' }),
      el('th', { text: 'Health' }),
      el('th', { class: 'usage-cell', text: 'Usage' }),
      el('th', { class: 'r temp-cell', text: 'Temp' }),
      el('th', { class: 'r age-cell', text: 'Powered on' }),
    ]),
  ]);

  const body = el('tbody', {});
  for (const d of devices) {
    append(body, [row(d, ctx)]);
  }

  return el('table', { class: 'fleet-table' }, [
    el('colgroup', {}, [
      el('col', { class: 'drive' }),
      el('col', { class: 'health' }),
      el('col', { class: 'usage' }),
      el('col', { class: 'temp' }),
      el('col', { class: 'age' }),
    ]),
    head,
    body,
  ]);
}

function row(d, ctx) {
  const state = stateOf(d);
  const classes = [];
  if (state === 'bad' || state === 'caution' || d.is_missing) classes.push('attention');
  if (d.is_missing) classes.push('missing');

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
  ]);
}

/**
 * State and score in one cell. The score is the interpreter's 0–100, and it sits beside the word
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
