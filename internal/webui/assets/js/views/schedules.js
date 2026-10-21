// Schedules (docs/UI.md §7.3).
import { el, mount, fmtDateTime, fmtAgo } from '../dom.js';
import { api } from '../api.js';
import { emptyState, errorBox, openDialog, confirmDialog, toast } from '../components.js';
import { icon } from '../icons.js';
import { describeCron, cronFieldError, buildDailyCron, parseDailyCron, WEEKDAY_NAMES } from '../cron.js';

export async function render(container, ctx) {
  mount(container, el('div', { class: 'skeleton', style: 'width:30%;height:24px' }));
  try {
    draw(container, ctx, await api.schedules());
  } catch (err) {
    mount(container, errorBox(`Could not load schedules: ${err.message}`));
  }
}

function describe(s) {
  if (s.kind === 'interval') return `every ${humanInterval(s.every_seconds || 0)}`;
  return describeCron(s.cron) || `cron ${s.cron}`;
}

/** Human phrasing for an interval, used in the list and the live preview. */
function humanInterval(seconds) {
  const plural = (n, word) => `${n} ${word}${n === 1 ? '' : 's'}`;
  if (seconds % 86400 === 0) return plural(seconds / 86400, 'day');
  if (seconds % 3600 === 0) return plural(seconds / 3600, 'hour');
  if (seconds % 60 === 0) return plural(seconds / 60, 'minute');
  return plural(seconds, 'second');
}

