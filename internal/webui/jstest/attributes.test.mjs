// Attribute help (FEATURES.md A12).
//
// Two things are worth pinning. The first is that the curated table stays in step with the
// interpreter: a description that disagreed with the verdict, or a missing one for a field
// the verdict acts on, would be worse than no help at all. The second is that the help is
// reachable without a mouse — the affordance is a button, not a `title`, and that is the
// whole reason it works on the mobile layout.

import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, StubNode } from './stub-dom.mjs';

installStubDom();

const { ATTRIBUTE_HELP, NVME_HELP, attributeHelp, nvmeHelp, attributeFlagged } =
  await import('../assets/js/attributes.js');
const { attributeTable } = await import('../assets/js/components.js');
const { nvmeTable, logSummary } = await import('../assets/js/views/device.js');

// The ids the interpreter acts on: SMART_INTERPRETATION.md §1.1 (wear) and §2 (the
// CrystalDiskInfo set). If the interpreter grows a new interpreted attribute, this list
// and the help table should grow together — which is the point of asserting on it.
const INTERPRETED_ATA = [5, 9, 10, 12, 187, 190, 194, 196, 197, 198, 199, 169, 177, 202, 231, 233];

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

const helpButtons = (node) =>
  node.all('button').filter((b) => String(b.className).includes('attr-help'));

const attr = (id, name, value, worst, threshold) => ({
  id, name, value, worst, threshold, raw_value: 0, raw_string: '0',
});

test('every attribute the interpreter acts on has an explanation', () => {
  for (const id of INTERPRETED_ATA) {
    const help = attributeHelp(id);
    assert.ok(help, `attribute ${id} is interpreted but has no help text`);
    assert.ok(help.what && help.what.length > 20, `attribute ${id} has a thin explanation`);
  }
});

test('no help entry is empty or placeholder text', () => {
  for (const [id, help] of Object.entries(ATTRIBUTE_HELP)) {
    assert.ok(help.what && help.what.trim().length > 0, `attribute ${id} has no \`what\``);
    if (help.watch !== undefined) {
      assert.ok(help.watch.trim().length > 0, `attribute ${id} has an empty \`watch\``);
    }
  }
  for (const [field, help] of Object.entries(NVME_HELP)) {
    assert.ok(help.what && help.what.trim().length > 0, `NVMe field ${field} has no \`what\``);
  }
});

test('lookup is by numeric id, and unknown ids get nothing', () => {
  assert.equal(attributeHelp('5').what, ATTRIBUTE_HELP[5].what);
  assert.equal(attributeHelp(250), null);
  assert.equal(attributeHelp(undefined), null);
  assert.equal(nvmeHelp('media_errors').what, NVME_HELP.media_errors.what);
  assert.equal(nvmeHelp('not_a_field'), null);
});

test('the attribute table offers help only where there is something to say', () => {
  const table = attributeTable([
    attr(5, 'Reallocated Sector Count', 100, 100, 10),
    attr(250, 'Vendor Mystery', 100, 100, 0),
  ]);
  const buttons = helpButtons(table);
  assert.equal(buttons.length, 1, 'only the known attribute should carry a ?');
  assert.equal(buttons[0].getAttribute('aria-label'), 'What is Reallocated Sector Count?');
  assert.equal(textOf(buttons[0]), '?');
});

test('pressing ? explains the attribute', () => {
  const table = attributeTable([attr(197, 'Current Pending Sector Count', 100, 100, 0)]);
  helpButtons(table)[0].click();

  const dialogs = document.body.all('dialog');
  const shown = dialogs[dialogs.length - 1];
  assert.ok(shown, 'pressing ? should open a dialog');
  const text = textOf(shown);
  assert.match(text, /has not yet given up on/, 'the definition is shown');
  assert.match(text, /retried on the next write/, 'so is how to read it');
});

test('a row the drive reports at its own threshold is marked', () => {
  const table = attributeTable([attr(5, 'Reallocated Sector Count', 10, 10, 10)]);
  const flagged = table.all('tr').filter((r) => String(r.className).includes('attr-flagged'));
  assert.equal(flagged.length, 1);
  assert.match(textOf(flagged[0]), /At threshold/);
});

test('a clean attribute table marks nothing', () => {
  const table = attributeTable([attr(5, 'Reallocated Sector Count', 100, 100, 10)]);
  const flagged = table.all('tr').filter((r) => String(r.className).includes('attr-flagged'));
  assert.equal(flagged.length, 0);
});

// The marker is the drive's own alarm, never a threshold this app invented: a threshold
// of 0 means the attribute has no threshold, not that it fails at zero. Most informational
// attributes report 0, so getting this wrong would flag half of a healthy table.
test('attributeFlagged follows the drive threshold, not a guessed one', () => {
  assert.equal(attributeFlagged(attr(5, 'x', 10, 10, 10)), true, 'at threshold');
  assert.equal(attributeFlagged(attr(5, 'x', 4, 10, 10)), true, 'below threshold');
  assert.equal(attributeFlagged(attr(5, 'x', 11, 10, 10)), false, 'above threshold');
  assert.equal(attributeFlagged(attr(5, 'x', 0, 0, 0)), false, 'no threshold');
  assert.equal(attributeFlagged(null), false, 'no attribute');
});

