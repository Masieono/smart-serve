// API client (docs/API.md). Every path is relative to the app base path, so
// subpath hosting works (ADR-015).
const basePath = new URL('.', document.baseURI).pathname.replace(/\/$/, '');

export function appPath(path) {
  return basePath + path;
}

async function request(method, path, body) {
  const init = { method };
  if (body !== undefined) {
    init.headers = { 'Content-Type': 'application/json' };
    init.body = JSON.stringify(body);
  }
  const res = await fetch(appPath(path), init);
  const text = await res.text();
  let data = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = null;
    }
  }
  if (!res.ok) {
    throw new Error((data && data.error) || `${res.status} ${res.statusText}`);
  }
  return data;
}

export const api = {
  devices: () => request('GET', '/api/devices'),
  device: (serial) => request('GET', `/api/devices/${encodeURIComponent(serial)}`),
  history: (serial, from) => request(
    'GET',
    `/api/devices/${encodeURIComponent(serial)}/history${from ? `?from=${encodeURIComponent(from)}` : ''}`,
  ),
  setNotes: (serial, notes) => request('PUT', `/api/devices/${encodeURIComponent(serial)}/notes`, notes),
  // Acknowledging a condition the user already knows about (FEATURES.md B7). It silences the alerts
  // for that condition and changes no finding: the verdict stays what it is.
  ackDevice: (serial, note) =>
    request('PUT', `/api/devices/${encodeURIComponent(serial)}/ack`, { note }),
  unackDevice: (serial) => request('DELETE', `/api/devices/${encodeURIComponent(serial)}/ack`),

  // The drive knowledge base (FEATURES.md A21). The refresh is a POST because it changes stored state
  // — and it is a button, never a timer: nothing here polls while the app is idle.
  knowledge: () => request('GET', '/api/knowledge'),
  refreshKnowledge: () => request('POST', '/api/knowledge/refresh'),
  putKnowledgeOverrides: (payload) => request('PUT', '/api/knowledge/overrides', { payload }),
  clearKnowledgeCache: () => request('DELETE', '/api/knowledge/cache'),
  setTransport: (serial, smartD) =>
    request('PUT', `/api/devices/${encodeURIComponent(serial)}/transport`, { smart_d: smartD }),
  reportUrl: (serial, format = 'txt') =>
    appPath(`/api/devices/${encodeURIComponent(serial)}/report?format=${format}`),

  async raw(serial) {
    const res = await fetch(appPath(`/api/devices/${encodeURIComponent(serial)}/raw`));
    if (!res.ok) throw new Error(`raw output unavailable (${res.status})`);
    return res.text();
  },

  scan: (wait = false) => request('POST', `/api/scan${wait ? '?wait=1' : ''}`),
  scanDevice: (serial) => request('POST', `/api/devices/${encodeURIComponent(serial)}/scan`),
  forgetDevice: (serial) => request('DELETE', `/api/devices/${encodeURIComponent(serial)}`),
  scans: () => request('GET', '/api/scans'),

  settings: () => request('GET', '/api/settings'),
  putSettings: (kv) => request('PUT', '/api/settings', kv),

  schedules: () => request('GET', '/api/schedules'),
  createSchedule: (s) => request('POST', '/api/schedules', s),
  updateSchedule: (s) => request('PUT', `/api/schedules/${s.id}`, s),
  deleteSchedule: (id) => request('DELETE', `/api/schedules/${id}`),
  runSchedule: (id) => request('POST', `/api/schedules/${id}/run`),

  channels: () => request('GET', '/api/channels'),
  createChannel: (c) => request('POST', '/api/channels', c),
  updateChannel: (c) => request('PUT', `/api/channels/${c.id}`, c),
  deleteChannel: (id) => request('DELETE', `/api/channels/${id}`),
  testChannel: (id) => request('POST', `/api/channels/${id}/test`),
  // Sends a test through a channel described in the body, so the editor can verify
  // what is on screen without saving it first.
  testChannelConfig: (c) => request('POST', '/api/channels/test', c),
  notifications: (limit = 25) => request('GET', `/api/notifications?limit=${limit}`),

  eventsUrl: () => appPath('/api/events'),

  // Import/export (FEATURES.md C7). The export is a URL rather than a fetch: it is a download, and
  // the browser should save it rather than this code holding it in memory.
  configExportUrl: () => appPath('/api/config/export'),
  importConfig: (bundle) => request('POST', '/api/config/import', bundle),

  // The fleet export (FEATURES.md A23). A URL for the same reason as the config export: it is a
  // file to keep, and the browser's own save dialog is the right one to put it somewhere.
  //
  // `tag` narrows it to one tag, which is how a bench batch gets its own sheet (FEATURES.md D11).
  // It is a scope the caller asked for, unlike the dashboard's filter, which the export ignores.
  fleetExportUrl: (format = 'csv', tag = '') =>
    appPath(`/api/devices/export?format=${format}${tag ? `&tag=${encodeURIComponent(tag)}` : ''}`),

  // Self-tests (FEATURES.md A19). The GET reads the drive live rather than from the stored
  // document, because progress exists nowhere else; the POSTs are the only requests in this
  // client that change a drive, so they are named for what they do.
  selfTest: (serial) => request('GET', `/api/devices/${encodeURIComponent(serial)}/selftest`),
  startSelfTest: (serial, kind) =>
    request('POST', `/api/devices/${encodeURIComponent(serial)}/selftest`, { kind }),
  abortSelfTest: (serial) =>
    request('POST', `/api/devices/${encodeURIComponent(serial)}/selftest/abort`),
};