function draw(container, ctx, data) {
  const schedules = data.schedules || [];
  const head = el('div', { class: 'page-head' }, [
    el('div', {}, [
      el('h1', { text: 'Schedules' }),
      el('div', { class: 'sub', text: 'Recurring scans run inside the container, so no host cron is needed.' }),
    ]),
    el('span', { class: 'spacer' }),
    el('button', { class: 'btn btn-primary', type: 'button', onclick: () => editSchedule(ctx, null) },
      [icon('plus'), 'New schedule']),
  ]);

  if (!schedules.length) {
    mount(container, head, emptyState('No schedules yet',
      'Create one to scan automatically, for example every night at 03:00.'));
    return;
  }

  mount(container, head, el('div', { class: 'card' }, [
    el('div', { class: 'table-wrap' }, [
      el('table', { class: 'data' }, [
        el('thead', {}, [el('tr', {}, ['Label', 'Action', 'When', 'Next run', 'Last run', 'Enabled', '']
          .map((h) => el('th', { text: h })))]),
        el('tbody', {}, schedules.map((s) => el('tr', {}, [
          el('td', { text: s.label || `#${s.id}` }),
          // Spelled out rather than implied by the label: a digest schedule and a scan schedule look
          // identical otherwise, and confusing the two is confusing "it scanned" with "it wrote to me".
          el('td', { text: s.action === 'digest' ? 'Digest' : 'Scan' }),
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
  openDialog(existing ? 'Edit schedule' : 'New schedule', (body, close, foot) => {
    const label = el('input', { type: 'text', value: (existing && existing.label) || '', placeholder: 'e.g. nightly' });

    // What the schedule *does* (`FEATURES.md` B13), which is a separate axis from when it runs. A
    // digest is weekly in practice, and weekly is a timing question the controls below already
    // answer, so the two are chosen independently.
    const action = el('select', {}, [
      el('option', { value: 'scan', text: 'Scan the drives' }),
      el('option', { value: 'digest', text: 'Send a digest of the period' }),
    ]);
    action.value = (existing && existing.action) || 'scan';

    const mode = el('select', {}, [
      el('option', { value: 'interval', text: 'Every few minutes / hours / days' }),
      el('option', { value: 'daily', text: 'On chosen days, at a time' }),
      el('option', { value: 'cron', text: 'Cron expression' }),
    ]);

    const everyValue = el('input', { type: 'number', min: '1', value: '6', style: 'max-width:120px' });
    const everyUnit = el('select', {}, [
      el('option', { value: '60', text: 'minutes' }),
      el('option', { value: '3600', text: 'hours' }),
      el('option', { value: '86400', text: 'days' }),
    ]);
    everyUnit.value = '3600';

    const time = el('input', { type: 'time', value: '03:00', style: 'max-width:160px' });
    const weekdayRows = WEEKDAY_NAMES.map((name, index) => {
      const box = el('input', { type: 'checkbox' });
      box.dataset.day = String(index);
      return el('label', { class: 'field-row' }, [box, el('span', { text: name })]);
    });

    const cronInput = el('input', { type: 'text', value: '0 3 * * *', placeholder: '0 3 * * *' });
    const preview = el('div', { class: 'muted', style: 'font-size:var(--text-xs)' });

    const intervalRow = el('div', { class: 'field' }, [
      el('label', { text: 'Run every' }),
      el('div', { class: 'row' }, [everyValue, everyUnit]),
    ]);

    const dailyRow = el('div', { class: 'stack', style: 'gap:var(--space-3)' }, [
      el('div', { class: 'field' }, [el('label', { text: 'At' }), time]),
      el('div', { class: 'field' }, [
        el('label', { text: 'On these days (none selected = every day)' }),
        el('div', { class: 'row wrap', style: 'gap:var(--space-3)' }, weekdayRows),
      ]),
    ]);

    const cronRow = el('div', { class: 'field' }, [
      el('label', { text: 'Cron (minute hour day-of-month month day-of-week)' }),
      cronInput,
    ]);

    const selectedDays = () => weekdayRows
      .filter((row) => row.querySelector('input').checked)
      .map((row) => Number(row.querySelector('input').dataset.day));

    function compute() {
      if (mode.value === 'interval') {
        const value = Math.max(1, Number(everyValue.value) || 1);
        return { kind: 'interval', every_seconds: value * Number(everyUnit.value), cron: '' };
      }
      if (mode.value === 'daily') {
        return { kind: 'cron', every_seconds: null, cron: buildDailyCron(time.value, selectedDays()) };
      }
      return { kind: 'cron', every_seconds: null, cron: cronInput.value.trim() };
    }

    function refresh() {
      intervalRow.hidden = mode.value !== 'interval';
      dailyRow.hidden = mode.value !== 'daily';
      cronRow.hidden = mode.value !== 'cron';

      const { kind, every_seconds: every, cron } = compute();
      if (kind === 'interval') {
        preview.className = 'muted';
        preview.textContent = `Runs every ${humanInterval(every)}.`;
        return;
      }
      const text = describeCron(cron);
      if (text) {
        preview.className = 'muted';
        preview.textContent = `Runs ${text.charAt(0).toLowerCase()}${text.slice(1)}.`;
      } else {
        preview.className = 'error-box';
        preview.textContent = 'Could not interpret that expression. Check the five fields.';
      }
    }

    // Prefill from an existing schedule, choosing the friendliest mode that fits.
    if (existing) {
      if (existing.kind === 'interval') {
        mode.value = 'interval';
        const secs = existing.every_seconds || 3600;
        if (secs % 86400 === 0) {
          everyValue.value = String(secs / 86400);
          everyUnit.value = '86400';
        } else if (secs % 3600 === 0) {
          everyValue.value = String(secs / 3600);
          everyUnit.value = '3600';
        } else {
          everyValue.value = String(Math.max(1, Math.round(secs / 60)));
          everyUnit.value = '60';
        }
      } else {
        const daily = parseDailyCron(existing.cron);
        if (daily) {
          mode.value = 'daily';
          time.value = daily.time;
          for (const row of weekdayRows) {
            const box = row.querySelector('input');
            if (daily.weekdays.includes(Number(box.dataset.day))) box.checked = true;
          }
        } else {
          mode.value = 'cron';
          cronInput.value = existing.cron || '';
        }
      }
    }

    for (const control of [mode, everyValue, everyUnit, time, cronInput]) {
      control.addEventListener('input', refresh);
      control.addEventListener('change', refresh);
    }
    for (const row of weekdayRows) row.querySelector('input').addEventListener('change', refresh);
    refresh();

    const enabled = el('input', { type: 'checkbox', checked: existing ? existing.enabled : true });

    body.append(
      el('div', { class: 'field' }, [el('label', { text: 'Label' }), label]),
      el('div', { class: 'field' }, [el('label', { text: 'Action' }), action,
        el('p', { class: 'muted field-hint', text: 'A digest sends one summary of what happened ' +
          'since this last ran, instead of a message per event.' })]),
      el('div', { class: 'field' }, [el('label', { text: 'Repeat' }), mode]),
      intervalRow,
      dailyRow,
      cronRow,
      preview,
      el('label', { class: 'field-row' }, [enabled, el('span', { text: 'Enabled' })]),
    );

    foot.append(
      el('button', { class: 'btn', type: 'button', text: 'Cancel', onclick: close }),
      el('button', { class: 'btn btn-primary', type: 'button', text: 'Save', onclick: async (event) => {
        // Captured before the first `await`: `event.currentTarget` is null once dispatch ends.
        const button = event.currentTarget;
        const { kind, every_seconds, cron } = compute();
        if (kind === 'cron') {
          const problem = cronFieldError(cron);
          if (problem) {
            toast(problem, 'error');
            return;
          }
        }
        const payload = { label: label.value.trim(), kind, enabled: enabled.checked, every_seconds, cron,
          action: action.value };
        if (existing) payload.id = existing.id;

        button.disabled = true;
        try {
          if (existing) await api.updateSchedule(payload);
          else await api.createSchedule(payload);
          toast('Schedule saved', 'good');
          close();
          ctx.refresh();
        } catch (err) {
          toast(err.message, 'error');
          button.disabled = false;
        }
      } }),
    );
  });
}

