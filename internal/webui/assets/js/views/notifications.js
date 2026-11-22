// Notifications (v2), channels, what they are told about, and whether the telling worked.
//
// The page's job is not "manage channels"; it is "be sure I would hear about a failing drive". That
// ordering decides the layout: the failure state comes first, the channels are rows you scan, and the
// delivery log is a feed you read when something did not arrive.
//
// The channel editor and the maintenance-window editor are the incumbent's dialogs, imported rather
// than rebuilt (`ADR-032`): they carry per-provider field specs, secret masking, test-send against
// unsaved config and the env-var hints, and not one of those is a layout decision.
import { el, clear, append, mount, holdHeight, fmtAgo, fmtDateTime } from '../dom.js';
import { api } from '../api.js';
import { emptyState, errorBox, toast, confirmDialog } from '../components.js';
import { icon } from '../icons.js';
import { fleetTags } from '../tags.js';
import {
  parseWindows, windowState, describeWindow, isRecurring,
  editChannel, editWindow, saveWindows, labelForSerial,
} from './notification-editors.js';

const FALLBACK_EVENTS = [
  'health_change', 'temperature_high', 'device_disappeared', 'device_appeared',
  'scan_failure', 'self_test_result', 'scan_success', 'group_heat',
];

// The page's data, kept so a single delivery can be re-read without refetching the fleet.
let state = null;
let host = null;
let hostCtx = null;

export async function render(container, ctx) {
  host = container;
  hostCtx = ctx;
  clear(container);
  mount(container, el('div', { class: 'skeleton', style: 'width:30%;height:24px' }));

  try {
    const [channels, log, settings, devices] = await Promise.all([
      api.channels(),
      api.notifications(25),
      api.settings(),
      api.devices(),
    ]);
    state = { channels, log, settings, devices };
  } catch (err) {
    clear(container);
    mount(container, errorBox(`Could not load notifications: ${err.message}`));
    return;
  }

  draw(container, ctx);
}

/**
 * Re-read the delivery log and redraw in place.
 *
 * A test send is the one action whose *result* appears somewhere else on the page, and a person who
 * clicks Test and sees nothing change reasonably concludes nothing happened. The redraw holds the
 * page height across the swap so the reader does not jump to the top of a long page.
 */
async function refreshLog() {
  if (!host || !state) return;
  let log;
  try {
    log = await api.notifications(25);
  } catch {
    return;
  }
  state = { ...state, log };
  const y = window.scrollY;
  const release = holdHeight(host);
  draw(host, hostCtx);
  release();
  window.scrollTo(0, y);
}

function draw(container, ctx) {
  const { channels: channelData, log, settings, devices: deviceData } = state || {};
  const channels = (channelData && channelData.channels) || [];
  const devices = ((deviceData && deviceData.devices) || []).filter((d) => d.serial);
  const eventTypes = (log && log.event_types) || FALLBACK_EVENTS;
  const windows = parseWindows(settings && settings.settings && settings.settings.maintenance_windows);
  const entries = (log && log.notifications) || [];

  const catalog = {
    eventTypes,
    serverTypes: (log && log.channel_types) || null,
    envVarList: (log && log.channel_env_vars) || null,
    groups: fleetTags(devices),
  };

  clear(container);
  append(container, [
    el('div', { class: 'fleet-head' }, [
      el('h1', { text: 'Notifications' }),
      el('button', {
        class: 'fleet-scan',
        type: 'button',
        text: 'New channel',
        onclick: () => editChannel(ctx, null, catalog),
      }),
    ]),
    el('div', { class: 'fleet-glance' }, statusLine(channels, entries)),

    failureBanner(entries, channels),

    channels.length
      ? channelTable(channels, ctx, catalog)
      : el('div', { class: 'fleet-empty', text: 'No channels yet, add one and a failing drive will tell you about it.' }),

    deliveryFeed(entries, channels),
    windowList(ctx, windows, devices),
  ].filter(Boolean));
}

/** One line under the title: how many channels, how many events, and how the last delivery went. */
function statusLine(channels, entries) {
  const enabled = channels.filter((c) => c.enabled).length;
  const last = entries[0];
  const parts = [
    el('span', { class: 'count' }, [el('b', { text: String(channels.length) }), el('span', { text: ' channels' })]),
    el('span', { class: 'count' }, [el('b', { text: String(enabled) }), el('span', { text: ' enabled' })]),
    el('span', { class: 'count' }, [
      el('b', { text: String(eventTypesCount(channels)) }),
      el('span', { text: ' event types routed' }),
    ]),
  ];
  if (last) {
    parts.push(el('span', { class: 'when', text: `last delivery ${fmtAgo(last.sent_at)}` }));
  } else {
    parts.push(el('span', { class: 'when', text: 'no deliveries yet' }));
  }
  return parts;
}

