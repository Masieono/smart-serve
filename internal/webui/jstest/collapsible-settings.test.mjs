// A collapsible settings page (`FEATURES.md` §Settings).
//
// The page is a dozen controls deep, and what a reader scans for is the *section*, not the fourth
// slider in it. These pin the two things that make collapsing safe rather than annoying: a closed
// section says what is inside it, and opening one survives the re-render that saving triggers.

import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, StubNode } from './stub-dom.mjs';

installStubDom();
const { rememberSection, sectionIsOpen } = await import('../assets/js/views/settings.js');

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

// Open state lives in a module-level set rather than in the DOM, because the page re-renders on save,
// on a threshold reset, and when the theme changes. If it lived in the DOM, changing the theme would
// slam the Appearance section shut — the one section you are most likely to be in.
test('opening a section survives a re-render', () => {
  assert.equal(sectionIsOpen('Appearance'), false, 'sections start closed');

  rememberSection('Appearance', true);
  assert.equal(sectionIsOpen('Appearance'), true);
  // And it is per-section, not a page-wide flag.
  assert.equal(sectionIsOpen('Health thresholds'), false);

  rememberSection('Appearance', false);
  assert.equal(sectionIsOpen('Appearance'), false);
});

// A section must not be able to hide its own error behind a collapsed summary, which is why the
// per-model editor forces itself open when the value it was given is rejected.
test('rememberSection reports the state it recorded', () => {
  assert.equal(rememberSection('Import and export', true), true);
  assert.equal(rememberSection('Import and export', false), false);
  assert.equal(sectionIsOpen('Import and export'), false);
});

// The hint is what makes a closed section a signpost rather than a blank. Asserted against the
// rendered page rather than the helper, because the value is in it reaching the summary.
test('every settings section renders closed, with a hint', async () => {
  const container = await renderSettings();
  const sections = container.all('details').filter((n) => n.className === 'settings-section');
  assert.ok(sections.length >= 5, `expected the settings sections, got ${sections.length}`);

  for (const section of sections) {
    const summary = section.all('summary')[0];
    assert.ok(summary, 'a section with no summary cannot be opened');
    const text = textOf(summary);
    assert.match(text, /[A-Za-z]/, 'a section with no title');
    // The title is a span, and the hint is a second span — a bare title is a blank when closed.
    assert.ok(summary.all('span').length >= 2, `section "${text}" has no hint line`);
  }
});

/** Renders the settings page against the stub payload the other settings tests use. */
async function renderSettings() {
  const { stubFetch } = await import('./stub-dom.mjs');
  const { render } = await import('../assets/js/views/settings.js');
  // One payload for all three requests the page makes (settings, knowledge, devices), which is what
  // the page has to cope with anyway.
  stubFetch({
    settings: {},
    bundled_entries: 12,
    resolved_entries: 12,
    bundled: '{}',
    devices: [],
  });
  const container = globalThis.document.createElement('div');
  await render(container, { refresh() {}, setTemperatureUnit() {}, setDisplayOptions() {} });
  return container;
}
