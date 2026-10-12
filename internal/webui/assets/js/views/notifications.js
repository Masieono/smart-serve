// Notification channels (docs/UI.md §7.4, docs/NOTIFICATIONS.md).
import { el, mount } from '../dom.js';
import { api } from '../api.js';
import { emptyState, errorBox, openDialog, confirmDialog, toast } from '../components.js';
import { icon } from '../icons.js';

const EVENT_TYPES = ['health_change', 'temperature_high', 'device_disappeared', 'device_appeared', 'scan_failure'];

export async function render(container, ctx) {
  mount(container, el('div', { class: 'skeleton', style: 'width:30%;height:24px' }));
  try {
    draw(container, ctx, await api.channels());
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

function draw(container, ctx, data) {
  const channels = data.channels || [];
  const head = el('div', { class: 'page-head' }, [
    el('div', {}, [
      el('h1', { text: 'Notifications' }),
      el('div', { class: 'sub', text: 'Alerts fire on health transitions — never on every scan.' }),
    ]),
    el('span', { class: 'spacer' }),
    el('button', { class: 'btn btn-primary', type: 'button', onclick: () => editChannel(ctx, null) },
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
          onclick: async () => {
            try {
              await api.testChannel(c.id);
              toast('Test event sent', 'good');
            } catch (err) {
              toast(err.message, 'error');
            }
          } }, ['Test']),
        el('button', { class: 'btn btn-sm btn-ghost', type: 'button', title: 'Edit',
          onclick: () => editChannel(ctx, c) }, [icon('settings')]),
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
  })));
}

function editChannel(ctx, existing) {
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
    const eventRows = EVENT_TYPES.map((name) => {
      const box = el('input', { type: 'checkbox', checked: subscribed.has(name) });
      box.dataset.event = name;
      return el('label', { class: 'field-row' }, [box, el('span', { text: name })]);
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