function eventTypesCount(channels) {
  // An empty subscription means every event, so a channel with no explicit list routes all of them.
  const all = new Set();
  for (const c of channels) {
    const list = parseList(c.events);
    if (!list.length) return FALLBACK_EVENTS.length;
    list.forEach((e) => all.add(e));
  }
  return all.size;
}

/**
 * A delivery that failed is the one thing on this page that has to be impossible to miss.
 *
 * Notifications fail silently by nature, the message simply does not arrive, so a page that only
 * records the failure in a log nobody opens is a page that reports success by omission.
 */
function failureBanner(entries, channels) {
  const failed = entries.filter((e) => e.status !== 'sent' && e.status !== 'squelched');
  if (!failed.length) return null;

  const names = [...new Set(failed.map((e) => labelFor(e.channel_id, channels)))];
  const latest = failed[0];

  return el('div', { class: 'v2-alert' }, [
    el('div', { class: 'v2-alert-title' }, [
      `${failed.length} recent deliver${failed.length === 1 ? 'y' : 'ies'} failed`,
    ]),
    el('div', { class: 'v2-alert-body' }, [
      `${names.join(', ')}, last at ${fmtAgo(latest.sent_at)}. `,
      latest.detail ? `${latest.detail}. ` : '',
      'A channel that cannot deliver is a drive nobody is watching.',
    ]),
  ]);
}

function channelTable(channels, ctx, catalog) {
  const body = el('tbody', {});
  for (const c of channels) {
    const events = parseList(c.events);
    const scope = parseList(c.scope_groups);
    append(body, [el('tr', { class: c.enabled ? null : 'missing' }, [
      el('td', { class: 'drive' }, [
        el('div', { class: 'name', text: c.label || c.type }),
        el('div', { class: 'sub', text: [c.type, c.enabled ? '' : 'disabled'].filter(Boolean).join(' · ') }),
      ]),
      el('td', {}, [
        el('div', { class: 'v2-chips' }, events.length
          ? events.map((e) => el('span', { class: 'v2-chip', text: e }))
          // The chip is short; the explanation the incumbent carried survives as a tooltip rather
          // than disappearing. An empty subscription is not "nothing", it is everything, including
          // the scheduled-scan heartbeat and group heat, which are the events nobody remembers to tick.
          : [el('span', {
            class: 'v2-chip',
            text: 'Every event',
            title: 'every alert and digest, the scheduled-scan heartbeat and group heat included',
          })]),
      ]),
      el('td', {}, [
        el('div', { class: 'v2-chips' }, scope.length
          ? scope.map((g) => el('span', { class: 'v2-chip', text: g }))
          : [el('span', { class: 'v2-chip', text: 'Every group' })]),
      ]),
      el('td', { class: 'r' }, [
        el('div', { class: 'v2-row-actions' }, [
          el('button', {
            class: 'v2-btn',
            type: 'button',
            text: 'Test',
            onclick: async (event) => {
              const b = event.currentTarget;
              b.disabled = true;
              b.textContent = 'Sending…';
              try {
                await api.testChannel(c.id);
                toast('Test event sent', 'good');
              } catch (err) {
                toast(`Test failed: ${err.message}`, 'error');
              } finally {
                // Either way the attempt is a delivery, and the log is where it shows up.
                await refreshLog();
                b.disabled = false;
                b.textContent = 'Test';
              }
            },
          }),
          el('button', {
            class: 'v2-btn',
            type: 'button',
            text: 'Edit',
            onclick: () => editChannel(ctx, c, catalog),
          }),
          el('button', {
            class: 'v2-btn',
            type: 'button',
            text: 'Delete',
            onclick: async () => {
              if (!(await confirmDialog('Delete channel', `Delete "${c.label || c.type}"?`, 'Delete'))) return;
              try {
                await api.deleteChannel(c.id);
                toast('Channel deleted', 'good');
                ctx.refresh();
              } catch (err) {
                toast(err.message, 'error');
              }
            },
          }),
        ]),
      ]),
    ])]);
  }

  return el('div', { class: 'v2-table-wrap' }, [
    el('table', { class: 'fleet-table v2-channels' }, [
      el('colgroup', {}, [
        el('col', { class: 'drive' }), el('col', { class: 'events' }),
        el('col', { class: 'scope' }), el('col', { class: 'actions' }),
      ]),
      el('thead', {}, [el('tr', {}, [
        el('th', { text: 'Channel' }),
        el('th', { text: 'Told about' }),
        el('th', { text: 'Drives' }),
        el('th', { class: 'r', text: '' }),
      ])]),
      body,
    ]),
  ]);
}

