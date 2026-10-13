// Dashboard: device cards, health summary, scan button (docs/UI.md §7.1).
import { el, mount, fmtAgo } from '../dom.js';
import { api } from '../api.js';
import { deviceCard, healthFilter, emptyState, skeletonCards, errorBox } from '../components.js';
import { icon } from '../icons.js';

let filter = null;
let query = '';
let showMissing = false;

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
    el('div', { class: 'devices-head-sub muted', text: `Last scan ${fmtAgo(data.last_scan_ts)}` }),
    el('div', { class: 'devices-head-sub muted', text: 'Rescan a single drive from its card. Open a ' +
      'drive that is no longer connected to forget it entirely.' }),
  ]);

  if (!all.length) {
    mount(container, head, emptyState(
      'No drives discovered yet',
      'Press Scan to enumerate every device visible to the host — internal and USB.',
      el('button', { class: 'btn btn-primary', type: 'button', style: 'margin-top:var(--space-4)',
        onclick: () => ctx.scan() }, [icon('scan'), 'Scan now']),
    ));
    return;
  }

  const grid = el('div', { class: 'card-grid' });

  function paint() {
    const pool = showMissing ? all : connected;
    const shown = pool.filter((d) => {
      if (filter && d.health !== filter) return false;
      if (!query) return true;
      return [d.name, d.model, d.serial, d.device_path, (d.mountpoints || []).join(' ')]
        .filter(Boolean)
        .some((field) => String(field).toLowerCase().includes(query));
    });
    mount(grid, shown.length
      ? shown.map((d) => deviceCard(d, ctx.openDevice, () => ctx.scanDevice(d.serial)))
      : [emptyState('Nothing to show', 'No devices match the current filter.')]);
  }
  paint();

  mount(container, head, grid);
}

