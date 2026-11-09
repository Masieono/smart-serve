// App shell: navigation, routing, SSE wiring, shortcuts (docs/UI.md §2, §8).
import { el, mount, clear, holdHeight, scrollToTop, setTemperatureUnit as applyTemperatureUnit } from './dom.js';
import { icon } from './icons.js';
import { api, appPath } from './api.js';
import { toast, confirmDialog } from './components.js';
import * as dashboard from './views/dashboard.js';
import * as device from './views/device.js';
import * as schedules from './views/schedules.js';
import * as notifications from './views/notifications.js';
import * as settings from './views/settings.js';
import { setPollSeconds as setSelfTestRefresh } from './views/selftest.js';
import { setDisplayOptions } from './views/device.js';

// The sidebar. A fourth element on an entry means it is a *group*: the pages underneath it are
// listed when you are in that part of the app, and only then. Settings is the first one, because its
// sections are each a page rather than a stack of collapsible headings — a dozen controls on one
// screen is a wall, and a dozen controls split four ways is four short pages.
const NAV = [
  ['/', 'Devices', 'dashboard'],
  ['/schedules', 'Schedules', 'schedule'],
  ['/notifications', 'Notifications', 'bell'],
  ['/settings', 'Settings', 'settings', [
    ['/settings/thresholds', 'Thresholds'],
    ['/settings/notifications', 'Notifications'],
    ['/settings/appearance', 'Appearance'],
    ['/settings/data', 'Data'],
  ]],
];

const root = document.getElementById('app');
const sidebar = el('aside', { class: 'sidebar', id: 'sidebar' });
const content = el('main', { class: 'content', id: 'content' });

let scanning = false;
// Set by a view that must be asked before it is left. The settings pages use it for unsaved edits;
// every other view leaves it null. Cleared on each route, so a guard cannot outlive its view.
let navGuard = null;

