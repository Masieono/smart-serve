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
  openDialog(existing ? 'Edit channel' : 'New channel', (body, close) => {
    const type = el('select', {}, [
      el('option', { value: 'ntfy', text: 'ntfy' }),
      el('option', { value: 'webhook', text: 'Webhook' }),
    ]);
    type.value = (existing && existing.type) || 'ntfy';

    const label = el('input', { type: 'text', value: (existing && existing.label) || '', placeholder: 'e.g. server alerts' });
    const config = el('textarea', { value: pretty((existing && existing.config) || '') });
    const enabled = el('input', { type: 'checkbox', checked: existing ? existing.enabled : true });

    const subscribed = new Set(parseEvents(existing && existing.events));
    const eventRows = EVENT_TYPES.map((name) => {
      const box = el('input', { type: 'checkbox', checked: subscribed.has(name) });
      box.dataset.event = name;
      return el('label', { class: 'field-row' }, [box, el('span', { text: name })]);
    });

    const syncPlaceholder = () => {
      config.placeholder = type.value === 'ntfy'
        ? '{"server":"https://ntfy.sh","topic":"my-smart-alerts"}'
        : '{"url":"https://example.com/hook","method":"POST"}';
    };
    type.addEventListener('change', syncPlaceholder);
    syncPlaceholder();

    body.append(
      el('div', { class: 'field' }, [el('label', { text: 'Type' }), type]),
      el('div', { class: 'field' }, [el('label', { text: 'Label' }), label]),
      el('div', { class: 'field' }, [
        el('label', { text: 'Config (JSON)' }),
        config,
        el('span', { class: 'muted', style: 'font-size:var(--text-xs)',
          text: 'Secrets set here can be overridden by environment variables (ADR-013).' }),
      ]),
      el('div', { class: 'field' }, [
        el('label', { text: 'Events' }),
        el('div', { class: 'stack', style: 'gap:var(--space-1)' }, eventRows),
      ]),
      el('label', { class: 'field-row' }, [enabled, el('span', { text: 'Enabled' })]),
      el('div', { class: 'dialog-foot', style: 'border:0;padding:0' }, [
        el('button', { class: 'btn', type: 'button', text: 'Cancel', onclick: close }),
        el('button', { class: 'btn btn-primary', type: 'button', text: 'Save', onclick: async (event) => {
          let parsed;
          try {
            parsed = config.value.trim() ? JSON.parse(config.value) : {};
          } catch {
            toast('Config must be valid JSON', 'error');
            return;
          }
          const events = eventRows
            .filter((row) => row.querySelector('input').checked)
            .map((row) => row.querySelector('input').dataset.event);

          const payload = {
            type: type.value,
            label: label.value.trim(),
            config: parsed,
            events,
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

function pretty(raw) {
  if (!raw || raw === '{}') return '';
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
}

