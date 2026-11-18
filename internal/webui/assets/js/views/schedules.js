// Schedules (v2), what runs, when it next runs, and whether it is on.
//
// A schedule row answers three questions in the order a reader asks them: what is this, when does it
// happen next, and did it happen last time. The incumbent table answered them in a column order that
// put "Enabled" before the buttons and "when" before "next run", which is the same information read
// back to front.
//
// The editor is the incumbent's dialog, imported rather than rebuilt (`ADR-032`): the cron-or-interval
// switch with its live preview is the hardest part of that screen to get right, and none of it is a
// layout decision.
import { el, clear, append, mount, fmtAgo, fmtDateTime, fmtTime } from '../dom.js';
import { api } from '../api.js';
import { emptyState, errorBox, toast, confirmDialog } from '../components.js';
import { describe, describeAction, editSchedule } from './schedule-editor.js';

export async function render(container, ctx) {
  clear(container);
  mount(container, el('div', { class: 'skeleton', style: 'width:26%;height:24px' }));

  let data;
  try {
    data = await api.schedules();
  } catch (err) {
    clear(container);
    mount(container, errorBox(`Could not load schedules: ${err.message}`));
    return;
  }
  draw(container, ctx, data);
}

function draw(container, ctx, data) {
  const schedules = data.schedules || [];
  clear(container);

  append(container, [
    el('div', { class: 'fleet-head' }, [
      el('h1', { text: 'Schedules' }),
      el('button', {
        class: 'fleet-scan',
        type: 'button',
        text: 'New schedule',
        onclick: () => editSchedule(ctx, null),
      }),
    ]),
    el('div', { class: 'fleet-glance' }, glance(schedules)),
    passBanner(data),
    schedules.length ? scheduleTable(schedules, ctx) : el('div', { class: 'fleet-empty' }, [
      el('div', { text: 'No schedules yet.' }),
      el('div', {
        class: 'muted',
        text: 'Create one to scan automatically, every night at 03:00 is the usual choice.',
      }),
    ]),
  ].filter(Boolean));
}

/** Counts, then the thing a reader actually wants: when the next one runs. */
function glance(schedules) {
  const enabled = schedules.filter((s) => s.enabled);
  const next = enabled
    .map((s) => s.next_run)
    .filter(Boolean)
    .sort()[0];

  const parts = [
    el('span', { class: 'count' }, [el('b', { text: String(schedules.length) }), el('span', { text: ' schedules' })]),
    el('span', { class: 'count' }, [el('b', { text: String(enabled.length) }), el('span', { text: ' on' })]),
  ];
  parts.push(el('span', {
    class: 'when',
    text: next ? `next run ${fmtIn(next)}` : (schedules.length ? 'nothing scheduled to run' : 'nothing scheduled'),
  }));
  return parts;
}

/**
 * A self-test pass takes hours and writes to every drive in the fleet.
 *
 * Without this, a pass that has been running for six hours looks exactly like one that finished six
 * hours ago, the row's "last run" cannot tell them apart.
 */
function passBanner(data) {
  const pass = data.selftest_pass;
  if (!pass || !pass.running) return null;
  const pct = pass.total ? Math.round((pass.done / pass.total) * 100) : 0;

  return el('div', { class: 'v2-pass' }, [
    el('div', { class: 'v2-pass-head' }, [
      el('b', { text: `Self-test pass running, ${pass.done} of ${pass.total} drives done` }),
      el('span', { class: 'muted', text: pass.current ? `testing ${pass.current} now` : 'between drives' }),
    ]),
    el('div', { class: 'v2-pass-bar' }, [el('div', { class: 'v2-pass-fill', style: `width:${pct}%` })]),
  ]);
}