const ctx = {
  async navigate(path) {
    // Asked before the history entry is written, so a refused navigation leaves no trace to go back
    // to — a "no" that still changed the URL would be worse than not asking.
    if (navGuard && !(await navGuard(path))) return;
    history.pushState({}, '', appPath(path));
    sidebar.dataset.open = 'false';
    route();
  },
  /**
   * Ask before this view is navigated away from.
   *
   * The guard returns true to allow the navigation. A view that sets one must also clear it when its
   * edits are saved, or the reader is asked about changes that no longer exist.
   */
  setNavGuard(fn) {
    navGuard = fn;
  },
  openDevice(serial) {
    ctx.navigate(`/device/${encodeURIComponent(serial)}`);
  },
  refresh() {
    // A refresh re-renders the view that is already on screen, so the reader keeps their place
    // (`route`). Navigation is the case that starts at the top.
    route({ keepScroll: true });
  },
  // Display preferences the settings view can change without a reload.
  setTemperatureUnit: applyTemperatureUnit,
  setDisplayOptions,
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
      // The button that started this is on the card being rescanned, so the reader is looking at
      // that part of the page: losing their place would be the app's fault, not their scroll.
      route({ keepScroll: true });
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

/**
 * Render the view for the current path.
 *
 * `keepScroll` is for a re-render of the view that is *already on screen* — a refresh, a rescan, a
 * scan finishing — where the reader's place has to survive it. Navigation passes nothing, because a
 * page you have just asked for should start at the top.
 */
async function route({ keepScroll = false } = {}) {
  const path = currentPath();
  // Cleared before the view renders: a guard belongs to the view that set it, and a view that wants
  // one sets it again on the way in.
  navGuard = null;
  renderSidebar(path);
  // A navigation starts at the top; a re-render keeps the reader where they were. Nothing else does
  // this — see `scrollToTop`, and the shell comment in `UI.md` §2.
  if (!keepScroll) scrollToTop();

  // Every view mounts a skeleton and then awaits its data, so the container is empty for the length
  // of the request and the browser clamps the scroll position to the top of a very short page.
  // Holding the old height across the swap is what keeps the reader where they were.
  const release = keepScroll ? holdHeight(content) : null;
  try {
    if (path.startsWith('/device/')) {
      await device.render(content, ctx, decodeURIComponent(path.slice('/device/'.length)));
    } else if (path === '/schedules') {
      await schedules.render(content, ctx);
    } else if (path === '/notifications') {
      await notifications.render(content, ctx);
    } else if (path === '/settings' || path.startsWith('/settings/')) {
      // `/settings` is the section, not a page: it renders the first one. That keeps the plain
      // sidebar entry working and means a bookmarked `/settings` is never a dead end.
      await settings.render(content, ctx, path.slice('/settings/'.length));
    } else {
      await dashboard.render(content, ctx);
    }
  } finally {
    if (release) release();
  }
}

function renderSidebar(path) {
  const items = NAV.flatMap(([href, label, name, children]) => {
    const here = href === '/' ? path === '/' : path === href || path.startsWith(`${href}/`);
    const link = el('a', {
      class: 'nav-item',
      href: appPath(href),
      // Only the exact page is `page`. A section whose child is current is *marked*, not claimed —
      // saying "you are here" about a link that goes somewhere else is worse than saying nothing.
      'aria-current': (href === '/' ? path === '/' : path === href) ? 'page' : null,
      onclick: (event) => {
        event.preventDefault();
        ctx.navigate(href);
      },
    }, [icon(name), label]);

    if (!children) return [link];

    // The sub-pages of the section you are in, and only then. Ten permanent entries would make the
    // sidebar as long as the page the sections were split to shorten.
    return [
      el('div', { class: here ? 'nav-group nav-group-active' : 'nav-group' }, [link]),
      ...(here ? [el('div', { class: 'nav-children' }, children.map(([subHref, subLabel]) =>
        el('a', {
          class: 'nav-item nav-sub',
          href: appPath(subHref),
          'aria-current': path === subHref ? 'page' : null,
          onclick: (event) => {
            event.preventDefault();
            ctx.navigate(subHref);
          },
        }, [subLabel])))] : []),
    ];
  });

  clear(sidebar);
  sidebar.append(
    // The wordmark is the way home, which is what a wordmark in a corner is for. It is a
    // button rather than an <a> because every navigation in this shell goes through
    // ctx.navigate, which owns the base path — a raw href would have to interpolate it and
    // get it wrong behind a proxy (ADR-015).
    el('button', {
      class: 'brand', type: 'button', title: 'Back to devices',
      'aria-label': 'Smart-Serve, back to devices',
      onclick: () => ctx.navigate('/'),
    }, [
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
        el('div', { text: 'Data reflects the last scan.' }),
        el('div', { text: 'S.M.A.R.T. via smartmontools. The app never writes to a drive.' }),
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
    // A scan finishing is news about the fleet, not a navigation: the reader was doing something
    // else, and yanking them to the top of the page to tell them is the wrong interruption.
    route({ keepScroll: true });
  });
  // A drive's self-test progress changed (`FEATURES.md` A20). The watcher re-reads only drives that
  // are mid-test, and publishes this when something actually moved — so the badge tracks a running
  // test instead of freezing at whatever the last scan caught.
  source.addEventListener('selftest_progress', () => {
    route({ keepScroll: true });
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
    const stored = (data && data.settings) || {};
    applyTemperatureUnit(stored.temperature_unit);
    // Same reason as the temperature unit, and the same place: a list that flashed cards and then
    // became a table would be worse than either one on its own.
    dashboard.setView(stored.dashboard_view);
    // And again: a card that polls should start at the interval the user chose, not at the default
    // and then change under them.
    setSelfTestRefresh(stored.selftest_poll_seconds);
    // A28: the partition table and its usage column, read here for the same reason as the rest —
    // a card that appeared and then vanished on first load is worse than either state.
    setDisplayOptions(stored);
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
