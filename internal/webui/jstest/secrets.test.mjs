// Per-channel environment overrides (`ADR-022`).
//
// Two things are worth pinning. The first is that the naming rule here matches the one the
// server resolves against — the rule is duplicated because the variable name has to appear
// while the label is being typed, before anything has been saved, and a mismatch would mean
// the editor showed a name that does nothing. The second is that a supplied field stops being
// demanded: a token that lives in a Docker secret is never typed into the form, so requiring
// it would make the override impossible to use.

import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom } from './stub-dom.mjs';

installStubDom();

const {
  channelSlug, channelEnvPrefix, channelEnvVars, suppliedFields, missingRequired,
} = await import('../assets/js/views/notifications.js');

// The same table as TestSlugMakesAStableVariableName in internal/notify/secrets_test.go. If
// one side changes, the other fails — which is the only thing keeping two implementations of
// one rule honest.
test('the slug rule matches the server', () => {
  const cases = {
    'Critical alerts': 'CRITICAL_ALERTS',
    'critical-alerts': 'CRITICAL_ALERTS',
    'critical_alerts': 'CRITICAL_ALERTS',
    '  spaced  out  ': 'SPACED_OUT',
    'Pool #1 (media)': 'POOL_1_MEDIA',
    AlreadyCAPS: 'ALREADYCAPS',
    '': '',
    '!!!': '',
  };
  for (const [input, want] of Object.entries(cases)) {
    assert.equal(channelSlug(input), want, `slug(${JSON.stringify(input)})`);
  }
});

test('the variable names one channel and one field', () => {
  assert.equal(channelEnvPrefix('Critical alerts'), 'SMARTSERVE_CHANNEL_CRITICAL_ALERTS');
  assert.deepEqual(channelEnvVars('Critical alerts', 'token'), [
    'SMARTSERVE_CHANNEL_CRITICAL_ALERTS_TOKEN',
    'SMARTSERVE_CHANNEL_CRITICAL_ALERTS_TOKEN_FILE',
  ]);
  // Two channels of one type must not collide — the whole reason the key is the label.
  assert.notDeepEqual(channelEnvVars('Critical alerts', 'token'), channelEnvVars('Weekly digest', 'token'));
  // A label with nothing usable in it names no variables at all, rather than a bare prefix
  // that would match every channel.
  assert.deepEqual(channelEnvVars('!!!', 'token'), []);
});

const NTFY = {
  type: 'ntfy',
  fields: [
    { key: 'server' },
    { key: 'topic', required: true },
    { key: 'token', secret: true },
  ],
};

test('a field counts as supplied only when its exact variable is set', () => {
  const supplied = suppliedFields('Critical alerts', NTFY, [
    'SMARTSERVE_CHANNEL_CRITICAL_ALERTS_TOKEN',
    'SMARTSERVE_CHANNEL_OTHER_TOPIC',
  ]);
  assert.equal(supplied.token, 'environment');
  // A near-miss must not count: a typo in the label should leave the field asking for a
  // value, not produce a save that quietly does nothing.
  assert.equal(supplied.topic, undefined);
  assert.equal(supplied.server, undefined);
});

test('the _FILE form is reported as coming from a file', () => {
  const supplied = suppliedFields('Critical alerts', NTFY, [
    'SMARTSERVE_CHANNEL_CRITICAL_ALERTS_TOKEN_FILE',
  ]);
  assert.equal(supplied.token, 'file');
});

test('the plain variable wins when both forms are set', () => {
  const supplied = suppliedFields('Critical alerts', NTFY, [
    'SMARTSERVE_CHANNEL_CRITICAL_ALERTS_TOKEN_FILE',
    'SMARTSERVE_CHANNEL_CRITICAL_ALERTS_TOKEN',
  ]);
  assert.equal(supplied.token, 'environment', 'the same precedence the server applies');
});

test('a supplied field is not demanded by the editor', () => {
  // Gotify needs a server and a token. With the token supplied by the deployment, only the
  // server should still be asked for.
  const gotify = {
    type: 'gotify',
    fields: [{ key: 'server', required: true }, { key: 'token', required: true }],
  };
  const values = { server: 'https://gotify.example.com', token: '' };

  assert.equal(missingRequired('gotify', values), 'App token is required.');
  assert.equal(
    missingRequired('gotify', values, suppliedFields('Critical alerts', gotify, ['SMARTSERVE_CHANNEL_CRITICAL_ALERTS_TOKEN'])),
    null,
    'a token the deployment supplies must not block the save',
  );
  // Supplying one field must not excuse the others.
  assert.equal(
    missingRequired('gotify', { server: '', token: '' }, { token: 'environment' }),
    'Server is required.',
  );
});
