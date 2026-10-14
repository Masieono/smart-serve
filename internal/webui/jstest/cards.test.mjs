// Device card status.
//
// The card is where a drive is first described, so the label it carries has to match
// the drive's actual situation. Three of them look similar in the data and mean
// completely different things to the person reading the screen.

import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, StubNode } from './stub-dom.mjs';

installStubDom();

const { deviceCard } = await import('../assets/js/components.js');

/**
 * Every string in a subtree, in document order. The stub's textContent is not
 * aggregated from children, so a test that wants the visible text has to walk.
 */
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

/** A device payload carrying the fields the card reads. */
function device(overrides = {}) {
  return {
    serial: 'S',
    model: 'Model',
    device_path: '/dev/sda',
    transport: 'nvme',
    health: 'good',
    score: 100,
    health_rationale: 'all checks passed',
    smart_available: true,
    mountpoints: [],
    ...overrides,
  };
}

const card = (overrides) => deviceCard(device(overrides), () => {}, () => {});

test('a drive with a verdict shows that verdict', () => {
  assert.match(textOf(card({})), /Good/);
});

test('a drive that is gone is "Not connected", not a verdict', () => {
  const text = textOf(card({ is_missing: true }));
  assert.match(text, /Not connected/);
  assert.doesNotMatch(text, /Good/);
});

// "Unknown" would read as "we have not worked it out yet". A drive that cannot report
// S.M.A.R.T. at all is a different situation and has to say so.
test('a drive that cannot report S.M.A.R.T. is labelled as such', () => {
  const text = textOf(card({
    smart_available: false,
    health: 'unknown',
    score: -1,
    health_rationale: 'no S.M.A.R.T. data reported',
  }));
  assert.match(text, /No S\.M\.A\.R\.T\./);
  assert.doesNotMatch(text, /Unknown/);
});

// A blocked read is not the drive's fault. Labelling it "No S.M.A.R.T." sends the reader
// looking at the hardware instead of at the compose file.
test('a blocked read is labelled differently from a drive with no S.M.A.R.T.', () => {
  const blocked = textOf(card({
    smart_available: false, health: 'unknown', score: -1, smart_cause: 'device_blocked',
    smart_error: 'Smartctl open device: /dev/sda failed: Operation not permitted',
  }));
  assert.match(blocked, /Blocked/);
  assert.doesNotMatch(blocked, /No S\.M\.A\.R\.T\./);

  const unsupported = textOf(card({
    smart_available: false, health: 'unknown', score: -1, smart_cause: 'unsupported',
    smart_error: 'SMART support is: Unavailable',
  }));
  assert.match(unsupported, /No S\.M\.A\.R\.T\./);
  assert.doesNotMatch(unsupported, /Blocked/);
});

// A drive whose bad-sector count is moving is worth replacing before it stops moving at
// all, so the dashboard flags it — separately from the verdict, which only describes today.
test('a drive with growing bad sectors is flagged as worsening', () => {
  const worsening = textOf(card({
    smart_available: true, health: 'good', score: 98, worsening: 5,
  }));
  assert.match(worsening, /Worsening/);

  // Green and stable must stay green: the flag is about direction, not about the verdict.
  const stable = textOf(card({ smart_available: true, health: 'good', score: 98 }));
  assert.doesNotMatch(stable, /Worsening/);
});

// There is no score to draw, so an empty ring would only read as broken.
test('a drive with no S.M.A.R.T. has no health gauge', () => {
  const isGauge = (n) => n.className === 'gauge';
  assert.ok(card({}).find(isGauge), 'a scored drive keeps its gauge');
  assert.equal(card({ smart_available: false, score: -1 }).find(isGauge), null);
});