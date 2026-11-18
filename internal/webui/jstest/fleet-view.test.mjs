// The fleet table (v2, `ADR-032`).
//
// The rules that matter are about what a row *says*: a verdict and its score share one cell, a drive
// that cannot report gives one line of its own reason, and what needs looking at comes first. Those
// are decisions about meaning, so they are worth pinning where a markup change would not be.

import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, StubNode } from './stub-dom.mjs';

installStubDom();

const { render, reset } = await import('../assets/js/views/fleet.js');

function payload(devices) {
  return { devices, ranking: [], last_scan_ts: '2026-11-14T20:43:49Z', bench_tag: 'bench' };
}

function device(overrides = {}) {
  return {
    serial: 'S1',
    model: 'Samsung SSD 870 EVO',
    device_path: '/dev/sda',
    transport: 'sata',
    capacity_bytes: 500107862016,
    health: 'good',
    score: 100,
    temperature_c: 33,
    power_on_hours: 1234,
    mountpoints: ['/'],
    last_scan: '2026-11-14T20:43:49Z',
    smart_available: true,
    ...overrides,
  };
}

/** Every string in a subtree, in document order. The stub does not aggregate textContent. */
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

function withFetch(body) {
  globalThis.fetch = async () => ({
    ok: true,
    status: 200,
    statusText: 'OK',
    text: async () => JSON.stringify(body),
  });
}

const ctx = { navigate() {} };

test('a row carries the verdict and the score, not a column each', async () => {
  reset();
  withFetch(payload([device({ health: 'caution', score: 62 })]));

  const host = new StubNode('main');
  await render(host, ctx);

  const text = textOf(host);
  assert.match(text, /Caution/);
  assert.match(text, /62/);
});

test('a drive that cannot report gives one line of its own reason', async () => {
  reset();
  withFetch(payload([device({
    health: 'unknown',
    score: -1,
    smart_available: false,
    health_rationale: 'error: /dev/sdb: Unknown USB bridge [0x154b:0x1006 (0x110)]',
  })]));

  const host = new StubNode('main');
  await render(host, ctx);

  const text = textOf(host);
  assert.match(text, /No reading/);
  assert.doesNotMatch(text, /error:/, 'the "error: " noise is dropped');
  assert.match(text, /Unknown USB bridge/, "the drive's own words are kept");
});

test('what needs looking at is listed first', async () => {
  reset();
  withFetch(payload([
    device({ serial: 'A', health: 'good', score: 100 }),
    device({ serial: 'B', health: 'bad', score: 12 }),
    device({ serial: 'C', health: 'caution', score: 55 }),
  ]));

  const host = new StubNode('main');
  await render(host, ctx);

  const text = textOf(host);
  const bad = text.indexOf('Failing');
  const caution = text.indexOf('Caution');
  const good = text.lastIndexOf('Healthy');
  assert.ok(bad >= 0 && caution >= 0 && good >= 0, 'all three verdicts should be on screen');
  assert.ok(bad < caution, 'a failing drive is listed before a caution');
  assert.ok(caution < good, 'a caution is listed before a healthy drive');
});
