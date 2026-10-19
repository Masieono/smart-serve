// Dashboard: device cards, health summary, scan button (docs/UI.md §7.1).
import { el, mount, fmtAgo } from '../dom.js';
import { api } from '../api.js';
import { deviceCard, fleetTable, deviceTitle, countText, healthFilter, emptyState, skeletonCards, errorBox, transportLabel } from '../components.js';
import { icon } from '../icons.js';

// Module-level so the choices survive navigating into a drive and back.
let filter = null;
let query = '';
let showMissing = false;
let transportFilter = '';
let sortKey = 'health';
let ascending = true;
let view = 'cards';
let grouped = false;

// Worst first. The rank is what makes "sort by risk" mean something: the server's own ORDER BY
// uses the same order, and this is the client's copy of it for the sorts below.
const HEALTH_RANK = { bad: 0, caution: 1, unknown: 2, good: 3 };

/**
 * The sort options, each with the direction that reads as "most important first".
 *
 * The default direction is per-field rather than one global toggle, because "first" means
 * different things: someone sorting by capacity wants the biggest drive at the top, and someone
 * sorting by name wants A. A single toggle makes the control feel broken on first use whichever
 * way it is set.
 */
const SORTS = [
  { key: 'health', label: 'Risk', ascending: true,
    value: (d) => HEALTH_RANK[d.health] ?? 9 },
  { key: 'name', label: 'Name', ascending: true, value: deviceTitle },
  { key: 'bay', label: 'Bay', ascending: true, value: (d) => d.bay || '' },
  { key: 'model', label: 'Model', ascending: true, value: (d) => d.model || '' },
  { key: 'capacity', label: 'Capacity', ascending: false, value: (d) => d.capacity_bytes ?? -1 },
  { key: 'temperature', label: 'Temperature', ascending: false, value: (d) => d.temperature_c ?? -1 },
  { key: 'power_on_hours', label: 'Age', ascending: false, value: (d) => d.power_on_hours ?? -1 },
  { key: 'last_scan', label: 'Last scan', ascending: false, value: (d) => d.last_scan || '' },
  { key: 'score', label: 'Score', ascending: true,
    // A drive with no score is not the best-scoring drive in the fleet. -1 means "not scored",
    // so it sorts last rather than first.
    value: (d) => (typeof d.score === 'number' && d.score >= 0 ? d.score : 999) },
  { key: 'transport', label: 'Transport', ascending: true, value: (d) => d.transport || '' },
];

function nameOf(d) {
  return deviceTitle(d);
}

/** Compare two sort values: numbers numerically, strings case-insensitively. */
function cmp(a, b) {
  if (typeof a === 'number' && typeof b === 'number') return a - b;
  return String(a).localeCompare(String(b), undefined, { sensitivity: 'base' });
}

/**
 * Sort a copy of the list.
 *
 * Ties always break on name, ascending, so the order is stable and a rack of identical drives
 * does not shuffle between renders. That matters more than it sounds: a list that reorders itself
 * when nothing has changed makes you re-read it every time you look.
 */
function sortBy(list, key, ascending) {
  const spec = SORTS.find((s) => s.key === key) || SORTS[0];
  const dir = ascending ? 1 : -1;
  return [...list].sort((a, b) => {
    const byValue = cmp(spec.value(a), spec.value(b));
    return byValue !== 0 ? byValue * dir : cmp(nameOf(a), nameOf(b));
  });
}

/**
 * Split the visible drives into tag groups (`FEATURES.md` D9).
 *
 * A drive appears under **each** of its tags. The alternative — first tag wins — silently drops a
 * drive from a group it is tagged for, and a grouping that hides a drive is worse than one that
 * shows it twice: the whole reason to group a fleet is not to miss one. The cost is that the group
 * counts do not sum to the fleet size, which the header says outright rather than leaving to be
 * discovered.
 *
 * Untagged drives come last, because they are the ones with something left to do to them.
 *
 * Deliberately not a filter. `FEATURES.md` draws that distinction, and it is the right one: a
 * filter answers "show me only this", which the health chips and the transport select already do,
 * while grouping answers "let me see the shape of the fleet".
 */
export function groupByTag(shown) {
  const groups = new Map();
  const untagged = [];

  for (const d of shown) {
    const tags = String(d.tags || '').split(',').map((t) => t.trim()).filter(Boolean);
    if (!tags.length) {
      untagged.push(d);
      continue;
    }
    for (const tag of tags) {
      if (!groups.has(tag)) groups.set(tag, []);
      groups.get(tag).push(d);
    }
  }

  const out = [...groups.entries()]
    .sort((a, b) => a[0].localeCompare(b[0], undefined, { sensitivity: 'base' }))
    .map(([label, devices]) => ({ label, devices }));
  if (untagged.length) out.push({ label: 'Untagged', devices: untagged });
  return out;
}

