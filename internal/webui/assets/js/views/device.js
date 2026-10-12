// Device detail (docs/UI.md §7.2).
import { el, mount, clear, fmtBytes, fmtDateTime, fmtAgo, fmtTemp, fmtHours, fmtScore, scoreGrade } from '../dom.js';
import { api } from '../api.js';
import { verdictBadge, healthGauge, attributeTable, errorBox, openDialog, toast } from '../components.js';
import { icon } from '../icons.js';

export async function render(container, ctx, serial) {
  mount(container, el('div', { class: 'skeleton', style: 'width:40%;height:24px' }));
  try {
    draw(container, ctx, await api.device(serial));
  } catch (err) {
    mount(container, errorBox(`Could not load device: ${err.message}`));
  }
}

function draw(container, ctx, data) {
  const d = data.device;
  const snapshot = data.snapshot || null;
  const payload = snapshot && snapshot.attributes ? snapshot.attributes : null;
  const attrs = payload ? payload.attributes || [] : [];
  const nvme = payload ? payload.nvme : null;

  const stats = el('div', { class: 'form-grid' }, [
    stat('Capacity', fmtBytes(d.capacity_bytes)),
    stat('Temperature', fmtTemp(d.temperature_c)),
    stat('Power-on hours', snapshot && snapshot.power_on_hours !== undefined ? fmtHours(snapshot.power_on_hours) : '—'),
    stat('Transport', `${d.transport || '—'}${d.smart_bus ? ` (${d.smart_bus})` : ''}`),
    stat('Mountpoints', d.mountpoints && d.mountpoints.length ? d.mountpoints.join(', ') : 'not mounted'),
    stat('Filesystem', d.fstype || '—'),
    stat('Serial', d.serial),
    stat('First seen', fmtDateTime(d.first_seen)),
    stat('Last scanned', d.last_scan ? `${fmtAgo(d.last_scan)} (${fmtDateTime(d.last_scan)})` : 'never'),
  ]);

  mount(container,
    el('div', { class: 'page-head' }, [
      el('button', { class: 'btn btn-ghost btn-sm', type: 'button', title: 'Back to devices',
        onclick: () => ctx.navigate('/') }, [icon('back')]),
      el('div', { style: 'min-width:0' }, [
        el('h1', { text: d.name || d.model || d.device_path }),
        el('div', { class: 'sub', text: `${d.model || '—'}${d.device_path ? ` · ${d.device_path}` : ''}` }),
      ]),
      el('span', { class: 'spacer' }),
      el('button', { class: 'btn btn-sm', type: 'button', onclick: () => ctx.scan() }, [icon('scan'), 'Scan']),
      el('a', { class: 'btn btn-sm', href: api.reportUrl(d.serial, 'txt'), download: '' }, [icon('download'), 'Report']),
      el('button', { class: 'btn btn-sm', type: 'button', onclick: () => editNotes(ctx, d) }, [icon('save'), 'Notes']),
    ]),

    el('div', { class: 'card stack' }, [
      el('div', { class: 'row' }, [
        healthGauge(d.score, 112),
        el('div', { class: 'stack', style: 'gap:var(--space-2);flex:1;min-width:0' }, [
          el('div', { class: 'row' }, [
            verdictBadge(d.health),
            el('span', { class: 'muted', text: `${fmtScore(d.score)}/100 · ${scoreGrade(d.score)}` }),
          ]),
          el('div', { text: d.health_rationale || '—' }),
          el('div', { class: 'muted', style: 'font-size:var(--text-xs)',
            text: d.smart_available
              ? 'S.M.A.R.T. data available'
              : `S.M.A.R.T. unavailable: ${d.smart_error || 'unknown reason'}` }),
        ]),
      ]),
    ]),

    el('div', { class: 'card' }, [el('h2', { style: 'margin-bottom:var(--space-4)', text: 'Details' }), stats]),

    d.notes ? el('div', { class: 'card' }, [
      el('h2', { style: 'margin-bottom:var(--space-2)', text: 'Notes' }),
      el('p', { text: d.notes }),
    ]) : null,

    el('div', { class: 'card' }, [
      el('div', { class: 'row', style: 'margin-bottom:var(--space-3)' }, [
        el('h2', { text: nvme ? 'NVMe health log' : 'S.M.A.R.T. attributes' }),
        el('span', { class: 'spacer' }),
        el('button', { class: 'btn btn-sm btn-ghost', type: 'button',
          onclick: (e) => toggleRaw(d, e.currentTarget) }, ['Show raw output']),
      ]),
      nvme ? nvmeTable(nvme) : attributeTable(attrs),
      el('div', { id: 'raw-holder' }),
    ]),
  );
}