function scheduleTable(schedules, ctx) {
  const body = el('tbody', {});
  for (const s of schedules) {
    append(body, [el('tr', { class: s.enabled ? null : 'missing' }, [
      el('td', { class: 'drive' }, [
        el('div', { class: 'name', text: s.label || `Schedule ${s.id}` }),
        el('div', { class: 'sub', text: describeAction(s) }),
      ]),
      el('td', {}, [el('div', { text: describe(s) })]),
      el('td', {}, s.enabled
        ? [
          el('div', { text: s.next_run ? fmtIn(s.next_run) : 'n/a' }),
          el('div', {
            class: 'sub',
            text: s.next_run ? fmtDateTime(s.next_run) : '',
            title: s.next_run || '',
          }),
        ]
        : [el('div', { class: 'muted', text: 'paused' })]),
      el('td', {}, [el('div', { text: s.last_run ? fmtAgo(s.last_run) : 'never' })]),
      el('td', {}, [el('input', {
        type: 'checkbox',
        class: 'v2-toggle',
        checked: s.enabled,
        'aria-label': `Enabled: ${s.label || `schedule ${s.id}`}`,
        onchange: async (event) => {
          const box = event.target;
          try {
            await api.updateSchedule({ ...s, enabled: box.checked });
            toast(box.checked ? 'Schedule on' : 'Schedule paused', 'good');
            ctx.refresh();
          } catch (err) {
            toast(err.message, 'error');
            box.checked = !box.checked;
          }
        },
      })]),
      el('td', { class: 'r' }, [el('div', { class: 'v2-row-actions' }, [
        el('button', {
          class: 'v2-btn',
          type: 'button',
          text: 'Run now',
          title: 'Run this schedule once, now',
          onclick: async (event) => {
            const b = event.currentTarget;
            b.disabled = true;
            b.textContent = 'Starting…';
            try {
              await api.runSchedule(s.id);
              // A self-test fire queues a pass rather than doing the work here; saying "scan started"
              // for one would be a claim the reader cannot check.
              toast(s.action === 'selftest' ? 'Self-test pass queued' : 'Schedule started');
            } catch (err) {
              toast(err.message, 'error');
            } finally {
              b.disabled = false;
              b.textContent = 'Run now';
            }
          },
        }),
        el('button', { class: 'v2-btn', type: 'button', text: 'Edit', onclick: () => editSchedule(ctx, s) }),
        el('button', {
          class: 'v2-btn',
          type: 'button',
          text: 'Delete',
          onclick: async () => {
            if (!(await confirmDialog('Delete schedule', `Delete "${s.label || `#${s.id}`}"?`, 'Delete'))) return;
            try {
              await api.deleteSchedule(s.id);
              toast('Schedule deleted', 'good');
              ctx.refresh();
            } catch (err) {
              toast(err.message, 'error');
            }
          },
        }),
      ])]),
    ])]);
  }

  return el('div', { class: 'v2-table-wrap' }, [
    el('table', { class: 'fleet-table v2-schedules' }, [
      el('colgroup', {}, [
        el('col', { class: 'drive' }), el('col', { class: 'cadence' }), el('col', { class: 'next' }),
        el('col', { class: 'last' }), el('col', { class: 'on' }), el('col', { class: 'actions' }),
      ]),
      el('thead', {}, [el('tr', {}, [
        el('th', { text: 'Schedule' }),
        el('th', { text: 'Runs' }),
        el('th', { text: 'Next run' }),
        el('th', { text: 'Last run' }),
        el('th', { text: 'On' }),
        el('th', { class: 'r', text: '' }),
      ])]),
      body,
    ]),
  ]);
}

/** "in 6h", "in 3 days", "in 40s", the future counterpart of `fmtAgo`. */
function fmtIn(iso) {
  const ms = Date.parse(iso) - Date.now();
  if (!Number.isFinite(ms)) return 'n/a';
  if (ms <= 0) return 'due now';

  const mins = Math.round(ms / 60000);
  if (mins < 1) return 'in under a minute';
  if (mins < 60) return `in ${mins}m`;
  const hours = Math.floor(mins / 60);
  const rest = mins % 60;
  if (hours < 24) return rest ? `in ${hours}h ${rest}m` : `in ${hours}h`;
  const days = Math.floor(hours / 24);
  const rh = hours % 24;
  if (days < 14) return rh ? `in ${days}d ${rh}h` : `in ${days}d`;
  return `in ${Math.round(days / 7)} weeks`;
}