/**
 * The drives to show, in the order to show them (`FEATURES.md` D5).
 *
 * Exported and pure, because this *is* the feature: the filtering and ordering rules are what D5
 * added, and keeping them out of the render means they can be tested without a DOM, a fetch, or a
 * card. Every option defaults to "show everything, worst first", so a caller can pass only what
 * it means to change.
 */
export function visibleDevices(all, opts = {}) {
  const {
    health = null, query = '', transport = '', showMissing = false, sortKey = 'health',
  } = opts;

  const spec = SORTS.find((s) => s.key === sortKey) || SORTS[0];
  // A caller may override the direction, but the *field's* own idea of "first" is the default.
  // That keeps the rule in one place: a caller that only names a field cannot get it backwards,
  // and the toolbar reads its default from the same row of the table.
  const ascending = opts.ascending === undefined ? spec.ascending : opts.ascending;

  // A drive that has been unplugged is kept (its notes and history matter) but is flagged
  // is_missing. Hidden unless asked for.
  const pool = showMissing ? all : all.filter((d) => !d.is_missing);
  const needle = String(query).trim().toLowerCase();

  const shown = pool.filter((d) => {
    if (transport && (d.transport || '') !== transport) return false;
    if (health && d.health !== health) return false;
    if (!needle) return true;
    // Bay is searchable, because hunting a drive by where it physically is is the whole point of
    // recording one.
    return [d.name, d.model, d.serial, d.device_path, d.bay, (d.mountpoints || []).join(' ')]
      .filter(Boolean)
      .some((field) => String(field).toLowerCase().includes(needle));
  });

  return sortBy(shown, sortKey, ascending);
}

export async function render(container, ctx) {
  mount(container, skeletonCards(3));
  try {
    draw(container, ctx, await api.devices());
  } catch (err) {
    mount(container, errorBox(`Could not load devices: ${err.message}`));
  }
}

