// Self-test triggering (FEATURES.md A19).
//
// The only part of the app that changes a drive, and the only part that polls one. The things
// worth pinning are the ones that make it safe to have either: a test cannot start without a
// confirmation, an abort is always available while one runs, a drive that cannot self-test is
// told so rather than offered a button that would fail, and the poll stops when the card leaves
// the page.

import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, stubFetch, StubNode } from './stub-dom.mjs';

installStubDom();

const { selfTestCard, stopPolling, POLL_MS } = await import('../assets/js/views/selftest.js');

/** A stand-in for the view context, recording what the card asked of it. */
function stubCtx() {
  return { rescanned: [], scanDevice(serial) { this.rescanned.push(serial); } };
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

/** The buttons the card is currently offering. */
function buttonsOf(node) {
  return node.all('button');
}

/** Press a button and let its async handler settle. */
async function press(button) {
  await button.onclick({ target: button, currentTarget: button });
  // Handlers await confirmDialog, which settles on the dialog's `close` event, then await the
  // request. Two turns is enough for both without depending on a timer.
  await new Promise((resolve) => setImmediate(resolve));
}

// The dialog is built by components.js and appended to document.body; its buttons are the last
// two in the document. Pressing one closes it, which settles the promise.
function dialogButtons() {
  const dialogs = globalThis.document.body.all('button');
  return dialogs.slice(-2);
}

async function confirmLastDialog() {
  const [, confirm] = dialogButtons();
  confirm.click();
  await new Promise((resolve) => setImmediate(resolve));
}

const flush = () => new Promise((resolve) => setImmediate(resolve));

/** Press a button, confirm the dialog it opens, and let the handler finish. */
async function clickThrough(button) {
  const done = button.onclick({ target: button, currentTarget: button });
  await flush();
  await confirmLastDialog();
  await done;
  await flush();
}

/**
 * Serve the self-test API, recording what was asked.
 *
 * Routes by path rather than answering everything the same way, because one action here makes two
 * calls — the POST and the read that follows it — and they must not be confusable. A test that
 * asserted on a single shared payload could not tell the two apart.
 */
function stubSelfTestApi({ running = null } = {}) {
  const requests = [];
  globalThis.fetch = async (url, init = {}) => {
    const path = String(url);
    const body = init.body ? JSON.parse(init.body) : null;
    const method = init.method || 'GET';
    requests.push({ path, method, body });

    let payload;
    if (path.endsWith('/selftest/abort')) {
      payload = { status: 'aborted', note: 'Aborted' };
    } else if (method === 'POST') {
      payload = { status: 'started', kind: body && body.kind, note: 'Please wait 2 minutes' };
    } else {
      payload = { running };
    }
    return { ok: true, status: 200, statusText: 'OK', text: async () => JSON.stringify(payload) };
  };
  return requests;
}

/** Run a body with `setTimeout` captured rather than scheduled, so a poll can be fired by hand. */
async function withFakeTimers(body) {
  const realSet = globalThis.setTimeout;
  const realClear = globalThis.clearTimeout;
  const pending = new Set();
  globalThis.setTimeout = (fn, ms) => {
    const id = { fn, ms };
    pending.add(id);
    return id;
  };
  globalThis.clearTimeout = (id) => pending.delete(id);
  try {
    await body(pending);
  } finally {
    globalThis.setTimeout = realSet;
    globalThis.clearTimeout = realClear;
  }
}

// A drive whose self-test log cannot be read gets an explanation rather than a button that would
// fail the moment it was pressed. This is the NVMe case on the real hardware.
test('a drive that cannot self-test is told so, not offered a button', () => {
  stopPolling();
  const card = selfTestCard(stubCtx(), { serial: 'S' }, {
    self_test_supported: false,
    note: 'Read Self-test Log failed: Invalid Field in Command (0x4002)',
  });
  assert.equal(buttonsOf(card).length, 0, 'no button that could only fail');
  assert.match(textOf(card), /cannot run self-tests/);
  assert.match(textOf(card), /Invalid Field in Command/);
});

// Both tests are offered, and the drive's own estimates are quoted: a figure from this app would be
// worse than none, because knowing how long the drive will be busy is the only reason to show one.
test('a drive that can self-test offers both, with its own estimates', () => {
  stopPolling();
  const card = selfTestCard(stubCtx(), { serial: 'S' }, {
    short_minutes: 2,
    extended_minutes: 133,
  });
  const labels = buttonsOf(card).map((b) => textOf(b));
  assert.equal(labels.length, 2);
  assert.match(labels[0], /Short test/);
  assert.match(labels[1], /Long test/);
  const text = textOf(card);
  assert.match(text, /short test about 2 minutes/);
  assert.match(text, /long test about 2 h 13 min/);
  assert.match(text, /Nothing runs on its own/);
});

test('a drive that gives no estimates still offers the tests', () => {
  stopPolling();
  const card = selfTestCard(stubCtx(), { serial: 'S' }, {});
  assert.equal(buttonsOf(card).length, 2);
  assert.match(textOf(card), /does not say how long/);
});

// A test that was already running when the drive was last scanned is shown straight away, with the
// drive's own percentage — otherwise reloading the page mid-test would look like nothing happening.
test('a test already running is shown with its percentage', () => {
  const card = selfTestCard(stubCtx(), { serial: 'S' }, {
    self_test_entries: [
      { type: 'Extended offline', status: 'Self-test routine in progress', remaining: 47 },
    ],
  });
  stopPolling();

  const text = textOf(card);
  assert.match(text, /Extended offline/);
  assert.match(text, /47% left/);

  const labels = buttonsOf(card).map((b) => textOf(b));
  assert.equal(labels.length, 1, 'the only action while a test runs is stopping it');
  assert.match(labels[0], /Stop test/);

  const fill = card.find((n) => n.className === 'meter-fill');
  assert.ok(fill, 'a bar is drawn when the drive gave a figure');
  assert.match(String(fill.style), /width:53%/);
});

// The stored log is read the way the server reads it: an entry counts as running only when the
// drive reported a percentage remaining. An entry without one is a finished test — note that a
// running test still reports `passed: true`, so `passed` could not be the signal either way.
test('a stored entry with no percentage is not treated as running', () => {
  stopPolling();
  const card = selfTestCard(stubCtx(), { serial: 'S' }, {
    self_test_entries: [
      { type: 'Short offline', status: 'Completed without error', passed: true, hours: 2116 },
    ],
  });
  assert.equal(buttonsOf(card).length, 2);
  assert.doesNotMatch(textOf(card), /% left/);
});

// Starting a test changes a drive, so it goes through a confirmation that says what will happen and
// how long it will take.
test('starting a short test asks first, then posts the type', async () => {
  stopPolling();
  const requests = stubSelfTestApi();
  const card = selfTestCard(stubCtx(), { serial: 'S' }, { short_minutes: 2 });

  await clickThrough(buttonsOf(card)[0]);

  const posts = requests.filter((r) => r.method === 'POST');
  assert.equal(posts.length, 1);
  assert.match(posts[0].path, /\/api\/devices\/S\/selftest$/);
  assert.deepEqual(posts[0].body, { kind: 'short' });
  // The card is redrawn from the drive's answer, not from what was asked of it.
  assert.ok(requests.some((r) => r.method === 'GET'), 'the drive is read back afterwards');
  stopPolling();
});

test('starting a long test posts the long type', async () => {
  stopPolling();
  const requests = stubSelfTestApi();
  const card = selfTestCard(stubCtx(), { serial: 'S' }, { extended_minutes: 133 });

  await clickThrough(buttonsOf(card)[1]);

  assert.deepEqual(requests.find((r) => r.method === 'POST').body, { kind: 'long' });
  stopPolling();
});

// Declining the confirmation must send nothing at all. A drive is not something to change by
// accident.
test('declining the confirmation sends nothing', async () => {
  stopPolling();
  const requests = stubSelfTestApi();
  const card = selfTestCard(stubCtx(), { serial: 'S' }, { short_minutes: 2 });

  const run = buttonsOf(card)[0];
  const done = run.onclick({ target: run, currentTarget: run });
  await flush();
  const [cancel] = dialogButtons();
  cancel.click();
  await done;
  await flush();

  assert.deepEqual(requests, []);
  assert.equal(run.disabled, false, 'the button stays usable after a cancelled confirmation');
  stopPolling();
});

// The abort exists because a long test runs for hours. It asks first, because stopping one cannot
// be undone.
test('stopping a running test asks first, then posts the abort', async () => {
  stopPolling();
  const requests = stubSelfTestApi();
  const card = selfTestCard(stubCtx(), { serial: 'S' }, {
    self_test_entries: [{ type: 'Extended offline', remaining: 47 }],
  });

  await clickThrough(buttonsOf(card)[0]);

  const posts = requests.filter((r) => r.method === 'POST');
  assert.equal(posts.length, 1);
  assert.match(posts[0].path, /\/api\/devices\/S\/selftest\/abort$/);
  stopPolling();
});

// The poll is what keeps a long test watchable, so it must not be able to outlive the card: one
// timer at a time, cleared whenever the card redraws.
test('the poll is cleared, never left running', async () => {
  await withFakeTimers(async (pending) => {
    selfTestCard(stubCtx(), { serial: 'S' }, {
      self_test_entries: [{ type: 'Short offline', remaining: 50 }],
    });
    assert.equal(pending.size, 1, 'a running test schedules exactly one poll');
    assert.equal([...pending][0].ms, POLL_MS);

    stopPolling();
    assert.equal(pending.size, 0, 'stopPolling clears it');
  });
});

// When a test ends, the log table above the card is still drawn from the last scan, so the drive is
// rescanned once to record the outcome — and only once. The re-rendered card is drawn from stored
// logs that still show the test running, so an unguarded rescan would poll, rescan, poll, forever.
test('a finished test is recorded once, not on every poll', async () => {
  await withFakeTimers(async (pending) => {
    const ctx = stubCtx();
    stubSelfTestApi({ running: null });
    const card = selfTestCard(ctx, { serial: 'S' }, {
      self_test_entries: [{ type: 'Short offline', remaining: 50 }],
    });

    const tick = [...pending][0].fn;
    await tick();
    assert.deepEqual(ctx.rescanned, ['S'], 'the outcome is recorded');

    await tick();
    await tick();
    assert.deepEqual(ctx.rescanned, ['S'], 'and not recorded again on later polls');
    assert.equal(card.isConnected, true);
  });
});

// A card that has left the page must stop reading the drive. That is the whole reason the poll is
// bounded: nobody should be able to navigate away and leave a drive read every fifteen seconds.
test('a card that has left the page stops polling', async () => {
  await withFakeTimers(async (pending) => {
    const requests = stubSelfTestApi({ running: { type: 'Extended offline', remaining: 40 } });
    const holder = new StubNode('div');
    const card = selfTestCard(stubCtx(), { serial: 'S' }, {
      self_test_entries: [{ type: 'Short offline', remaining: 50 }],
    });
    holder.append(card);
    assert.equal(card.isConnected, true);

    const tick = [...pending][0].fn;
    holder.removeChild(card);
    assert.equal(card.isConnected, false);

    await tick();
    assert.deepEqual(requests, [], 'a detached card reads nothing at all');
  });
});
