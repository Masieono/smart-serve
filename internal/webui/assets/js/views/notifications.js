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
    const [channels, log] = await Promise.all([api.channels(), api.notifications(25)]);
    draw(container, ctx, channels, log);
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

function draw(container, ctx, data, log) {
  const channels = data.channels || [];
  const eventTypes = (log && log.event_types) || EVENT_TYPES;
  const head = el('div', { class: 'page-head' }, [
    el('div', {}, [
      el('h1', { text: 'Notifications' }),
      el('div', { class: 'sub', text: 'Alerts fire on health transitions — never on every scan.' }),
    ]),
    el('span', { class: 'spacer' }),
    el('button', { class: 'btn btn-primary', type: 'button', onclick: () => editChannel(ctx, null, eventTypes) },
      [icon('plus'), 'New channel']),
  ]);

  if (!channels.length) {
    mount(container, head, emptyState('No channels yet',
      'Add an ntfy topic or a webhook to be told when a drive starts failing.'));
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
          onclick: () => editChannel(ctx, c, eventTypes) }, [icon('settings')]),
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
  })), deliveryLog(log, channels));
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

function editChannel(ctx, existing, eventTypes = EVENT_TYPES) {
  const saved = parseConfig(existing && existing.config);

  openDialog(existing ? 'Edit channel' : 'New channel', (body, close) => {
    const type = el('select', {}, [
      el('option', { value: 'ntfy', text: 'ntfy (push notifications)' }),
      el('option', { value: 'webhook', text: 'Webhook (generic HTTP POST)' }),
    ]);
    type.value = (existing && existing.type) || 'ntfy';

    const label = el('input', {
      type: 'text',
      value: (existing && existing.label) || '',
      placeholder: 'e.g. server alerts',
    });

    // ntfy: structured fields instead of raw JSON.
    const ntfyServer = el('input', { type: 'text', value: saved.server || 'https://ntfy.sh' });
    const ntfyTopic = el('input', { type: 'text', value: saved.topic || '', placeholder: 'my-smart-alerts' });
    const ntfyToken = el('input', { type: 'password', value: saved.token || '', placeholder: 'leave blank for public topics' });

    const ntfyGroup = el('div', { class: 'stack', style: 'gap:var(--space-3)' }, [
      el('div', { class: 'field' }, [el('label', { text: 'Server' }), ntfyServer,
        hint('ntfy.sh, or your own self-hosted server.')]),
      el('div', { class: 'field' }, [el('label', { text: 'Topic' }), ntfyTopic,
        hint('Subscribe to this topic in the ntfy app to receive the alerts.')]),
      el('div', { class: 'field' }, [el('label', { text: 'Access token (optional)' }), ntfyToken,
        hint('Can also come from the SMARTSERVE_NTFY_TOKEN environment variable (ADR-013).')]),
    ]);

    // webhook: URL + method + "Name: value" header lines.
    const hookURL = el('input', { type: 'text', value: saved.url || '', placeholder: 'https://example.com/hook' });
    const hookMethod = el('select', {}, [
      el('option', { value: 'POST', text: 'POST' }),
      el('option', { value: 'PUT', text: 'PUT' }),
    ]);
    hookMethod.value = saved.method || 'POST';
    const hookHeaders = el('textarea', {
      value: headersToText(saved.headers),
      placeholder: 'Authorization: Bearer abc123',
    });

    const hookGroup = el('div', { class: 'stack', style: 'gap:var(--space-3)' }, [
      el('div', { class: 'field' }, [el('label', { text: 'URL' }), hookURL,
        hint('Smart-Serve sends a JSON body describing the event.')]),
      el('div', { class: 'field' }, [el('label', { text: 'Method' }), hookMethod]),
      el('div', { class: 'field' }, [
        el('label', { text: 'Headers' }),
        hookHeaders,
        hint('One "Name: value" per line — for example an Authorization header.'),
      ]),
    ]);

    const syncType = () => {
      ntfyGroup.hidden = type.value !== 'ntfy';
      hookGroup.hidden = type.value !== 'webhook';
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
      if (type.value === 'ntfy') {
        const config = {
          server: ntfyServer.value.trim() || 'https://ntfy.sh',
          topic: ntfyTopic.value.trim(),
        };
        if (ntfyToken.value.trim()) config.token = ntfyToken.value.trim();
        return config;
      }
      return {
        url: hookURL.value.trim(),
        method: hookMethod.value,
        headers: textToHeaders(hookHeaders.value),
      };
    }

    function validate() {
      if (type.value === 'ntfy' && !ntfyTopic.value.trim()) return 'Enter an ntfy topic.';
      if (type.value === 'webhook' && !hookURL.value.trim()) return 'Enter the webhook URL.';
      return null;
    }

    body.append(
      el('div', { class: 'field' }, [el('label', { text: 'Type' }), type]),
      el('div', { class: 'field' }, [el('label', { text: 'Label' }), label]),
      ntfyGroup,
      hookGroup,
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

