// App shell: navigation, routing, SSE wiring, shortcuts (docs/UI.md §2, §8).
import { el, mount, clear, setTemperatureUnit as applyTemperatureUnit } from './dom.js';
import { icon } from './icons.js';
import { api, appPath } from './api.js';
import { toast, confirmDialog } from './components.js';
import * as dashboard from './views/dashboard.js';
import * as device from './views/device.js';
import * as schedules from './views/schedules.js';
import * as notifications from './views/notifications.js';
import * as settings from './views/settings.js';

const NAV = [
  ['/', 'Devices', 'dashboard'],
  ['/schedules', 'Schedules', 'schedule'],
  ['/notifications', 'Notifications', 'bell'],
  ['/settings', 'Settings', 'settings'],
];

const root = document.getElementById('app');
const sidebar = el('aside', { class: 'sidebar', id: 'sidebar' });
const content = el('main', { class: 'content', id: 'content' });

let scanning = false;

const ctx = {
  navigate(path) {
    history.pushState({}, '', appPath(path));
    sidebar.dataset.open = 'false';
    route();
  },
  openDevice(serial) {
    ctx.navigate(`/device/${encodeURIComponent(serial)}`);
  },
  refresh() {
    route();
  },
  // Display preferences the settings view can change without a reload.
  setTemperatureUnit: applyTemperatureUnit,
  async scan() {
    if (scanning) return;
    scanning = true;
    try {
      await api.scan();
      toast('Scan started');
    } catch (err) {
      toast(err.message, 'error');
      scanning = false;
    }
  },
  // Rescans one drive. Unlike a full scan this waits for the result, because one
  // drive is quick and the button that started it is right there.
  async scanDevice(serial) {
    try {
      await api.scanDevice(serial);
      toast('Drive rescanned', 'good');
      route();
    } catch (err) {
      // "device X is not connected" is the interesting case: the drive was unplugged
      // between the last scan and this button press.
      toast(err.message, 'error');
    }
  },
  // Removes a drive and everything derived from it. Only reachable through a
  // confirmation, because it is not undoable.
  async forgetDevice(serial) {
    const ok = await confirmDialog('Forget this drive',
      'This removes the drive, its history and its notes. A scan will not bring it ' +
      'back unless it is connected again. This cannot be undone.', 'Forget');
    if (!ok) return;
    try {
      await api.forgetDevice(serial);
      toast('Drive forgotten', 'good');
      ctx.navigate('/');
    } catch (err) {
      toast(err.message, 'error');
    }
  },
};

function currentPath() {
  const base = root.dataset.basePath || '';
  const path = location.pathname.slice(base.length) || '/';
  return path.startsWith('/') ? path : `/${path}`;
}

async function route() {
  const path = currentPath();
  renderSidebar(path);

  if (path.startsWith('/device/')) {
    await device.render(content, ctx, decodeURIComponent(path.slice('/device/'.length)));
  } else if (path === '/schedules') {
    await schedules.render(content, ctx);
  } else if (path === '/notifications') {
    await notifications.render(content, ctx);
  } else if (path === '/settings') {
    await settings.render(content, ctx);
  } else {
    await dashboard.render(content, ctx);
  }
}

function renderSidebar(path) {
  const items = NAV.map(([href, label, name]) => el('a', {
    class: 'nav-item',
    href: appPath(href),
    'aria-current': (href === '/' ? path === '/' : path.startsWith(href)) ? 'page' : null,
    onclick: (event) => {
      event.preventDefault();
      ctx.navigate(href);
    },
  }, [icon(name), label]));

  clear(sidebar);
  sidebar.append(
    el('div', { class: 'brand' }, [
      icon('device', 'brand-mark'),
      el('div', {}, [
        el('div', { class: 'brand-name', text: 'Smart-Serve' }),
        el('div', { class: 'brand-version', text: root.dataset.version || 'dev' }),
      ]),
    ]),
    el('nav', { class: 'nav', 'aria-label': 'Primary' }, items),
    el('div', { class: 'sidebar-foot' }, [
      el('button', { class: 'btn btn-primary', type: 'button', onclick: () => ctx.scan() },
        [icon('scan'), 'Scan now']),
      el('div', { class: 'sidebar-note' }, [
        el('div', { text: `Smart-Serve ${root.dataset.version || 'dev'}` }),
        el('div', { text: 'Data reflects the last scan.' }),
        el('div', { text: 'S.M.A.R.T. via smartmontools — the app never writes to a drive.' }),
      ]),
    ]),
  );
}

function buildShell() {
  // The topbar exists for the mobile navigation toggle; on desktop the sidebar is
  // always visible and the CSS hides the bar entirely, so it holds nothing else.
  const topbar = el('header', { class: 'topbar' }, [
    el('button', {
      class: 'btn btn-ghost btn-sm topbar-toggle',
      type: 'button',
      'aria-label': 'Toggle navigation',
      onclick: () => {
        sidebar.dataset.open = sidebar.dataset.open === 'true' ? 'false' : 'true';
      },
    }, [icon('menu')]),
  ]);

  mount(root, el('div', { class: 'app' }, [
    sidebar,
    el('div', { class: 'main' }, [topbar, content]),
  ]));
}

// --- SSE (docs/API.md) --------------------------------------------------------

function connectEvents() {
  const source = new EventSource(api.eventsUrl());
  source.addEventListener('scan_started', () => {
    scanning = true;
  });
  source.addEventListener('scan_complete', (event) => {
    scanning = false;
    let data = {};
    try {
      data = JSON.parse(event.data);
    } catch {
      /* ignore malformed payloads */
    }
    if (data.ok === false) {
      toast(`Scan failed: ${data.error || 'unknown error'}`, 'error');
    } else {
      toast(`Scan complete: ${data.devices_ok}/${data.devices_total} readable`, 'good');
    }
    route();
  });
  // EventSource reconnects automatically; nothing to do on error.
}

// --- keyboard shortcuts (docs/UI.md §8) ---------------------------------------

function bindShortcuts() {
  document.addEventListener('keydown', (event) => {
    const tag = (event.target.tagName || '').toLowerCase();
    if (tag === 'input' || tag === 'textarea' || tag === 'select') return;
    // A modal owns the keyboard while it is open. Starting a full scan behind a
    // dialog the user is still reading is never what they meant.
    if (document.querySelector('dialog[open]')) return;
    if (event.key === 's') {
      event.preventDefault();
      ctx.scan();
    }
    if (event.key === '/') {
      const search = document.getElementById('device-search');
      if (search) {
        event.preventDefault();
        search.focus();
      }
    }
  });
}

window.addEventListener('popstate', route);

// Display preferences are read before the first render, so temperatures never
// flash as Celsius for someone who picked Fahrenheit.
async function loadDisplayPreferences() {
  try {
    const data = await api.settings();
    applyTemperatureUnit(data && data.settings && data.settings.temperature_unit);
  } catch {
    /* keep the defaults; the first view will report the real error */
  }
}

async function boot() {
  await loadDisplayPreferences();
  buildShell();
  await route();
  connectEvents();
  bindShortcuts();
}

boot();