/** The log as a feed: the page you open when something did not arrive. */
function deliveryFeed(entries, channels) {
  const rows = entries.slice(0, 25).map((e) => el('div', { class: 'v2-feed-row' }, [
    el('span', { class: 'v2-feed-when', text: fmtAgo(e.sent_at), title: fmtDateTime(e.sent_at) }),
    el('span', { class: 'v2-feed-channel', text: labelFor(e.channel_id, channels) }),
    el('span', { class: 'v2-feed-event', text: e.event_type }),
    el('span', { class: `v2-feed-status ${statusTone(e.status)}`, text: e.status }),
    el('span', { class: 'v2-feed-detail', text: e.detail || '' }),
  ]));

  return el('section', { class: 'v2-section' }, [
    el('div', { class: 'v2-shead' }, [
      el('h2', { text: 'Recent deliveries' }),
      el('div', { class: 'v2-aside' }, [
        el('span', { text: entries.length ? `${entries.length} recorded · the last 25 are kept here` : 'nothing yet' }),
      ]),
    ]),
    entries.length
      ? el('div', { class: 'v2-feed' }, rows)
      : el('div', { class: 'fleet-empty', text: 'Nothing has been delivered yet. A delivery appears here the first time an alert fires.' }),
  ]);
}

function statusTone(status) {
  if (status === 'sent') return 'good';
  if (status === 'squelched') return 'muted';
  return 'bad';
}

function windowList(ctx, windows, devices) {
  const now = Date.now();
  const sorted = [...windows].sort((a, b) => {
    if (isRecurring(a) !== isRecurring(b)) return isRecurring(a) ? 1 : -1;
    return isRecurring(a) ? a.start_minute - b.start_minute : a.starts_at - b.starts_at;
  });

  const rows = sorted.map((w) => {
    const state = windowState(w, now);
    return el('div', { class: 'v2-window' }, [
      el('div', {}, [
        el('div', { class: 'v2-window-label', text: w.label || 'Maintenance window' }),
        el('div', { class: 'v2-window-when', text: describeWindow(w) }),
        el('div', { class: 'v2-window-when', text: w.serials && w.serials.length
          ? `Drives: ${w.serials.map((s) => labelForSerial(devices, s)).join(', ')}`
          : 'All drives' }),
      ]),
      el('span', { class: `v2-state ${state === 'active' ? 'caution' : 'muted'}`, text: state }),
      el('button', {
        class: 'v2-btn',
        type: 'button',
        text: 'Delete',
        onclick: async () => {
          if (!(await confirmDialog('Delete window', `Delete "${w.label || 'this window'}"?`, 'Delete'))) return;
          await saveWindows(ctx, windows.filter((x) => x !== w));
        },
      }),
    ]);
  });

  return el('section', { class: 'v2-section' }, [
    el('div', { class: 'v2-shead' }, [
      el('h2', { text: 'Maintenance windows' }),
      el('div', { class: 'v2-aside' }, [
        el('span', { text: 'Alerts are silenced for the drives a window covers, and windows expire on their own.' }),
        el('button', { class: 'v2-btn', type: 'button', text: 'Add window', onclick: () => editWindow(ctx, windows, devices) }),
      ]),
    ]),
    sorted.length ? el('div', { class: 'v2-windows' }, rows)
      : el('div', { class: 'fleet-empty', text: 'No maintenance windows, every drive alerts all the time.' }),
  ]);
}

// --- bits -------------------------------------------------------------------

function parseList(raw) {
  try {
    const list = JSON.parse(raw);
    return Array.isArray(list) ? list : [];
  } catch {
    return [];
  }
}

function labelFor(id, channels) {
  const match = channels.find((c) => c.id === id);
  return match ? (match.label || match.type) : `channel ${id}`;
}
