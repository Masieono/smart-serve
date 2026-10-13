// Notification channels (docs/UI.md §7.4, docs/NOTIFICATIONS.md).
import { el, mount, fmtAgo, fmtDateTime } from '../dom.js';
import { api } from '../api.js';
import { emptyState, errorBox, openDialog, confirmDialog, toast } from '../components.js';
import { icon } from '../icons.js';

// Fallback for the event catalog; the API is the source of truth so a new event
// type shows up in the subscription checkboxes without a UI change.
const EVENT_TYPES = ['health_change', 'temperature_high', 'device_disappeared', 'device_appeared', 'scan_failure'];

// Human labels for the subscription checkboxes; the event list itself comes from
// the API so a new event type appears without a UI change.
const EVENT_LABELS = {
  health_change: 'A drive’s verdict changes',
  temperature_high: 'A drive runs hotter than the threshold',
  device_disappeared: 'A drive disappears',
  device_appeared: 'A drive appears',
  scan_failure: 'A scheduled scan fails',
};

export async function render(container, ctx) {
  mount(container, el('div', { class: 'skeleton', style: 'width:30%;height:24px' }));
  try {
    const [channels, log, settings] = await Promise.all([
      api.channels(),
      api.notifications(25),
      api.settings(),
    ]);
    draw(container, ctx, channels, log, settings);
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

function draw(container, ctx, data, log, settings) {
  const channels = data.channels || [];
  const eventTypes = (log && log.event_types) || EVENT_TYPES;
  // The server owns the list of channel types it can actually deliver to, so a
  // build without a provider never offers it (docs/NOTIFICATIONS.md).
  const typeList = (log && log.channel_types) || null;
  const windows = parseWindows(settings && settings.settings && settings.settings.maintenance_windows);
  const head = el('div', { class: 'page-head' }, [
    el('div', {}, [
      el('h1', { text: 'Notifications' }),
      el('div', { class: 'sub', text: 'Alerts fire on health transitions — never on every scan.' }),
    ]),
    el('span', { class: 'spacer' }),
    el('button', { class: 'btn btn-primary', type: 'button', onclick: () => editChannel(ctx, null, eventTypes, typeList) },
      [icon('plus'), 'New channel']),
  ]);

  if (!channels.length) {
    mount(container, head, emptyState('No channels yet',
      'Add a channel — ntfy, Gotify, Telegram, Discord, email or a webhook — to be told when a drive starts failing.'),
      maintenanceCard(ctx, windows));
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
      el('div', { class: 'row wrap' }, events.length
        ? events.map((e) => el('span', { class: 'chip', text: e }))
        : [el('span', { class: 'muted', text: 'No events subscribed' })]),
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
          onclick: () => editChannel(ctx, c, eventTypes, typeList) }, [icon('settings')]),
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
  })), deliveryLog(log, channels), maintenanceCard(ctx, windows));
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

/** Where a window sits relative to now. */
export function windowState(w, now) {
  if (now >= w.ends_at) return 'ended';
  if (now >= w.starts_at) return 'active';
  return 'upcoming';
}

/**
 * Builds a window from the form values, or returns an error message. Validating here
 * as well as on the server means the operator sees the problem in the field they
 * just filled in, rather than after a round trip.
 */
