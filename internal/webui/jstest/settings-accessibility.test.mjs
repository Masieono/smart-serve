// Every control on a settings page answers to the words beside it.
//
// The rows are built as label text in a `div` next to the control, so a screen reader announced
// "checkbox" or "popup button" and nothing else: twelve controls across the four pages, including
// both switches in "What the pages show". Nothing in the suite could see it, because every test
// asserted what the rows *say*, never what the control is *called*.
//
// The rule this file enforces: a control with no accessible name is a control some people cannot use.

import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, stubFetch } from './stub-dom.mjs';

installStubDom();
const settings = await import('../assets/js/views/settings.js');

const ctx = {
  setTemperatureUnit() {},
  setDisplayOptions() {},
  setNavGuard() {},
  refresh() {},
};

// GET /api/settings and GET /api/knowledge are both stubbed with this, which is what the page has to
// cope with anyway: one payload, two readers.
const PAYLOAD = {
  url: 'https://example.test/knowledge.json',
  bundled_entries: 12,
  resolved_entries: 12,
  cache: null,
  overrides: { entries: 0, payload: '' },
  bundled: '{"version":1,"drives":[]}',
};

/** Every control on the page that no assistive technology can name. */
function unnamedControls(container) {
  const out = [];
  for (const tag of ['input', 'select', 'textarea']) {
    for (const control of container.querySelectorAll(tag)) {
      if (control.type === 'hidden') continue;
      if (control.getAttribute('aria-label')) continue;
      if (control.labels && control.labels.length) continue;
      out.push(`${tag}${control.type ? `[${control.type}]` : ''}${control.name ? ` ${control.name}` : ''}`);
    }
  }
  return out;
}

test('every control on every settings page has an accessible name', async () => {
  const problems = [];
  for (const page of ['thresholds', 'notifications', 'appearance', 'data']) {
    installStubDom();
    stubFetch({ settings: {}, ...PAYLOAD });
    const container = document.createElement('div');
    await settings.render(container, ctx, page);
    for (const bad of unnamedControls(container)) problems.push(`${page}: ${bad}`);
  }
  assert.deepEqual(problems, [], 'these controls have no accessible name');
});

// The theme picker is the one control built outside a setting row, because its value is read before
// the row exists. It needs its name set where it is made, and this is the assertion that says so.
test('the theme picker is named, even though its row cannot name it', async () => {
  installStubDom();
  stubFetch({ settings: {}, ...PAYLOAD });
  const container = document.createElement('div');
  await settings.render(container, ctx, 'appearance');

  const named = container.querySelectorAll('select')
    .filter((s) => s.getAttribute('aria-label') === 'Theme');
  assert.equal(named.length, 1, 'the theme select should answer to "Theme"');
});

// A check that has never failed is indistinguishable from one that cannot fail.
test('the check catches an unnamed control', () => {
  installStubDom();
  const container = document.createElement('div');
  const loose = document.createElement('input');
  loose.type = 'checkbox';
  container.append(loose);

  assert.deepEqual(unnamedControls(container), ['input[checkbox]']);
});
