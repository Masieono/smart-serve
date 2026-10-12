// Schedules (docs/UI.md §7.3).
import { el, mount, fmtDateTime, fmtAgo } from '../dom.js';
import { api } from '../api.js';
import { emptyState, errorBox, openDialog, confirmDialog, toast } from '../components.js';
import { icon } from '../icons.js';

export async function render(container, ctx) {
  mount(container, el('div', { class: 'skeleton', style: 'width:30%;height:24px' }));
  try {
    draw(container, ctx, await api.schedules());
  } catch (err) {
    mount(container, errorBox(`Could not load schedules: ${err.message}`));
  }
}

function describe(s) {
  if (s.kind === 'interval') {
    const secs = s.every_seconds || 0;
    if (secs % 3600 === 0) return `every ${secs / 3600} h`;
    if (secs % 60 === 0) return `every ${secs / 60} min`;
    return `every ${secs} s`;
  }
  return `cron ${s.cron}`;
}

function draw(container, ctx, data) {
  const schedules = data.schedules || [];
  const head = el('div', { class: 'page-head' }, [
    el('div', {}, [
      el('h1', { text: 'Schedules' }),
      el('div', { class: 'sub', text: 'Recurring scans run inside the container — no host cron needed.' }),
    ]),
    el('span', { class: 'spacer' }),
    el('button', { class: 'btn btn-primary', type: 'button', onclick: () => editSchedule(ctx, null) },
      [icon('plus'), 'New schedule']),
  ]);

  if (!schedules.length) {
    mount(container, head, emptyState('No schedules yet',
      'Create one to scan automatically — for example every night at 03:00.'));
    return;
  }

  mount(container, head, el('div', { class: 'card' }, [
    el('div', { class: 'table-wrap' }, [
      el('table', { class: 'data' }, [
        el('thead', {}, [el('tr', {}, ['Label', 'When', 'Next run', 'Last run', 'Enabled', '']
          .map((h) => el('th', { text: h })))]),
        el('tbody', {}, schedules.map((s) => el('tr', {}, [
          el('td', { text: s.label || `#${s.id}` }),
          el('td', { text: describe(s) }),
          el('td', { text: s.next_run ? fmtDateTime(s.next_run) : '—' }),
          el('td', { text: s.last_run ? fmtAgo(s.last_run) : 'never' }),
          el('td', {}, [el('input', {
            type: 'checkbox',
            checked: s.enabled,
            'aria-label': 'Enabled',
            onchange: async (e) => {
              try {
                await api.updateSchedule({ ...s, enabled: e.target.checked });
                toast('Schedule updated', 'good');
                ctx.refresh();
              } catch (err) {
                toast(err.message, 'error');
                e.target.checked = !e.target.checked;
              }
            },
          })]),
          el('td', {}, [el('div', { class: 'row' }, [
            el('button', { class: 'btn btn-sm btn-ghost', type: 'button', title: 'Run now',
              onclick: async () => {
                try {
                  await api.runSchedule(s.id);
                  toast('Scan started');
                } catch (err) {
                  toast(err.message, 'error');
                }
              } }, [icon('scan')]),
            el('button', { class: 'btn btn-sm btn-ghost', type: 'button', title: 'Edit',
              onclick: () => editSchedule(ctx, s) }, [icon('settings')]),
            el('button', { class: 'btn btn-sm btn-ghost', type: 'button', title: 'Delete',
              onclick: async () => {
                if (!(await confirmDialog('Delete schedule', `Delete "${s.label || s.id}"?`, 'Delete'))) return;
                try {
                  await api.deleteSchedule(s.id);
                  toast('Schedule deleted', 'good');
                  ctx.refresh();
                } catch (err) {
                  toast(err.message, 'error');
                }
              } }, [icon('trash')]),
          ])]),
        ]))),
      ]),
    ]),
  ]));
}

function editSchedule(ctx, existing) {
  openDialog(existing ? 'Edit schedule' : 'New schedule', (body, close) => {
    const label = el('input', { type: 'text', value: (existing && existing.label) || '', placeholder: 'e.g. nightly' });
    const kind = el('select', {}, [
      el('option', { value: 'interval', text: 'Interval' }),
      el('option', { value: 'cron', text: 'Cron expression' }),
    ]);
    kind.value = (existing && existing.kind) || 'interval';

    const defaultHours = existing && existing.kind === 'interval'
      ? Math.round((existing.every_seconds || 21600) / 3600)
      : 6;
    const hours = el('input', { type: 'number', min: '1', value: String(defaultHours) });
    const cron = el('input', { type: 'text', value: (existing && existing.cron) || '0 3 * * *', placeholder: '0 3 * * *' });
    const enabled = el('input', { type: 'checkbox', checked: existing ? existing.enabled : true });

    const intervalRow = el('div', { class: 'field' }, [el('label', { text: 'Every (hours)' }), hours]);
    const cronRow = el('div', { class: 'field' }, [
      el('label', { text: 'Cron (minute hour day month weekday)' }),
      cron,
      el('span', { class: 'muted', style: 'font-size:var(--text-xs)', text: 'Example: 0 3 * * * runs daily at 03:00.' }),
    ]);
    const sync = () => {
      intervalRow.hidden = kind.value !== 'interval';
      cronRow.hidden = kind.value !== 'cron';
    };
    kind.addEventListener('change', sync);
    sync();

    body.append(
      el('div', { class: 'field' }, [el('label', { text: 'Label' }), label]),
      el('div', { class: 'field' }, [el('label', { text: 'Type' }), kind]),
      intervalRow,
      cronRow,
      el('label', { class: 'field-row' }, [enabled, el('span', { text: 'Enabled' })]),
      el('div', { class: 'dialog-foot', style: 'border:0;padding:0' }, [
        el('button', { class: 'btn', type: 'button', text: 'Cancel', onclick: close }),
        el('button', { class: 'btn btn-primary', type: 'button', text: 'Save', onclick: async (event) => {
          const payload = {
            label: label.value.trim(),
            kind: kind.value,
            enabled: enabled.checked,
            every_seconds: kind.value === 'interval' ? Number(hours.value) * 3600 : null,
            cron: kind.value === 'cron' ? cron.value.trim() : '',
          };
          if (existing) payload.id = existing.id;
          event.currentTarget.disabled = true;
          try {
            if (existing) await api.updateSchedule(payload);
            else await api.createSchedule(payload);
            toast('Schedule saved', 'good');
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