export function buildWindow(values) {
  const start = parseLocalInput(values.start);
  const end = parseLocalInput(values.end);
  if (start === null || end === null) return { error: 'Pick a start and end time.' };
  if (end <= start) return { error: 'The end must be after the start.' };
  const serials = String(values.serials || '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);
  return {
    window: { label: String(values.label || '').trim(), starts_at: start, ends_at: end, serials },
  };
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

function maintenanceCard(ctx, windows) {
  const now = Date.now();
  const sorted = [...windows].sort((a, b) => a.starts_at - b.starts_at);

  return el('div', { class: 'card stack' }, [
    el('div', { class: 'row' }, [
      el('h2', { text: 'Maintenance windows' }),
      el('span', { class: 'spacer' }),
      el('button', { class: 'btn btn-sm', type: 'button', onclick: () => editWindow(ctx, windows) },
        [icon('plus'), 'Add window']),
    ]),
    el('p', { class: 'muted', text: 'Alerts are silenced for the drives a window covers — an array ' +
      'rebuild, a migration, a chassis being moved. Windows expire on their own, so there is nothing ' +
      'to remember to switch back on.' }),
    sorted.length
      ? el('div', { class: 'stack', style: 'gap:var(--space-2)' },
          sorted.map((w) => windowRow(ctx, windows, w, now)))
      : el('p', { class: 'muted', text: 'No maintenance windows configured.' }),
  ]);
}

function windowRow(ctx, windows, w, now) {
  const state = windowState(w, now);
  const tone = { active: 'caution', upcoming: 'unknown', ended: 'good' }[state];

  return el('div', { class: 'row window-row' }, [
    el('div', { style: 'flex:1;min-width:0' }, [
      el('div', { class: 'setting-label', text: w.label || 'Maintenance window' }),
      el('div', { class: 'setting-help',
        text: `${fmtDateTime(w.starts_at)} → ${fmtDateTime(w.ends_at)}` }),
      el('div', { class: 'setting-help',
        text: w.serials && w.serials.length ? `Drives: ${w.serials.join(', ')}` : 'All drives' }),
    ]),
    el('span', { class: `badge badge-${tone}`, text: state }),
    el('button', { class: 'btn btn-sm btn-ghost', type: 'button', title: 'Delete',
      onclick: async () => {
        if (!(await confirmDialog('Delete window', `Delete "${w.label || 'this window'}"?`, 'Delete'))) return;
        await saveWindows(ctx, windows.filter((x) => x !== w));
      } }, [icon('trash')]),
  ]);
}

function editWindow(ctx, windows) {
  openDialog('Add maintenance window', (body, close) => {
    const now = Date.now();
    const label = el('input', { type: 'text', placeholder: 'e.g. array rebuild' });
    const start = el('input', { type: 'datetime-local', value: localInputValue(now) });
    const end = el('input', { type: 'datetime-local', value: localInputValue(now + 2 * 3600000) });
    const serials = el('input', { type: 'text', placeholder: 'leave blank for every drive' });

    body.append(
      el('div', { class: 'field' }, [el('label', { text: 'Label' }), label,
        hint('Shown in the delivery log, so you can tell which window silenced an alert.')]),
      el('div', { class: 'field' }, [el('label', { text: 'Starts' }), start]),
      el('div', { class: 'field' }, [el('label', { text: 'Ends' }), end,
        hint('Times are in your browser’s local timezone. The window ends at this time.')]),
      el('div', { class: 'field' }, [el('label', { text: 'Drives' }), serials,
        hint('Comma-separated serial numbers, or blank to silence every drive.')]),
      el('div', { class: 'dialog-foot', style: 'border:0;padding:0' }, [
        el('button', { class: 'btn', type: 'button', text: 'Cancel', onclick: close }),
        el('button', { class: 'btn btn-primary', type: 'button', text: 'Add', onclick: async (event) => {
          const built = buildWindow({
            label: label.value, start: start.value, end: end.value, serials: serials.value,
          });
          if (built.error) {
            toast(built.error, 'error');
            return;
          }
          event.currentTarget.disabled = true;
          await saveWindows(ctx, [...windows, built.window]);
          close();
        } }),
      ]),
    );
  });
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

  return el('div', { class: 'card', style: 'margin-top:var(--space-6)' }, [
    el('h2', { style: 'margin-bottom:var(--space-3)', text: 'Recent deliveries' }),
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
        help: 'Can also come from the SMARTSERVE_NTFY_TOKEN environment variable (ADR-013).' },
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
        help: 'One "Name: value" per line — for example an Authorization header.' },
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
export function missingRequired(type, values) {
  for (const field of specFor(type).fields) {
    if (field.required && !String(values[field.key] ?? '').trim()) {
      return `${field.label} is required.`;
    }
  }
  return null;
}

/** The channel types the editor offers, in order. */
export function channelTypes() {
  return CHANNEL_TYPES.map((t) => t.type);
}


function editChannel(ctx, existing, eventTypes = EVENT_TYPES, serverTypes = null) {
  const saved = parseConfig(existing && existing.config);
  const offered = availableTypes(serverTypes);

  openDialog(existing ? 'Edit channel' : 'New channel', (body, close) => {
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

    // Controls are built once per type and kept, so flicking through the type list
    // does not discard what has already been typed.
    const groups = new Map();
    const controls = new Map();

    for (const spec of offered) {
      const map = {};
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
        return el('div', { class: 'field' }, [
          el('label', { text: field.label }),
          control,
          field.help ? hint(field.help) : null,
        ]);
      });
      controls.set(spec.type, map);
      groups.set(spec.type, el('div', { class: 'stack', style: 'gap:var(--space-3)' }, fields));
    }

    const syncType = () => {
      for (const [name, group] of groups) group.hidden = name !== type.value;
    };
    type.addEventListener('change', syncType);
    syncType();

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
      return missingRequired(type.value, values);
    };

    body.append(
      el('div', { class: 'field' }, [el('label', { text: 'Type' }), type]),
      el('div', { class: 'field' }, [el('label', { text: 'Label' }), label]),
      ...groups.values(),
      el('div', { class: 'field' }, [
        el('label', { text: 'Send me an alert when…' }),
        el('div', { class: 'stack', style: 'gap:var(--space-1)' }, eventRows),
      ]),
      el('label', { class: 'field-row' }, [enabled, el('span', { text: 'Enabled' })]),
      hint('Save, then use Test on the channel card to verify delivery.'),
      el('div', { class: 'dialog-foot', style: 'border:0;padding:0' }, [
        el('button', { class: 'btn', type: 'button', text: 'Cancel', onclick: close }),
        el('button', { class: 'btn btn-primary', type: 'button', text: 'Save', onclick: async (event) => {
          const problem = validate();
          if (problem) {
            toast(problem, 'error');
            return;
          }
          const payload = {
            type: type.value,
            label: label.value.trim(),
            config: buildConfig(),
            events: eventRows
              .filter((row) => row.querySelector('input').checked)
              .map((row) => row.querySelector('input').dataset.event),
            enabled: enabled.checked,
          };
          if (existing) payload.id = existing.id;

          event.currentTarget.disabled = true;
          try {
            if (existing) await api.updateChannel(payload);
            else await api.createChannel(payload);
            toast('Channel saved', 'good');
            close();
            ctx.refresh();
          } catch (err) {
            toast(err.message, 'error');
            event.currentTarget.disabled = false;
          }
        } }),
      ]),
    );
  });
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

