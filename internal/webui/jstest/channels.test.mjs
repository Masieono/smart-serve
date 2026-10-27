import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom, StubNode } from './stub-dom.mjs';

installStubDom();
const { channelTypes, availableTypes, configFromValues, missingRequired, render } =
  await import('../assets/js/views/notifications.js');

// The Go registry must ship the same set; internal/notify/adapters_test.go pins the
// other end. Drift here means the UI offers a channel the server cannot deliver.
const EXPECTED_TYPES = ['ntfy', 'webhook', 'gotify', 'telegram', 'discord', 'smtp'];

test('the editor offers every registered channel type', () => {
  assert.deepEqual([...channelTypes()].sort(), [...EXPECTED_TYPES].sort());
});

test('availableTypes narrows to what the server reports', () => {
  assert.deepEqual(availableTypes(['ntfy', 'smtp']).map((t) => t.type), ['ntfy', 'smtp']);
});

test('availableTypes falls back when the server reports nothing usable', () => {
  for (const input of [null, undefined, [], 'ntfy', ['carrier-pigeon']]) {
    assert.equal(availableTypes(input).length, EXPECTED_TYPES.length, `input ${JSON.stringify(input)}`);
  }
});

test('configFromValues drops blank optional fields', () => {
  const config = configFromValues('ntfy', { server: 'https://ntfy.sh', topic: 'alerts', token: '  ' });
  assert.deepEqual(config, { server: 'https://ntfy.sh', topic: 'alerts' });
});

test('configFromValues trims values and parses the webhook headers', () => {
  const config = configFromValues('webhook', {
    url: '  https://example.com/hook  ',
    method: 'POST',
    headers: 'Authorization: Bearer abc\nX-Trace: 1\n\n',
  });
  assert.equal(config.url, 'https://example.com/hook');
  assert.deepEqual(config.headers, { Authorization: 'Bearer abc', 'X-Trace': '1' });
});

test('configFromValues omits an empty header block', () => {
  const config = configFromValues('webhook', { url: 'https://x', method: 'POST', headers: '   ' });
  assert.ok(!('headers' in config), 'an empty header block should be omitted');
});

// A blank port must let the server's own default apply rather than being sent as "".
test('configFromValues keeps the SMTP port only when set', () => {
  const withPort = configFromValues('smtp', {
    host: 'smtp.example.com', port: '587', from: 'a@b.c', to: 'x@y.z',
  });
  assert.equal(withPort.port, '587');

  const withoutPort = configFromValues('smtp', { host: 'smtp.example.com', from: 'a@b.c', to: 'x@y.z' });
  assert.ok(!('port' in withoutPort), 'a blank port should be omitted');
});

test('missingRequired names the first empty required field', () => {
  assert.equal(missingRequired('ntfy', { topic: 'alerts' }), null);
  assert.match(missingRequired('ntfy', {}), /Topic/);
  assert.match(missingRequired('smtp', { host: 'h', from: 'a@b.c' }), /Recipients/);
  assert.match(missingRequired('telegram', { token: 't' }), /Chat ID/);
  assert.match(missingRequired('discord', {}), /Webhook URL/);
});

// Every type must demand something, or an empty channel could be saved and would
// then fail at delivery time instead of at the form.
test('every channel type requires at least one field', () => {
  for (const type of channelTypes()) {
    assert.notEqual(missingRequired(type, {}), null, `${type} can be saved with no configuration`);
  }
});

// --- the subscription line on a channel card -----------------------------------

/** Serve the notifications view's four reads, routed by path. */
function stubNotificationsApi(channels) {
  globalThis.fetch = async (url) => {
    const path = String(url);
    const payload = path.includes('/api/channels') ? { channels }
      : path.includes('/api/notifications')
        ? { events: [], event_types: ['health_change'], channel_types: ['ntfy'], channel_env_vars: [] }
        : path.includes('/api/settings') ? { settings: {} }
          : { devices: [] };
    return { ok: true, status: 200, statusText: 'OK', text: async () => JSON.stringify(payload) };
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

async function drawWith(channel) {
  installStubDom();
  stubNotificationsApi([channel]);
  const container = new StubNode('div');
  await render(container, { refresh() {} });
  return textOf(container);
}

// An empty subscription is every event, not none of them: the setting lists what has been *narrowed
// away*, so nothing listed means nothing excluded. The card said "No events subscribed", which
// described the setting rather than its effect and read as a channel that would never speak — on
// exactly the channel a digest gets set up on, where the digest arrives *because* nothing was
// narrowed away.
test('a channel with no events ticked is described as receiving everything', async () => {
  const text = await drawWith({ id: 1, label: '4-hour digest', type: 'ntfy', enabled: true, events: '' });

  assert.match(text, /Everything/, `the card should say what the channel receives: ${text}`);
  assert.doesNotMatch(text, /No events subscribed/,
    `an empty subscription is not nothing: ${text}`);
  assert.match(text, /digest/, `the digest should be named, since that is what an empty list reaches: ${text}`);
});

// And a narrowed subscription still lists what it is narrowed to, rather than being summarised.
test('a narrowed channel lists its events', async () => {
  const text = await drawWith({
    id: 1, label: 'pager', type: 'ntfy', enabled: true,
    events: JSON.stringify(['health_change', 'self_test_result']),
  });

  assert.match(text, /health_change/);
  assert.match(text, /self_test_result/);
  assert.doesNotMatch(text, /Everything/, `a narrowed subscription is not everything: ${text}`);
});
