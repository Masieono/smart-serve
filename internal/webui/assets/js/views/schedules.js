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

/**
 * The action's name for the list, spelled out rather than implied by the label.
 *
 * A digest schedule and a scan schedule look identical otherwise, and confusing the two is confusing
 * "it scanned" with "it wrote to me". A self-test schedule is the only one that writes to a *drive*,
 * so it names the test it runs as well.
 */
function describeAction(s) {
  if (s.action === 'digest') return 'Digest';
  if (s.action === 'selftest') return `Self-test · ${s.selftest_kind === 'long' ? 'long' : 'short'}`;
  return 'Scan';
}

/**
 * What a running self-test pass is doing, or null.
 *
 * A pass takes hours and writes to every drive in the fleet, so the row's "last run" is not enough
 * for a reader to know what is happening: without this, a pass that has been running for six hours
 * looks exactly like one that finished six hours ago.
 */
function passNotice(data) {
  const pass = data.selftest_pass;
  if (!pass || !pass.running) return null;
  const on = pass.current ? ` Testing ${pass.current} now.` : '';
  return el('div', { class: 'notice' }, [
    el('div', { class: 'notice-title', text: `Self-tests running — ${pass.done} of ${pass.total} drives done` }),
    el('div', { text: `One drive at a time, so a fleet does not start every test at once.${on}` }),
  ]);
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

  mount(container, head, [passNotice(data), el('div', { class: 'card' }, [
    el('div', { class: 'table-wrap' }, [
      el('table', { class: 'data' }, [
        el('thead', {}, [el('tr', {}, ['Label', 'Action', 'When', 'Next run', 'Last run', 'Enabled', '']
          .map((h) => el('th', { text: h })))]),
        el('tbody', {}, schedules.map((s) => el('tr', {}, [
          el('td', { text: s.label || `#${s.id}` }),
          // Spelled out rather than implied by the label: a digest schedule and a scan schedule look
          // identical otherwise, and confusing the two is confusing "it scanned" with "it wrote to me".
          el('td', { text: describeAction(s) }),
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
                  // A self-test fire queues a pass rather than doing the work here, and saying
                  // "scan started" for one would be a lie the reader has no way to check.
                  toast(s.action === 'selftest' ? 'Self-test pass queued' : 'Schedule started');
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
  ])]);
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
      el('option', { value: 'selftest', text: 'Run a self-test on each drive' }),
    ]);
    action.value = (existing && existing.action) || 'scan';

    // The test's length, which is the whole difference between a pass that costs minutes and one that
    // costs most of a day. Offered only for a self-test, for the same reason as the digest's hint.
    const selfTestKind = el('select', {}, [
      el('option', { value: 'short', text: 'Short — a few minutes per drive' }),
      el('option', { value: 'long', text: 'Long — hours per drive' }),
    ]);
    selfTestKind.value = (existing && existing.selftest_kind) || 'short';
    const kindRow = el('div', { class: 'field' }, [el('label', { text: 'Test' }), selfTestKind]);

    // A digest has no destination of its own, so the modal has to say where it goes — that was the
    // question the old one-line hint left open. It appears only for the action it explains: an
    // explanation of a choice the reader has not made is noise on a form they are trying to fill in.
    const actionHint = el('p', { class: 'muted field-hint' });
    const syncActionHint = () => {
      const digest = action.value === 'digest';
      const selfTest = action.value === 'selftest';
      kindRow.hidden = !selfTest;
      actionHint.hidden = !(digest || selfTest);
      if (digest) {
        actionHint.textContent =
          'One summary of everything since this last ran, sent through your notification channels — '
          + 'it has no destination of its own. A channel with no events ticked receives it; a '
          + 'channel limited to particular events needs “digest” ticked.';
      } else if (selfTest) {
        actionHint.textContent =
          'Every drive that can take one, one at a time in serial order, so a fleet does not start '
          + 'every test at once. A drive already testing itself is skipped, and each drive is re-read '
          + 'as its test finishes — so the result is stored and notified like a manual one. This is '
          + 'the one schedule that writes to the drives.';
      } else {
        actionHint.textContent = '';
      }
    };
    action.addEventListener('change', syncActionHint);
    syncActionHint();

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
      el('div', { class: 'field' }, [el('label', { text: 'Action' }), action, actionHint]),
      kindRow,
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
          action: action.value, selftest_kind: selfTestKind.value };
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
    // The form this option exists for. Setting up a digest means typing a label, picking an action
    // and reading the preview, and the modal used to vanish if the pointer left the box mid-drag —
    // see `openDialog`. Cancel and Escape still close it.
  }, { dismissOnBackdrop: false });
}