function stat(label, value) {
  return el('div', {}, [
    el('span', { class: 'stat-label', text: label }),
    el('span', { class: 'stat-value', text: String(value) }),
  ]);
}

function nvmeTable(n) {
  const rows = [
    ['Critical warning', n.critical_warning],
    ['Available spare', `${n.available_spare}%`],
    ['Spare threshold', `${n.available_spare_threshold}%`],
    ['Percentage used', `${n.percentage_used}%`],
    ['Power cycles', n.power_cycles],
    ['Power-on hours', n.power_on_hours],
    ['Unsafe shutdowns', n.unsafe_shutdowns],
    ['Media errors', n.media_errors],
    ['Error log entries', n.num_err_log_entries],
  ];
  return el('div', { class: 'table-wrap' }, [
    el('table', { class: 'data' }, [
      el('thead', {}, [el('tr', {}, [el('th', { text: 'Field' }), el('th', { class: 'num', text: 'Value' })])]),
      el('tbody', {}, rows.map(([k, v]) =>
        el('tr', {}, [el('td', { text: k }), el('td', { class: 'num', text: String(v) })]))),
    ]),
  ]);
}

async function toggleRaw(device, button) {
  const holder = document.getElementById('raw-holder');
  if (!holder) return;
  if (holder.dataset.open === 'true') {
    clear(holder);
    holder.dataset.open = 'false';
    button.textContent = 'Show raw output';
    return;
  }
  button.disabled = true;
  try {
    const raw = await api.raw(device.serial);
    let pretty = raw;
    try {
      pretty = JSON.stringify(JSON.parse(raw), null, 2);
    } catch {
      /* leave the raw text as-is */
    }
    holder.append(el('pre', {
      class: 'mono',
      style: 'max-height:320px;overflow:auto;background:var(--surface-2);padding:var(--space-3);border-radius:var(--radius-sm)',
    }, [pretty]));
    holder.dataset.open = 'true';
    button.textContent = 'Hide raw output';
  } catch (err) {
    toast(err.message, 'error');
  } finally {
    button.disabled = false;
  }
}

function editNotes(ctx, device) {
  openDialog('Device notes', (body, close) => {
    const name = el('input', { type: 'text', value: device.name || '', placeholder: 'e.g. boot, backup pool' });
    const tags = el('input', { type: 'text', value: device.tags || '', placeholder: 'e.g. critical,os' });
    const notes = el('textarea', { value: device.notes || '', placeholder: 'Anything worth remembering about this drive' });
    const hidden = el('input', { type: 'checkbox', checked: Boolean(device.hidden) });

    body.append(
      el('div', { class: 'field' }, [el('label', { text: 'Name' }), name]),
      el('div', { class: 'field' }, [el('label', { text: 'Tags' }), tags]),
      el('div', { class: 'field' }, [el('label', { text: 'Notes' }), notes]),
      el('label', { class: 'field-row' }, [hidden, el('span', { text: 'Hide from the dashboard' })]),
      el('div', { class: 'dialog-foot', style: 'border:0;padding:0' }, [
        el('button', { class: 'btn', type: 'button', text: 'Cancel', onclick: close }),
        el('button', {
          class: 'btn btn-primary',
          type: 'button',
          text: 'Save',
          onclick: async (event) => {
            event.currentTarget.disabled = true;
            try {
              await api.setNotes(device.serial, {
                name: name.value.trim(),
                tags: tags.value.trim(),
                notes: notes.value,
                hidden: hidden.checked,
              });
              toast('Notes saved', 'good');
              close();
              ctx.refresh();
            } catch (err) {
              toast(err.message, 'error');
              event.currentTarget.disabled = false;
            }
          },
        }),
      ]),
    );
  });
}

