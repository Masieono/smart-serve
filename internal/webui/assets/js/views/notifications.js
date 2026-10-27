// Notification channels (docs/UI.md §7.4, docs/NOTIFICATIONS.md).
import { el, mount, fmtAgo, fmtDateTime } from '../dom.js';
import { api } from '../api.js';
import { emptyState, errorBox, openDialog, confirmDialog, toast } from '../components.js';
import { icon } from '../icons.js';

// Fallback for the event catalog; the API is the source of truth so a new event
// type shows up in the subscription checkboxes without a UI change.
const EVENT_TYPES = ['health_change', 'temperature_high', 'device_disappeared', 'device_appeared', 'scan_failure', 'self_test_result', 'scan_success', 'group_heat'];

// Human labels for the subscription checkboxes; the event list itself comes from
// the API so a new event type appears without a UI change.
const EVENT_LABELS = {
  health_change: 'A drive’s verdict changes',
  temperature_high: 'A drive runs hotter than the threshold',
  device_disappeared: 'A drive disappears',
  device_appeared: 'A drive appears',
  scan_failure: 'A scheduled scan fails',
  self_test_result: 'A self-test finishes',
  scan_success: 'A scheduled scan completes successfully',
  group_heat: 'Several drives in one group heat up together',
};

export async function render(container, ctx) {
  mount(container, el('div', { class: 'skeleton', style: 'width:30%;height:24px' }));
  try {
    const [channels, log, settings, devices] = await Promise.all([
      api.channels(),
      api.notifications(25),
      api.settings(),
      // The maintenance-window editor picks drives by name, so it needs the list.
      api.devices(),
    ]);
    draw(container, ctx, channels, log, settings, devices);
  } catch (err) {
    mount(container, errorBox(`Could not load channels: ${err.message}`));
  }
}

function parseEvents(raw) {
  try {
    const list = JSON.parse(raw);
    return Array.isArray(list) ? list : [];
  } catch {
    return [];
  }
}

function draw(container, ctx, data, log, settings, deviceData) {
  const channels = data.channels || [];
  const eventTypes = (log && log.event_types) || EVENT_TYPES;
  // The server owns the list of channel types it can actually deliver to, so a
  // build without a provider never offers it (docs/NOTIFICATIONS.md).
  const typeList = (log && log.channel_types) || null;
  // Names only — the server never sends a resolved secret to the browser (ADR-022).
  const envVarList = (log && log.channel_env_vars) || null;
  const windows = parseWindows(settings && settings.settings && settings.settings.maintenance_windows);
  const devices = ((deviceData && deviceData.devices) || []).filter((d) => d.serial);
  const head = el('div', { class: 'page-head' }, [
    el('div', {}, [
      el('h1', { text: 'Notifications' }),
      el('div', { class: 'sub', text: 'Alerts fire on health transitions, never on every scan.' }),
    ]),
    el('span', { class: 'spacer' }),
    el('button', { class: 'btn btn-primary', type: 'button', onclick: () => editChannel(ctx, null, eventTypes, typeList, envVarList) },
      [icon('plus'), 'New channel']),
  ]);

  if (!channels.length) {
    mount(container, head, emptyState('No channels yet',
      'Add a channel (ntfy, Gotify, Telegram, Discord, email or a webhook) to be told when a drive starts failing.'),
      maintenanceCard(ctx, windows, devices));
    return;
  }

  mount(container, head, el('div', { class: 'card-grid' }, channels.map((c) => {
    const events = parseEvents(c.events);
    return el('article', { class: 'card stack' }, [
      el('div', { class: 'row' }, [
        el('div', { style: 'flex:1;min-width:0' }, [
          el('div', { class: 'device-card-title', text: c.label || c.type }),
          el('div', { class: 'device-card-model', text: c.type }),
        ]),
        el('span', { class: `badge badge-${c.enabled ? 'good' : 'unknown'}`,
          text: c.enabled ? 'Enabled' : 'Disabled' }),
      ]),
      // An empty subscription is not "nothing subscribed" — it is every event, which is what it has
      // always meant (`notify.subscribed`). The old text described the *setting* rather than its
      // effect, and read as a channel that would never speak: exactly the channel a digest gets set
      // up on, where the digest arrives *because* nothing has been narrowed away.
      el('div', { class: 'row wrap' }, events.length
        ? events.map((e) => el('span', { class: 'chip', text: e }))
        : [
            el('span', { class: 'chip', text: 'Everything' }),
            el('span', { class: 'muted',
              text: 'every alert and digest — the scheduled-scan heartbeat and group heat must be ticked' }),
          ]),
      el('div', { class: 'row' }, [
        el('span', { class: 'spacer' }),
        el('button', { class: 'btn btn-sm', type: 'button', title: 'Send a test event',
          onclick: async (event) => {
            const button = event.currentTarget;
            button.disabled = true;
            button.textContent = 'Sending…';
            try {
              await api.testChannel(c.id);
              toast('Test event sent', 'good');
            } catch (err) {
              // The API returns the destination's own error, which is the only
              // way to tell a bad token from a bad URL.
              toast(`Test failed: ${err.message}`, 'error');
            } finally {
              button.disabled = false;
              button.textContent = 'Test';
            }
          } }, ['Test']),
        el('button', { class: 'btn btn-sm btn-ghost', type: 'button', title: 'Edit',
          onclick: () => editChannel(ctx, c, eventTypes, typeList, envVarList) }, [icon('settings')]),
        el('button', { class: 'btn btn-sm btn-ghost', type: 'button', title: 'Delete',
          onclick: async () => {
            if (!(await confirmDialog('Delete channel', `Delete "${c.label || c.type}"?`, 'Delete'))) return;
            try {
              await api.deleteChannel(c.id);
              toast('Channel deleted', 'good');
              ctx.refresh();
            } catch (err) {
              toast(err.message, 'error');
            }
          } }, [icon('trash')]),
      ]),
    ]);
  })), deliveryLog(log, channels), maintenanceCard(ctx, windows, devices));
}

