import test from 'node:test';
import assert from 'node:assert/strict';

// Minimal DOM stub: just enough for the helpers under test.
class StubNode {
  constructor(tag) {
    this.tagName = tag.toUpperCase();
    this.childNodes = [];
    this.attrs = {};
  }

  append(child) {
    this.childNodes.push(child);
  }

  setAttribute(key, value) {
    this.attrs[key] = value;
  }

  addEventListener() {}

  get firstChild() {
    return this.childNodes[0] ?? null;
  }

  removeChild(child) {
    this.childNodes = this.childNodes.filter((n) => n !== child);
  }
}

class StubText extends StubNode {
  constructor(text) {
    super('#text');
    this.data = text;
  }
}

globalThis.Node = StubNode;
globalThis.document = {
  createElement: (tag) => new StubNode(tag),
  createTextNode: (text) => new StubText(text),
};

const { el, mount, fmtBytes, fmtHours, fmtInt, scoreGrade } = await import('../assets/js/dom.js');

// Regression: an array of nodes used to be stringified into
// "[object HTMLElement],[object HTMLElement]" instead of being appended.
test('mount flattens an array of nodes instead of stringifying them', () => {
  const grid = document.createElement('div');
  mount(grid, [el('article'), el('article'), el('article')]);
  assert.equal(grid.childNodes.length, 3);
  assert.ok(grid.childNodes.every((node) => node instanceof StubNode));
});

test('mount accepts a mix of nodes, arrays and text', () => {
  const host = document.createElement('div');
  mount(host, el('h1'), [el('p'), el('p')], 'trailing text');
  assert.equal(host.childNodes.length, 4);
});

test('el appends nested arrays of children', () => {
  const row = el('div', {}, [el('span'), [el('span'), el('span')]]);
  assert.equal(row.childNodes.length, 3);
});

test('table-style row arrays render as rows', () => {
  const tbody = el('tbody', {}, [
    el('tr', {}, [el('td', { text: 'a' })]),
    el('tr', {}, [el('td', { text: 'b' })]),
  ]);
  assert.equal(tbody.childNodes.length, 2);
});

test('formatters', () => {
  assert.equal(fmtBytes(1000204886016), '1.0 TB');
  assert.equal(fmtBytes(null), 'unknown');
  assert.equal(fmtHours(8760), '1.0 yr');
  assert.equal(fmtHours(48), '2 d');
  assert.equal(scoreGrade(100), 'Excellent');
  assert.equal(scoreGrade(74), 'Fair');
  assert.equal(scoreGrade(10), 'Critical');
  assert.equal(scoreGrade(-1), 'n/a');
});

test('fmtInt groups raw counters and blanks missing values', () => {
  assert.equal(fmtInt(null), '—');
  assert.equal(fmtInt(undefined), '—');
  assert.equal(fmtInt(0), '0');
  // Locale-aware grouping; compare against the platform's own rendering.
  assert.equal(fmtInt(31603), (31603).toLocaleString());
});
