// When a device page re-reads the drive it is showing.
//
// The rule: an *absent* setting means "not written yet" and gets the documented default; an explicit
// zero, a negative, or nonsense means "never". Reading absence as "never" turned the feature off on
// every database created before the setting existed — and the settings editor still showed its own
// default of 15 beside a switch that did nothing.

import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom } from './stub-dom.mjs';

installStubDom();

const { setDisplayOptions, deviceRefreshWindow } = await import('../assets/js/views/device-parts.js');

test('an absent setting means the default, not "never"', () => {
  setDisplayOptions({});
  assert.equal(deviceRefreshWindow(), 15);
});

test('a stored value wins', () => {
  setDisplayOptions({ device_refresh_minutes: '30' });
  assert.equal(deviceRefreshWindow(), 30);
});

test('an explicit zero still means "never"', () => {
  setDisplayOptions({ device_refresh_minutes: '0' });
  assert.equal(deviceRefreshWindow(), 0);
});

test('a negative or unparseable value means "never"', () => {
  for (const value of ['-5', 'twelve', '15abc']) {
    setDisplayOptions({ device_refresh_minutes: value });
    assert.equal(deviceRefreshWindow(), 0, `value ${JSON.stringify(value)} should mean never`);
  }
});

// A cleared field is "unset", not "disable": someone who empties the number should get the default
// back, not a feature that quietly stops working.
test('an empty or whitespace-only value falls back to the default', () => {
  for (const value of ['', '   ']) {
    setDisplayOptions({ device_refresh_minutes: value });
    assert.equal(deviceRefreshWindow(), 15, `value ${JSON.stringify(value)} should use the default`);
  }
});

test('no settings at all falls back to the default', () => {
  setDisplayOptions(null);
  assert.equal(deviceRefreshWindow(), 15);
});