const NVME_HEALTH = {
  critical_warning: 0, available_spare: 100, available_spare_threshold: 10,
  percentage_used: 4, data_units_read: 1000, data_units_written: 2000,
  host_reads: 10, host_writes: 20, controller_busy_time: 5, power_cycles: 30,
  power_on_hours: 40, unsafe_shutdowns: 2, media_errors: 0, num_err_log_entries: 0,
  warning_temp_time: 0, critical_comp_time: 0,
};

test('every NVMe counter in the table carries a ?', () => {
  const table = nvmeTable(NVME_HEALTH);
  assert.equal(helpButtons(table).length, 16, 'all sixteen counters should be explained');
});

// A missing field must render as a gap, never as the literal word "undefined" — which is
// what a bare `String(value)` produces. The Raw column did exactly that for an attribute
// whose raw value the drive never reported, which reads as data rather than as a gap.
test('a sparse attribute row renders a gap, not "undefined"', () => {
  // Attribute 9 as a vendor that reports no raw value at all.
  const text = textOf(attributeTable([{ id: 9, name: 'Power_On_Hours', value: 95, worst: 95, threshold: 0 }]));
  assert.doesNotMatch(text, /undefined/);
  assert.doesNotMatch(text, /null/);
  assert.doesNotMatch(text, /NaN/);
  assert.match(text, /—/, 'the gap should be visible');
});

// "— min" reads as a measurement of zero minutes. A bare dash is the honest answer.
test('an absent minute counter does not render as "— min"', () => {
  const text = textOf(nvmeTable({ critical_warning: 0, available_spare: 100 }));
  assert.doesNotMatch(text, /undefined|null|NaN/);
  assert.doesNotMatch(text, /— min/);
});

// The precondition that makes the two tests below meaningful: the stub must stringify what
// the browser stringifies. If it quietly dropped a null, a dialog could render the word
// "null" and every assertion here would still pass.
// smartctl calls anything it cannot identify "Unknown_Attribute", and on a drive it does not
// recognise that is a dozen rows. Shown verbatim it reads as the app having failed to read
// them, so the row is labelled as vendor specific and the table says why.
test('an attribute smartctl cannot name is labelled vendor specific', () => {
  const table = attributeTable([
    attr(5, 'Reallocated_Sector_Ct', 100, 100, 10),
    attr(169, 'Unknown_Attribute', 100, 100, 50),
    attr(170, '', 100, 100, 50),
  ]);
  const text = textOf(table);
  assert.doesNotMatch(text, /Unknown_Attribute/, 'the placeholder must not reach the table');
  assert.equal((text.match(/Vendor specific/g) || []).length, 2, 'both unnamed rows');
  assert.match(text, /2 of these attributes are vendor specific/);
  // The drive's own name still wins where there is one.
  assert.match(text, /Reallocated_Sector_Ct/);
});

test('a fully named table carries no vendor-specific note', () => {
  const text = textOf(attributeTable([attr(5, 'Reallocated_Sector_Ct', 100, 100, 10)]));
  assert.doesNotMatch(text, /vendor specific/);
});

// The panel used to say the entries "are in the raw output" without saying where, which is not
// findable in several hundred lines of JSON. It names the sections now.
test('the log panel names the raw-output sections to look in', () => {
  const text = textOf(logSummary({ error_count: 1, self_test_count: 2 }));
  assert.match(text, /ata_smart_error_log/);
  assert.match(text, /ata_smart_self_test_log/);
  assert.match(text, /nvme_error_information_log/);
  assert.doesNotMatch(text, /Log entries themselves are in the raw output/);
});

test('the DOM stub stringifies non-node arguments, as the browser does', () => {
  const node = new StubNode('div');
  node.append('a', null, undefined, 7);
  assert.equal(textOf(node), 'a null undefined 7');
});

// Every help dialog is opened and checked for the literal word "null".
//
// The bug this guards against was not a wrong value but a wrong *absence*: an entry with no
// `watch` line passed a null to the native `Element.append`, which stringifies whatever it is
// given, so the dialog read "…replace worn-out blocks. null" underneath the explanation. It
// reached a real deployment because the DOM stub dropped null arguments silently — so this
// test only works because the stub was fixed to behave like the real thing first.
test('no attribute help dialog renders null or undefined', () => {
  const ids = Object.keys(ATTRIBUTE_HELP);
  assert.ok(ids.length > 20, 'expected a curated table, not a handful of entries');

  for (const id of ids) {
    const table = attributeTable([attr(Number(id), 'Probe', 100, 100, 0)]);
    const button = helpButtons(table)[0];
    assert.ok(button, `attribute ${id} has help text but no ? button`);
    button.click();

    const dialogs = document.body.all('dialog');
    const text = textOf(dialogs[dialogs.length - 1]);
    assert.doesNotMatch(text, /null/, `attribute ${id} dialog: ${text}`);
    assert.doesNotMatch(text, /undefined/, `attribute ${id} dialog: ${text}`);
  }
});

test('no NVMe help dialog renders null or undefined', () => {
  const buttons = helpButtons(nvmeTable(NVME_HEALTH));
  assert.equal(buttons.length, 16, 'expected one ? per NVMe counter');

  for (const button of buttons) {
    button.click();
    const dialogs = document.body.all('dialog');
    const text = textOf(dialogs[dialogs.length - 1]);
    assert.doesNotMatch(text, /null|undefined/, `NVMe dialog: ${text}`);
  }
});