function draw(container, ctx, data) {
  const all = (data.devices || []).filter((d) => !d.hidden);
  // A drive that has been unplugged is kept (its notes and history matter) but
  // is flagged is_missing. Hide those from the dashboard unless asked for.
  const connected = all.filter((d) => !d.is_missing);
  const missing = all.filter((d) => d.is_missing);

  const counts = { good: 0, caution: 0, bad: 0, unknown: 0 };
  for (const d of connected) counts[d.health] = (counts[d.health] || 0) + 1;

  const search = el('input', {
    type: 'search',
    id: 'device-search',
    class: 'devices-search',
    placeholder: 'Filter by name, model or serial  (/)',
    value: query,
    oninput: () => {
      query = search.value.trim().toLowerCase();
      paint();
    },
  });

  // One rectangle for the page identity, the health counts and the search box. The
  // counts are a filter for the grid below, so they belong beside the thing they
  // filter rather than in a row of their own.
  //
  // The transports actually present, rather than a vocabulary this app invented: a host with no
  // SAS drives should not be offered "SAS" and then show nothing when it is picked.
  const transportOptions = [...new Set(all.map((d) => d.transport || ''))].sort()
    .map((t) => ({ value: t, label: transportLabel({ transport: t }) }));

  const sortSelect = el('select', {
    title: 'Order the list',
    onchange: () => {
      sortKey = sortSelect.value;
      // Each field carries its own idea of "first" — see SORTS.
      ascending = (SORTS.find((s) => s.key === sortKey) || SORTS[0]).ascending;
      draw(container, ctx, data);
    },
  }, SORTS.map((s) => el('option', { value: s.key, text: s.label, selected: s.key === sortKey })));

  const direction = el('button', {
    class: 'btn btn-sm btn-ghost',
    type: 'button',
    title: ascending ? 'Ascending — click for descending' : 'Descending — click for ascending',
    'aria-label': ascending ? 'Sort ascending' : 'Sort descending',
    onclick: () => {
      ascending = !ascending;
      draw(container, ctx, data);
    },
  }, [ascending ? '↑' : '↓']);

  /** One half of the cards/table switch. */
  function viewButton(key, label) {
    return el('button', {
      class: 'btn btn-sm',
      type: 'button',
      'aria-pressed': view === key ? 'true' : 'false',
      onclick: () => {
        view = key;
        draw(container, ctx, data);
      },
    }, [label]);
  }

  const transportSelect = transportOptions.length > 1
    ? el('select', {
        title: 'Show only one kind of connection',
        onchange: () => {
          transportFilter = transportSelect.value;
          paint();
        },
      }, [
        el('option', { value: '', text: 'All transports', selected: transportFilter === '' }),
        ...transportOptions.map((t) => el('option', {
          value: t.value, text: t.label, selected: t.value === transportFilter,
        })),
      ])
    : null;

  // Only offered when something is actually tagged: a single "Untagged" group is not a grouping,
  // it is the list with a heading on it.
  const canGroup = all.some((d) => String(d.tags || '').trim() !== '');

  const head = el('div', { class: 'card devices-head' }, [
    el('div', { class: 'devices-head-row' }, [
      el('h1', { text: 'Devices' }),
      healthFilter(counts, filter, (next) => {
        filter = next;
        draw(container, ctx, data);
      }),
      el('span', { class: 'spacer' }),
      search,
      // No scan button here: the sidebar carries the global one, and every card has
      // its own rescan. A third copy in the header was pure duplication.
      missing.length
        ? el('button', {
            class: 'btn btn-sm',
            type: 'button',
            'aria-pressed': showMissing ? 'true' : 'false',
            title: 'Drives recorded earlier that were not present in the last scan',
            onclick: () => {
              showMissing = !showMissing;
              draw(container, ctx, data);
            },
          }, [
            icon('help'),
            showMissing ? `Hide ${missing.length} disconnected` : `Show ${missing.length} disconnected`,
          ])
        : null,
    ]),
    // Sorting and filtering one drive is meaningless, so the row is only drawn when there is a
    // choice to make.
    all.length > 1
      ? el('div', { class: 'devices-controls' }, [
          el('span', { class: 'muted', text: 'Sort' }),
          sortSelect,
          direction,
          transportSelect,
          // Cards for a handful of drives, a table for a rack. It sits at the far end because it
          // changes the shape of the answer rather than what is being asked.
          el('span', { class: 'spacer' }),
          canGroup
            ? el('button', {
                class: 'btn btn-sm',
                type: 'button',
                'aria-pressed': grouped ? 'true' : 'false',
                title: 'Group the list by tag, so a pool reads as one block',
                onclick: () => {
                  grouped = !grouped;
                  draw(container, ctx, data);
                },
              }, ['Group by tag'])
            : null,
          viewButton('cards', 'Cards'),
          viewButton('table', 'Table'),
        ])
      : null,
    el('div', { class: 'devices-head-sub muted', text: `Last scan ${fmtAgo(data.last_scan_ts)}` }),
    // Stated rather than left to be discovered, because a reader who notices the counts do not add
    // up will otherwise conclude the app is wrong.
    grouped
      ? el('div', { class: 'devices-head-sub muted', text: 'A drive with more than one tag appears ' +
          'under each of them, so the group counts can add up to more than the fleet size.' })
      : null,
    el('div', { class: 'devices-head-sub muted', text: 'Rescan a single drive from its card. Open a ' +
      'drive that is no longer connected to forget it entirely.' }),
  ]);

  if (!all.length) {
    mount(container, head, emptyState(
      'No drives discovered yet',
      'Press Scan to enumerate every device visible to the host, internal and USB.',
      el('button', { class: 'btn btn-primary', type: 'button',
        onclick: () => ctx.scan() }, [icon('scan'), 'Scan now']),
    ));
    return;
  }

  const grid = el('div', { class: 'card-grid' });

  function paint() {
    const shown = visibleDevices(all, {
      health: filter, query, transport: transportFilter, showMissing, sortKey, ascending,
    });
    const groups = grouped && shown.length ? groupByTag(shown) : null;

    // The holder is a card grid, a grouped stack or a table holder — never a mix. A table inside a
    // grid cell is laid out as a single column and loses the columns it exists for, and a card
    // grid inside a card grid inherits the wrong column sizing.
    grid.className = view === 'table' ? 'fleet-holder' : (groups ? 'grouped-holder' : 'card-grid');

    if (!shown.length) {
      mount(grid, emptyState('Nothing to show', 'No devices match the current filter.'));
      return;
    }

    const cardFor = (d) => deviceCard(d, ctx.openDevice, () => ctx.scanDevice(d.serial));

    if (view === 'table') {
      mount(grid, fleetTable(shown, {
        sortKey, ascending, groups, onOpen: ctx.openDevice,
        onScan: (serial) => ctx.scanDevice(serial),
        onSort: sortFromHeader,
      }));
      return;
    }

    mount(grid, groups
      ? groups.flatMap((g) => [
          el('div', { class: 'group-head' }, [
            el('h2', { text: g.label }),
            el('span', { class: 'muted', text: countText(g.devices.length) }),
          ]),
          el('div', { class: 'card-grid' }, g.devices.map(cardFor)),
        ])
      : shown.map(cardFor));
  }

  function sortFromHeader(key) {
    // Clicking the column already sorted reverses it; clicking another starts from that field's
    // own idea of "first", the same default the select uses.
    ascending = key === sortKey
      ? !ascending
      : (SORTS.find((s) => s.key === key) || SORTS[0]).ascending;
    sortKey = key;
    draw(container, ctx, data);
  }
  paint();

  mount(container, head, grid);
}

