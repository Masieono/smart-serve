// Settings as four pages (`FEATURES.md` §Settings).
//
// The sections used to be collapsible headings on one long screen, which made the wall shorter to
// look at without making it shorter to use. They are pages now, reached from the sidebar, and what
// this pins is the part that makes that safe: each page draws only its own controls, saves only its
// own fields, and asks before it is left with unsaved edits.

import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, stubFetch, StubNode } from './stub-dom.mjs';

installStubDom();
const settings = await import('../assets/js/views/settings.js');

/** A view context that records the navigation guard the page registers. */
function stubCtx() {
  return {
    guard: null,
    setNavGuard(fn) { this.guard = fn; },
    setTemperatureUnit() {},
    setDisplayOptions() {},
    refresh() {},
  };
}

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

const PAYLOAD = {
  settings: {},
  devices: [],
  bundled_entries: 12,
  resolved_entries: 12,
  bundled: '{}',
};

async function renderPage(id) {
  stubFetch(PAYLOAD);
  const ctx = stubCtx();
  const container = globalThis.document.createElement('div');
  await settings.render(container, ctx, id);
  return { container, ctx };
}

const flush = () => new Promise((resolve) => setImmediate(resolve));

/** A regex that matches the literal text, so a label with punctuation in it still works. */
function literal(text) {
  return new RegExp(text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'));
}

// --- which page ----------------------------------------------------------------

// `/settings` is a section, not a page, so it renders the first one. A bookmark to it must land
// somewhere rather than on nothing.
test('an unknown or missing page id renders the first page', () => {
  assert.equal(settings.settingsPage('').id, 'thresholds');
  assert.equal(settings.settingsPage(undefined).id, 'thresholds');
  assert.equal(settings.settingsPage('nonsense').id, 'thresholds');
  // And a real one is itself.
  assert.equal(settings.settingsPage('appearance').id, 'appearance');
});

test('the page list is the sidebar order', () => {
  // app.js duplicates these ids, because the shell cannot import a view's list without importing the
  // view. This is the assertion that the duplication is still true.
  assert.deepEqual(settings.settingsPageIds(),
    ['thresholds', 'notifications', 'appearance', 'data']);
});

// --- what a page draws ---------------------------------------------------------

// Each page draws its own controls and none of the others'. A page that leaked another's rows would
// have the same problem the single long screen had, one page at a time.
test('each page draws only its own rows', async () => {
  const expected = {
    thresholds: ['Temperature caution threshold', 'Per-model thresholds'],
    notifications: ['Alert squelch window', 'Public URL'],
    appearance: ['Temperature unit', 'Theme', 'Partitions on the drive page'],
    data: ['Import and export', 'Prometheus metrics', 'Knowledge base'],
  };
  const leaked = {
    thresholds: ['Theme', 'Alert squelch window'],
    notifications: ['Temperature caution threshold', 'Theme'],
    appearance: ['Alert squelch window', 'Per-model thresholds'],
    data: ['Temperature caution threshold', 'Theme'],
  };

  for (const id of settings.settingsPageIds()) {
    const { container } = await renderPage(id);
    const text = textOf(container);
    for (const want of expected[id]) {
      assert.match(text, literal(want), `page ${id} is missing "${want}"`);
    }
    for (const other of leaked[id]) {
      assert.doesNotMatch(text, literal(other), `page ${id} leaked "${other}"`);
    }
  }
});

// The page title is the section name, so the sidebar entry and the page agree about where you are.
test('the page heading names the page', async () => {
  for (const id of settings.settingsPageIds()) {
    const { container } = await renderPage(id);
    const heading = container.querySelectorAll('h1')[0];
    assert.ok(heading, `page ${id} has no heading`);
    assert.equal(heading.textContent, settings.settingsPage(id).label);
  }
});

// --- the unsaved-changes guard -------------------------------------------------

// Nothing to lose, nothing to ask about. A guard that prompted on every navigation would be worse
// than no guard at all, because people would learn to dismiss it.
test('an untouched page is left without a question', async () => {
  const { ctx } = await renderPage('thresholds');
  assert.equal(typeof ctx.guard, 'function', 'the page registered no guard');
  assert.equal(await ctx.guard(), true, 'an untouched page refused to be left');
});

// An edit that has not been saved must be asked about — and answered, either way.
test('an edited page asks before it is left', async () => {
  const { container, ctx } = await renderPage('thresholds');
  const slider = container.querySelectorAll('input').find((n) => n.className === 'setting-range');
  slider.value = String(Number(slider.getAttribute('min')) + 1);
  slider.dispatch('input');

  // Cancel: the navigation is refused.
  const refused = ctx.guard();
  await flush();
  const [cancel] = globalThis.document.body.querySelectorAll('button').slice(-2);
  cancel.click();
  assert.equal(await refused, false, 'cancelling the prompt allowed the navigation anyway');

  // Confirm: it goes through.
  const allowed = ctx.guard();
  await flush();
  const [, confirm] = globalThis.document.body.querySelectorAll('button').slice(-2);
  confirm.click();
  assert.equal(await allowed, true, 'confirming the prompt blocked the navigation');
});

// And once saved there is nothing to ask about, so the guard goes quiet — otherwise every navigation
// after a save would prompt about changes that no longer exist.
test('saving clears the question', async () => {
  const { container, ctx } = await renderPage('thresholds');
  const slider = container.querySelectorAll('input').find((n) => n.className === 'setting-range');
  slider.value = String(Number(slider.getAttribute('min')) + 1);
  slider.dispatch('input');

  const save = container.querySelectorAll('button').find((b) => b.textContent === 'Save');
  assert.equal(save.disabled, false, 'the edit did not enable Save');
  save.click();
  await flush();

  assert.equal(await ctx.guard(), true, 'a saved page still asks about unsaved changes');
});
