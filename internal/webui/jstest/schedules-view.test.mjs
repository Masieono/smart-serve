// The schedules page, rendered.
//
// It had no view test at all, which is how two things reached a reader: a switch the action column
// could cover at narrow widths, and an untitled schedule that was "Schedule 5" in its row and "#5" in
// the confirm that deletes it. Neither is visible to a test of the row builder alone.

import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, StubNode } from './stub-dom.mjs';

installStubDom();
const schedules = await import('../assets/js/views/schedules.js');

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
  return out.join(' | ');
}

const SCHEDULES = {
  schedules: [
    { id: 5, label: '', action: 'scan', enabled: true, interval_minutes: 1440, next_run: null, last_run: null },
    { id: 6, label: 'nightly', action: 'digest', enabled: true, interval_minutes: 1440, next_run: null, last_run: null },
  ],
  selftest_pass: null,
};

function stub() {
  globalThis.fetch = async (url) => ({
    ok: true, status: 200, statusText: 'OK',
    text: async () => JSON.stringify(String(url).includes('/api/schedules') ? SCHEDULES : {}),
  });
}

async function renderPage() {
  installStubDom();
  stub();
  const container = document.createElement('div');
  await schedules.render(container, { refresh() {}, navigate() {} });
  return container;
}

// An untitled schedule is named for the reader, and the name is the same wherever it is used. The
// delete confirm is the worst possible place to introduce a second name: it is the irreversible one.
test('an untitled schedule has one name, in the row and in the confirm', async () => {
  const container = await renderPage();
  assert.match(textOf(container), /Schedule 5/, 'the row should name the untitled schedule');

  const rows = container.querySelectorAll('tr');
  const row = rows.find((r) => /Schedule 5/.test(textOf(r)));
  assert.ok(row, 'the row for schedule 5 should be on the page');

  const del = row.querySelectorAll('button').find((b) => b.textContent === 'Delete');
  assert.ok(del, 'every row offers Delete');
  del.click();

  const dialog = document.body.firstElementChild;
  assert.ok(dialog, 'Delete should ask first');
  assert.match(textOf(dialog), /Schedule 5/,
    'the confirm has to use the name the row used');
  assert.doesNotMatch(textOf(dialog), /#5/,
    'a second name for the same row is how "Schedule 5" became "#5"');
});

// A named schedule is named by its label, in both places.
test('a named schedule is confirmed by its label', async () => {
  const container = await renderPage();
  const row = container.querySelectorAll('tr').find((r) => /nightly/.test(textOf(r)));
  assert.ok(row);

  row.querySelectorAll('button').find((b) => b.textContent === 'Delete').click();
  const dialog = document.body.firstElementChild;
  assert.match(textOf(dialog), /"nightly"/);
});

// The switch is a first-class control, not a decoration in the corner: it carries the schedule's name
// so a screen reader can tell six of them apart.
test('each switch says which schedule it turns on', async () => {
  const container = await renderPage();
  const labels = container.querySelectorAll('input')
    .filter((i) => i.getAttribute('type') === 'checkbox')
    .map((i) => i.getAttribute('aria-label'));

  assert.deepEqual(labels, ['Enabled: Schedule 5', 'Enabled: nightly']);
});
