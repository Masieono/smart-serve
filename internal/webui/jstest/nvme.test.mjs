// The NVMe health log.
//
// Sixteen counters in a flat list read as a wall — health, lifetime, workload and error
// figures all look alike at a glance. The grouping is the fix, so the grouping is what
// this pins.

import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, StubNode } from './stub-dom.mjs';

installStubDom();

const { nvmeTable } = await import('../assets/js/views/device.js');

/** Every string in a subtree, in document order. */
function textOf(node) {
  const out = [];
  const walk = (n) => {
    for (const child of n.childNodes) {
      if (!(child instanceof StubNode)) continue;
      if (child.tagName === '#TEXT') out.push(child.data);
      else if (child.textContent) out.push(child.textContent);
      walk(child);
    }
  };
  walk(node);
  return out.join(' ');
}

const HEALTH = {
  critical_warning: 0, available_spare: 100, available_spare_threshold: 10,
  percentage_used: 4, data_units_read: 1000, data_units_written: 2000,
  host_reads: 10, host_writes: 20, controller_busy_time: 5, power_cycles: 30,
  power_on_hours: 40, unsafe_shutdowns: 2, media_errors: 0, num_err_log_entries: 0,
  warning_temp_time: 0, critical_comp_time: 0,
};

test('the NVMe health log is grouped into sections', () => {
  const text = textOf(nvmeTable(HEALTH));
  for (const section of ['Health', 'Lifetime', 'Workload', 'Errors']) {
    assert.match(text, new RegExp(`\\b${section}\\b`));
  }
});

// The sections are landmarks, not a licence to drop a field. Every counter the API sends
// has to reach the table.
test('every counter still reaches the table', () => {
  const text = textOf(nvmeTable(HEALTH));
  for (const label of [
    'Critical warning', 'Available spare', 'Spare threshold', 'Percentage used',
    'Power-on hours', 'Power cycles', 'Unsafe shutdowns', 'Controller busy time',
    'Warning temp time', 'Critical comp time', 'Data units read', 'Data units written',
    'Host read commands', 'Host write commands', 'Media errors', 'Error log entries',
  ]) {
    assert.match(text, new RegExp(label.replace(/[-.]/g, '.')), `missing ${label}`);
  }
});

// A missing field must render as a dash rather than "undefined" or "NaN%".
test('absent counters do not render as undefined', () => {
  const text = textOf(nvmeTable({ critical_warning: 0, available_spare: 100 }));
  assert.doesNotMatch(text, /undefined/);
  assert.doesNotMatch(text, /NaN/);
});
