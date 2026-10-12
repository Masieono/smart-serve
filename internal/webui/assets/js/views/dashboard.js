// Dashboard: device cards, health summary, scan button (docs/UI.md §7.1).
import { el, mount, fmtAgo } from '../dom.js';
import { api } from '../api.js';
import { deviceCard, summaryBar, emptyState, skeletonCards, errorBox } from '../components.js';
import { icon } from '../icons.js';

let filter = null;
let query = '';

export async function render(container, ctx) {
  mount(container, skeletonCards(3));
  try {
    draw(container, ctx, await api.devices());
  } catch (err) {
    mount(container, errorBox(`Could not load devices: ${err.message}`));
  }
}

function draw(container, ctx, data) {
  const devices = (data.devices || []).filter((d) => !d.hidden);
  const counts = { good: 0, caution: 0, bad: 0, unknown: 0 };
  for (const d of devices) counts[d.health] = (counts[d.health] || 0) + 1;

  const search = el('input', {
    type: 'search',
    id: 'device-search',
    placeholder: 'Filter by name, model or serial  (/)',
    value: query,
    style: 'max-width:280px',
    oninput: () => {
      query = search.value.trim().toLowerCase();
      paint();
    },
  });

  const head = el('div', { class: 'page-head' }, [
    el('div', {}, [
      el('h1', { text: 'Devices' }),
      el('div', { class: 'sub', text: `Last scan ${fmtAgo(data.last_scan_ts)}` }),
    ]),
    el('span', { class: 'spacer' }),
    search,
    el('button', { class: 'btn', type: 'button', onclick: () => ctx.scan() }, [icon('scan'), 'Scan now']),
  ]);

  if (!devices.length) {
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
    const shown = devices.filter((d) => {
      if (filter && d.health !== filter) return false;
      if (!query) return true;
      return [d.name, d.model, d.serial, d.device_path, (d.mountpoints || []).join(' ')]
        .filter(Boolean)
        .some((field) => String(field).toLowerCase().includes(query));
    });
    mount(grid, shown.length
      ? shown.map((d) => deviceCard(d, ctx.openDevice, () => ctx.scan()))
      : [emptyState('Nothing to show', 'No devices match the current filter.')]);
  }
  paint();

  mount(container,
    head,
    summaryBar(counts, filter, (next) => {
      filter = next;
      draw(container, ctx, data);
    }),
    grid);
}