// --- maintenance windows ------------------------------------------------------

/**
 * Decodes the maintenance_windows setting. Anything malformed is treated as "no
 * windows", matching the server: a bad value must never look like an active silence.
 */
export function parseWindows(raw) {
  if (typeof raw !== 'string' || !raw.trim()) return [];
  try {
    const list = JSON.parse(raw);
    if (!Array.isArray(list)) return [];
    return list.filter((w) => w && Number.isFinite(w.starts_at) && Number.isFinite(w.ends_at)
      && w.ends_at > w.starts_at);
  } catch {
    return [];
  }
}

/** A `datetime-local` value: local time, no timezone suffix, minute precision. */
export function localInputValue(ms) {
  const d = new Date(ms);
  const pad = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T` +
    `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** Parses a `datetime-local` value as local time, or null when it is not a time. */
export function parseLocalInput(value) {
  if (!value) return null;
  const ms = new Date(value).getTime();
  return Number.isFinite(ms) ? ms : null;
}

/** Where a window sits relative to now. Recurring windows are labelled by their
 *  schedule instead: their times are read on the *server's* clock, so a browser in
 *  another timezone cannot say whether one is live right now. */
export function windowState(w, now) {
  if (isRecurring(w)) return 'recurring';
  if (now >= w.ends_at) return 'ended';
  if (now >= w.starts_at) return 'active';
  return 'upcoming';
}

const DAY_NAMES = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

/** Whether a window repeats rather than being a single interval. */
export function isRecurring(w) {
  return Boolean(w) && (w.recurrence === 'daily' || w.recurrence === 'weekly');
}

/** Minutes past midnight as "HH:MM". */
export function fmtClock(minutes) {
  const m = ((Number(minutes) || 0) % 1440 + 1440) % 1440;
  return `${String(Math.floor(m / 60)).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`;
}

/** Parses an "HH:MM" time input into minutes past midnight, or null. */
export function parseClock(value) {
  const match = /^(\d{1,2}):(\d{2})$/.exec(String(value ?? '').trim());
  if (!match) return null;
  const hours = Number(match[1]);
  const minutes = Number(match[2]);
  if (hours > 23 || minutes > 59) return null;
  return hours * 60 + minutes;
}

/** A window's schedule in words, for the list. */
export function describeWindow(w) {
  if (!isRecurring(w)) {
    return `${fmtDateTime(w.starts_at)} → ${fmtDateTime(w.ends_at)}`;
  }
  const hours = `${fmtClock(w.start_minute)}–${fmtClock(w.end_minute)}`;
  if (w.recurrence === 'daily') return `Every day, ${hours}`;
  const days = (w.days || [])
    .slice()
    .sort((a, b) => a - b)
    .map((d) => DAY_NAMES[d] || '?')
    .join(', ');
  return `${days} · ${hours}`;
}

/**
 * Builds a window from the form values, or returns an error message. Validating here
 * as well as on the server means the operator sees the problem in the field they
 * just filled in, rather than after a round trip.
 */
export function buildWindow(values) {
  const label = String(values.label || '').trim();
  const serials = toSerialList(values.serials);
  const recurrence = values.recurrence === 'weekly' ? 'weekly'
    : values.recurrence === 'daily' ? 'daily' : 'once';

  if (recurrence === 'once') {
    const start = parseLocalInput(values.start);
    const end = parseLocalInput(values.end);
    if (start === null || end === null) return { error: 'Pick a start and end time.' };
    if (end <= start) return { error: 'The end must be after the start.' };
    return { window: { label, serials, recurrence: 'once', starts_at: start, ends_at: end } };
  }

  const startMinute = parseClock(values.startTime);
  const endMinute = parseClock(values.endTime);
  if (startMinute === null || endMinute === null) return { error: 'Pick a start and end time.' };
  // Equal ends would be a zero-length window, which can never apply.
  if (startMinute === endMinute) return { error: 'The end must be different from the start.' };

  const days = recurrence === 'weekly'
    ? [...new Set((values.days || []).map(Number).filter((d) => d >= 0 && d <= 6))].sort()
    : [];
  if (recurrence === 'weekly' && !days.length) return { error: 'Pick at least one day.' };

  return {
    window: { label, serials, recurrence, start_minute: startMinute, end_minute: endMinute, days },
  };
}

/** Serial numbers from a list of picked drives. */
function toSerialList(raw) {
  if (Array.isArray(raw)) return raw.map((s) => String(s).trim()).filter(Boolean);
  return String(raw || '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);
}

async function saveWindows(ctx, windows) {
  try {
    await api.putSettings({ maintenance_windows: JSON.stringify(windows) });
    toast('Maintenance windows saved', 'good');
    ctx.refresh();
  } catch (err) {
    toast(err.message, 'error');
  }
}

function maintenanceCard(ctx, windows, devices) {
  const now = Date.now();
  // Recurring windows sort by their time of day; one-off ones by when they start.
  const sorted = [...windows].sort((a, b) => {
    if (isRecurring(a) !== isRecurring(b)) return isRecurring(a) ? 1 : -1;
    return isRecurring(a) ? a.start_minute - b.start_minute : a.starts_at - b.starts_at;
  });

  return el('div', { class: 'card stack' }, [
    el('div', { class: 'row' }, [
      el('h2', { text: 'Maintenance windows' }),
      el('span', { class: 'spacer' }),
      el('button', { class: 'btn btn-sm', type: 'button',
        onclick: () => editWindow(ctx, windows, devices) },
        [icon('plus'), 'Add window']),
    ]),
    el('p', { class: 'muted', text: 'Alerts are silenced for the drives a window covers. An array ' +
      'rebuild, a migration, a chassis being moved. Windows expire on their own, so there is nothing ' +
      'to remember to switch back on.' }),
    sorted.length
      ? el('div', { class: 'stack', style: 'gap:var(--space-2)' },
          sorted.map((w) => windowRow(ctx, windows, devices, w, now)))
      : el('p', { class: 'muted', text: 'No maintenance windows configured.' }),
  ]);
}

function windowRow(ctx, windows, devices, w, now) {
  const state = windowState(w, now);
  const tone = { active: 'caution', upcoming: 'unknown', ended: 'good', recurring: 'unknown' }[state];

  return el('div', { class: 'row window-row' }, [
    el('div', { style: 'flex:1;min-width:0' }, [
      el('div', { class: 'setting-label', text: w.label || 'Maintenance window' }),
      el('div', { class: 'setting-help', text: describeWindow(w) }),
      el('div', { class: 'setting-help', text: w.serials && w.serials.length
        ? `Drives: ${w.serials.map((s) => labelForSerial(devices, s)).join(', ')}`
        : 'All drives' }),
    ]),
    el('span', { class: `badge badge-${tone}`, text: state }),
    el('button', { class: 'btn btn-sm btn-ghost', type: 'button', title: 'Delete',
      onclick: async () => {
        if (!(await confirmDialog('Delete window', `Delete "${w.label || 'this window'}"?`, 'Delete'))) return;
        await saveWindows(ctx, windows.filter((x) => x !== w));
      } }, [icon('trash')]),
  ]);
}

/** A drive's friendly name where we have one, so the list is readable. */
function labelForSerial(devices, serial) {
  const match = devices.find((d) => d.serial === serial);
  if (!match) return serial;
  return match.name || match.model || serial;
}

function editWindow(ctx, windows, devices) {
  openDialog('Add maintenance window', (body, close, foot) => {
    const now = Date.now();
    const label = el('input', { type: 'text', placeholder: 'e.g. array rebuild' });
    const recurrence = el('select', {}, [
      el('option', { value: 'once', text: 'Once, with a start and end date' }),
      el('option', { value: 'daily', text: 'Every day' }),
      el('option', { value: 'weekly', text: 'Certain days of the week' }),
    ]);

    const start = el('input', { type: 'datetime-local', value: localInputValue(now) });
    const end = el('input', { type: 'datetime-local', value: localInputValue(now + 2 * 3600000) });
    const onceGroup = el('div', { class: 'stack', style: 'gap:var(--space-4)' }, [
      el('div', { class: 'field' }, [el('label', { text: 'Starts' }), start]),
      el('div', { class: 'field' }, [el('label', { text: 'Ends' }), end,
        hint('Your browser’s local timezone. The window ends at this time.')]),
    ]);

    const startTime = el('input', { type: 'time', value: '02:00' });
    const endTime = el('input', { type: 'time', value: '04:00' });
    const dayRows = DAY_NAMES.map((name, index) => {
      const box = el('input', { type: 'checkbox', checked: index === 0 || index === 6 });
      box.dataset.day = String(index);
      return el('label', { class: 'field-row' }, [box, el('span', { text: name })]);
    });
    const daysField = el('div', { class: 'field' }, [
      el('label', { text: 'Days' }),
      el('div', { class: 'row wrap', style: 'gap:var(--space-3)' }, dayRows),
    ]);
    const recurringGroup = el('div', { class: 'stack', style: 'gap:var(--space-4)' }, [
      el('div', { class: 'row', style: 'gap:var(--space-4)' }, [
        el('div', { class: 'field' }, [el('label', { text: 'From' }), startTime]),
        el('div', { class: 'field' }, [el('label', { text: 'To' }), endTime]),
      ]),
      daysField,
      hint('Recurring times are read on the server’s clock, the same one scheduled scans use.'),
    ]);

    const syncRecurrence = () => {
      const recurring = recurrence.value !== 'once';
      onceGroup.hidden = recurring;
      recurringGroup.hidden = !recurring;
      daysField.hidden = recurrence.value !== 'weekly';
    };
    recurrence.addEventListener('change', syncRecurrence);
    syncRecurrence();

    const drives = drivePicker(devices);

    body.append(
      el('div', { class: 'field' }, [el('label', { text: 'Label' }), label,
        hint('Shown in the delivery log, so you can tell which window silenced an alert.')]),
      el('div', { class: 'field' }, [el('label', { text: 'Repeats' }), recurrence]),
      onceGroup,
      recurringGroup,
      el('div', { class: 'field' }, [el('label', { text: 'Drives' }), drives.element,
        hint(devices.length
          ? 'Nothing selected means every drive, including ones not connected yet.'
          : 'No drives discovered yet, so the window will cover every drive.')]),
    );

    foot.append(
      el('button', { class: 'btn', type: 'button', text: 'Cancel', onclick: close }),
      el('button', { class: 'btn btn-primary', type: 'button', text: 'Add', onclick: async (event) => {
        // Captured before the first `await`: `event.currentTarget` is null once dispatch ends.
        const button = event.currentTarget;
        const built = buildWindow({
          label: label.value,
          recurrence: recurrence.value,
          start: start.value,
          end: end.value,
          startTime: startTime.value,
          endTime: endTime.value,
          days: dayRows
            .filter((row) => row.querySelector('input').checked)
            .map((row) => Number(row.querySelector('input').dataset.day)),
          serials: drives.selected(),
        });
        if (built.error) {
          toast(built.error, 'error');
          return;
        }
        button.disabled = true;
        await saveWindows(ctx, [...windows, built.window]);
        close();
      } }),
    );
    // A form: a stray click beside it must not throw away a half-built window.
  }, { dismissOnBackdrop: false });
}

/**
 * A checklist of the drives we know about, so a window can be scoped by picking names
 * rather than pasting serial numbers. Nothing ticked means every drive, which also
 * covers drives that are not connected right now.
 */
function drivePicker(devices) {
  const boxes = devices.map((d) => {
    const box = el('input', { type: 'checkbox' });
    box.dataset.serial = d.serial;
    return el('label', { class: 'drive-pick' }, [
      box,
      el('span', { class: 'drive-pick-name', text: d.name || d.model || d.serial }),
      el('span', { class: 'drive-pick-meta', text: d.serial }),
    ]);
  });

  return {
    element: boxes.length
      ? el('div', { class: 'drive-pick-list' }, boxes)
      : el('p', { class: 'muted', text: 'No drives discovered yet.' }),
    selected: () => boxes
      .filter((row) => row.querySelector('input').checked)
      .map((row) => row.querySelector('input').dataset.serial),
  };
}

// The delivery log is the only record of an alert that fired while a channel was
// down, so it is not optional furniture (ADR-008).
function deliveryLog(log, channels) {
  const entries = (log && log.notifications) || [];
  if (!entries.length) return null;

  const labelFor = (id) => {
    const match = channels.find((c) => c.id === id);
    return match ? (match.label || match.type) : `channel ${id}`;
  };

  // No inline margin: the page's own gap spaces this card from its neighbours, and a
  // margin on top of that gap is what made the cards look unevenly spaced.
  return el('div', { class: 'card stack' }, [
    el('h2', { text: 'Recent deliveries' }),
    el('div', { class: 'table-wrap' }, [
      el('table', { class: 'data' }, [
        el('thead', {}, [el('tr', {}, [
          el('th', { text: 'When' }),
          el('th', { text: 'Channel' }),
          el('th', { text: 'Event' }),
          el('th', { text: 'Status' }),
          el('th', { text: 'Detail' }),
        ])]),
        el('tbody', {}, entries.map((e) => el('tr', {}, [
          el('td', { text: fmtAgo(e.sent_at), title: fmtDateTime(e.sent_at) }),
          el('td', { text: labelFor(e.channel_id) }),
          el('td', { text: e.event_type }),
          el('td', {}, [el('span', {
            class: `badge badge-${e.status === 'sent' ? 'good' : e.status === 'squelched' ? 'unknown' : 'bad'}`,
            text: e.status,
          })]),
          el('td', { class: 'muted', text: e.detail || '—' }),
        ]))),
      ]),
    ]),
  ]);
}

// One entry per channel type. Each field declares how to render itself and how to
// read itself back, so adding a provider is a data change rather than another branch
// in the editor (docs/NOTIFICATIONS.md).
const CHANNEL_TYPES = [
  {
    type: 'ntfy',
    label: 'ntfy (push notifications)',
    fields: [
      { key: 'server', label: 'Server', default: 'https://ntfy.sh',
        help: 'ntfy.sh, or your own self-hosted server.' },
      { key: 'topic', label: 'Topic', required: true, placeholder: 'my-smart-alerts',
        help: 'Subscribe to this topic in the ntfy app to receive the alerts.' },
      { key: 'token', label: 'Access token', secret: true,
        help: 'Can also be supplied by the environment. The variable name is shown below the label.' },
    ],
  },
  {
    type: 'gotify',
    label: 'Gotify (self-hosted push)',
    fields: [
      { key: 'server', label: 'Server', required: true, placeholder: 'https://gotify.example.com' },
      { key: 'token', label: 'App token', secret: true, required: true,
        help: 'Create an application in Gotify and copy its token.' },
    ],
  },
  {
    type: 'telegram',
    label: 'Telegram',
    fields: [
      { key: 'token', label: 'Bot token', secret: true, required: true,
        help: 'From @BotFather. The bot must be allowed to post in the chat.' },
      { key: 'chat_id', label: 'Chat ID', required: true, placeholder: '-1001234567890',
        help: 'Send /start to the bot, then ask @userinfobot for the id.' },
      { key: 'thread_id', label: 'Topic ID', placeholder: 'optional',
        help: 'Only needed for a forum topic.' },
    ],
  },
  {
    type: 'discord',
    label: 'Discord (webhook)',
    fields: [
      { key: 'webhook_url', label: 'Webhook URL', secret: true, required: true,
        placeholder: 'https://discord.com/api/webhooks/…',
        help: 'Channel settings → Integrations → Webhooks.' },
      { key: 'username', label: 'Display name', placeholder: 'Smart-Serve' },
    ],
  },
  {
    type: 'smtp',
    label: 'Email (SMTP)',
    fields: [
      { key: 'host', label: 'SMTP host', required: true, placeholder: 'smtp.example.com' },
      { key: 'port', label: 'Port', default: '587', placeholder: '587',
        help: '587 upgrades with STARTTLS, 465 uses implicit TLS.' },
      { key: 'username', label: 'Username' },
      { key: 'password', label: 'Password', secret: true },
      { key: 'from', label: 'From address', required: true, placeholder: 'smart-serve@example.com' },
      { key: 'to', label: 'Recipients', required: true, placeholder: 'ops@example.com, me@example.com',
        help: 'Separate several addresses with commas.' },
    ],
  },
  {
    type: 'webhook',
    label: 'Webhook (generic HTTP POST)',
    fields: [
      { key: 'url', label: 'URL', required: true, placeholder: 'https://example.com/hook',
        help: 'Smart-Serve sends a JSON body describing the event.' },
      { key: 'method', label: 'Method', options: ['POST', 'PUT'], default: 'POST' },
      { key: 'headers', label: 'Headers', multiline: true,
        placeholder: 'Authorization: Bearer abc123',
        parse: textToHeaders, format: headersToText,
        help: 'One "Name: value" per line, for example an Authorization header.' },
    ],
  },
];

/** The specs the editor should offer, limited to what the server can deliver to. */
export function availableTypes(serverTypes) {
  if (!Array.isArray(serverTypes) || !serverTypes.length) return CHANNEL_TYPES;
  const offered = CHANNEL_TYPES.filter((t) => serverTypes.includes(t.type));
  return offered.length ? offered : CHANNEL_TYPES;
}

function specFor(type) {
  return CHANNEL_TYPES.find((t) => t.type === type) || CHANNEL_TYPES[0];
}

/**
 * Builds a channel config from a type spec and its raw field values. Pure, so the
 * mapping — trimming, dropping blanks, parsing header lines — can be tested without
 * a DOM.
 *
 * Blank optional fields are omitted rather than sent as empty strings, so the
 * server's own defaults apply.
 */
export function configFromValues(type, values) {
  const config = {};
  for (const field of specFor(type).fields) {
    const raw = String(values[field.key] ?? '').trim();
    const value = field.parse ? field.parse(raw) : raw;
    if (value === '' || value === null || value === undefined) continue;
    if (typeof value === 'object' && !Object.keys(value).length) continue;
    config[field.key] = value;
  }
  return config;
}

/**
 * The first missing required field, as a message, or null. Derived from the same
 * spec that builds the form, so a new required field cannot be added without also
 * being enforced.
 */
export function missingRequired(type, values, supplied = {}) {
  for (const field of specFor(type).fields) {
    // A field the deployment supplies is never typed into the form, so demanding it here
    // would make the override impossible to use — the editor would refuse to save the very
    // configuration that makes it work.
    if (supplied[field.key]) continue;
    if (field.required && !String(values[field.key] ?? '').trim()) {
      return `${field.label} is required.`;
    }
  }
  return null;
}

// --- per-channel environment overrides (ADR-022) -------------------------------------
//
// The server resolves `SMARTSERVE_CHANNEL_<LABEL>_<FIELD>` — and its `_FILE` form — over the
// value stored in the database, so a token can live in a Docker secret rather than in the
// `/data` volume. The editor has to know the same naming rule: it shows the operator which
// variable to set, and it must stop demanding a value the deployment already provides.
//
// This mirrors `slug()` in `internal/notify/secrets.go`. The rule is duplicated because the
// name has to appear while the label is being typed, before anything has been saved; both
// sides are pinned by a test using the same cases, so they cannot drift apart unnoticed.

/** Uppercase, with any run of non-alphanumerics reduced to a single underscore. */
export function channelSlug(label) {
  return String(label || '')
    .toUpperCase()
    .replace(/[^A-Z0-9]+/g, '_')
    .replace(/^_+|_+$/g, '');
}

/** The environment prefix for one channel, or '' when the label has no usable characters. */
export function channelEnvPrefix(label) {
  const slug = channelSlug(label);
  return slug ? `SMARTSERVE_CHANNEL_${slug}` : '';
}

/** Both variables that can override one field: the plain form and the `_FILE` form. */
export function channelEnvVars(label, key) {
  const prefix = channelEnvPrefix(label);
  if (!prefix) return [];
  const name = `${prefix}_${channelSlug(key)}`;
  return [name, `${name}_FILE`];
}

/**
 * The fields the deployment supplies, as a `key → "environment" | "file"` map.
 *
 * `set` is the list of variable names the server reports as present. A field counts as
 * supplied only when the exact variable is set, not when it merely *could* be — so a typo in
 * the label shows up as a field that still asks for a value, rather than as a save that
 * quietly does nothing.
 */
export function suppliedFields(label, spec, set) {
  const names = new Set(set || []);
  const out = {};
  for (const field of (spec && spec.fields) || []) {
    const [plain, file] = channelEnvVars(label, field.key);
    if (!plain) continue;
    if (names.has(plain)) out[field.key] = 'environment';
    else if (names.has(file)) out[field.key] = 'file';
  }
  return out;
}

/** The channel types the editor offers, in order. */
export function channelTypes() {
  return CHANNEL_TYPES.map((t) => t.type);
}


function editChannel(ctx, existing, eventTypes = EVENT_TYPES, serverTypes = null, envVarList = null) {
  const saved = parseConfig(existing && existing.config);
  const offered = availableTypes(serverTypes);

  openDialog(existing ? 'Edit channel' : 'New channel', (body, close, foot) => {
    const type = el('select', {}, offered.map((t) => el('option', { value: t.type, text: t.label })));
    // A channel saved with a type this build no longer ships falls back to the first
    // offered type rather than leaving the select blank.
    type.value = (existing && existing.type) || offered[0].type;
    if (!type.value) type.value = offered[0].type;

    const label = el('input', {
      type: 'text',
      value: (existing && existing.label) || '',
      placeholder: 'e.g. server alerts',
    });
    const envHint = hint('');

    // Controls are built once per type and kept, so flicking through the type list
    // does not discard what has already been typed.
    const groups = new Map();
    const controls = new Map();
    // Per-field badges saying the deployment supplies this value (`ADR-022`). Collected in
    // the same shape as `controls` so they can be refreshed whenever the label changes.
    const badges = new Map();

    for (const spec of offered) {
      const map = {};
      const badgeMap = {};
      const fields = spec.fields.map((field) => {
        let control;
        if (field.options) {
          control = el('select', {}, field.options.map((o) => el('option', { value: o, text: o })));
        } else if (field.multiline) {
          control = el('textarea', {});
        } else {
          control = el('input', { type: field.secret ? 'password' : 'text' });
        }

        // Only the type being edited has saved values; the others start blank.
        const raw = existing && existing.type === spec.type ? saved[field.key] : undefined;
        const text = field.format ? field.format(raw) : raw;
        control.value = text === undefined || text === null ? '' : String(text);
        if (!control.value && field.default) control.value = field.default;
        if (field.placeholder) control.placeholder = field.placeholder;

        map[field.key] = control;
        const badge = el('span', { class: 'badge badge-good env-supplied' });
        badge.hidden = true;
        badgeMap[field.key] = badge;
        return el('div', { class: 'field' }, [
          el('label', { text: field.label }),
          control,
          badge,
          field.help ? hint(field.help) : null,
        ]);
      });
      controls.set(spec.type, map);
      badges.set(spec.type, badgeMap);
      groups.set(spec.type, el('div', { class: 'stack', style: 'gap:var(--space-3)' }, fields));
    }

    const syncType = () => {
      for (const [name, group] of groups) group.hidden = name !== type.value;
    };
    type.addEventListener('change', syncType);
    syncType();

    /**
     * Refresh the environment-override badges and the variable-name hint.
     *
     * Recomputed from the label as it is typed, because the variable name is derived from the
     * label: renaming a channel changes which variables it reads, and seeing that immediately
     * is far better than discovering it because alerts quietly stopped arriving.
     */
    const syncSupplied = () => {
      const spec = specFor(type.value);
      const supplied = suppliedFields(label.value, spec, envVarList);
      const badgeMap = badges.get(spec.type) || {};
      for (const field of spec.fields) {
        const badge = badgeMap[field.key];
        if (!badge) continue;
        const source = supplied[field.key];
        badge.hidden = !source;
        badge.textContent = source === 'file' ? 'from file' : 'from environment';
      }
      const prefix = channelEnvPrefix(label.value);
      envHint.hidden = !prefix;
      envHint.textContent = prefix
        ? `Environment overrides: ${prefix}_<FIELD>, or …_<FIELD>_FILE for a mounted secret file.`
        : '';
    };
    label.addEventListener('input', syncSupplied);
    type.addEventListener('change', syncSupplied);
    syncSupplied();

    const enabled = el('input', { type: 'checkbox', checked: existing ? existing.enabled : true });

    const subscribed = new Set(parseEvents(existing && existing.events));
    const eventRows = eventTypes.map((name) => {
      const box = el('input', { type: 'checkbox', checked: subscribed.has(name) });
      box.dataset.event = name;
      return el('label', { class: 'field-row' }, [box, el('span', { text: EVENT_LABELS[name] || name })]);
    });

    function buildConfig() {
      const spec = specFor(type.value);
      const map = controls.get(spec.type);
      const values = {};
      for (const field of spec.fields) values[field.key] = map[field.key].value;
      return configFromValues(type.value, values);
    }

    const validate = () => {
      const spec = specFor(type.value);
      const map = controls.get(spec.type);
      const values = {};
      for (const field of spec.fields) values[field.key] = map[field.key].value;
      // A field the deployment supplies is never typed here, so it must not be demanded.
      return missingRequired(type.value, values, suppliedFields(label.value, spec, envVarList));
    };

    body.append(
      el('div', { class: 'field' }, [el('label', { text: 'Type' }), type]),
      el('div', { class: 'field' }, [el('label', { text: 'Label' }), label, envHint]),
      ...groups.values(),
      el('div', { class: 'field' }, [
        el('label', { text: 'Send me an alert when…' }),
        el('div', { class: 'stack', style: 'gap:var(--space-1)' }, eventRows),
        // "Everything" is the default subscription, and it is about exceptions. The heartbeat is
        // not one — it fires on every scheduled scan — so it has to be asked for, and the user needs
        // to know that rather than wondering why it never arrives. `group_heat` is opt-in for the
        // same reason (`NOTIFICATIONS.md`), and the hint named only the heartbeat, so a channel with
        // nothing ticked looked like it would receive a finding it would not. The digest is named
        // too, because an empty subscription *does* receive it and the schedule that sends it gives
        // no sign of which channels it reaches.
        hint('Leaving every box unchecked means every alert above, plus every digest — except the ' +
          'two that have to be ticked: the scheduled-scan heartbeat and the group-heat finding.'),
      ]),
      el('label', { class: 'field-row' }, [enabled, el('span', { text: 'Enabled' })]),
    );

    function payloadFromForm() {
      return {
        type: type.value,
        label: label.value.trim(),
        config: buildConfig(),
        events: eventRows
          .filter((row) => row.querySelector('input').checked)
          .map((row) => row.querySelector('input').dataset.event),
        enabled: enabled.checked,
      };
    }

    async function store() {
      const payload = payloadFromForm();
      if (existing) payload.id = existing.id;
      return existing ? api.updateChannel(payload) : api.createChannel(payload);
    }

    foot.append(
      // Tests the form as it stands, saved or not, and never closes the dialog: the
      // whole point is to fix a misconfiguration and try again in place.
      el('button', { class: 'btn', type: 'button', text: 'Test',
        title: 'Send one test event through these settings, without saving them',
        onclick: async (event) => {
          // Captured before the first `await`: `event.currentTarget` is null once dispatch ends,
          // so a `finally` that reads it throws and leaves the button greyed out for good.
          const button = event.currentTarget;
          const problem = validate();
          if (problem) {
            toast(problem, 'error');
            return;
          }
          button.disabled = true;
          try {
            await api.testChannelConfig(payloadFromForm());
            toast('Test event sent', 'good');
          } catch (err) {
            // The API returns the destination's own error, which is the only way to
            // tell a bad token from a bad URL.
            toast(`Test failed: ${err.message}`, 'error');
          } finally {
            button.disabled = false;
          }
        } }),
      el('span', { class: 'spacer' }),
      el('button', { class: 'btn', type: 'button', text: 'Cancel', onclick: close }),
      el('button', { class: 'btn btn-primary', type: 'button', text: 'Save', onclick: async (event) => {
        // Captured before the first `await`: `event.currentTarget` is null once dispatch ends.
        const button = event.currentTarget;
        const problem = validate();
        if (problem) {
          toast(problem, 'error');
          return;
        }
        button.disabled = true;
        try {
          await store();
          toast('Channel saved', 'good');
          close();
          ctx.refresh();
        } catch (err) {
          toast(err.message, 'error');
          button.disabled = false;
        }
      } }),
    );
    // A form, and a long one: a mistyped URL or a pasted token is not worth losing to a stray click.
  }, { dismissOnBackdrop: false });
}

function hint(text) {
  return el('span', { class: 'muted', style: 'font-size:var(--text-xs)', text });
}

function parseConfig(raw) {
  try {
    const parsed = JSON.parse(raw || '{}');
    return parsed && typeof parsed === 'object' ? parsed : {};
  } catch {
    return {};
  }
}

function headersToText(headers) {
  if (!headers || typeof headers !== 'object') return '';
  return Object.entries(headers).map(([name, value]) => `${name}: ${value}`).join('\n');
}

function textToHeaders(text) {
  const out = {};
  for (const line of String(text || '').split('\n')) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const index = trimmed.indexOf(':');
    if (index <= 0) continue;
    out[trimmed.slice(0, index).trim()] = trimmed.slice(index + 1).trim();
  }
  return out;
}

