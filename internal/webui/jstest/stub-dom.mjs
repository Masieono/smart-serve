// A tiny DOM stub, just enough for the view helpers under test.
//
// It supports event listeners and dispatch on purpose. Dialogs settle on the
// `close` event, and a stub that swallowed listeners could not have caught the
// bug where a confirmation resolved before the user clicked anything.

/** A minimal CSSStyleDeclaration: enough to assert on what applyTheme sets. */
class StubStyle {
  constructor() {
    this.properties = new Map();
  }

  setProperty(name, value) {
    this.properties.set(name, String(value));
  }

  removeProperty(name) {
    this.properties.delete(name);
  }

  getPropertyValue(name) {
    return this.properties.get(name) || '';
  }
}

export class StubNode {
  constructor(tag) {
    this.tagName = tag.toUpperCase();
    this.childNodes = [];
    this.attrs = {};
    this.dataset = {};
    this.listeners = new Map();
    this.parent = null;

    // Declared so `el()` assigns them as properties: it uses `key in node` to
    // decide between a property and an attribute.
    this.onclick = null;
    this.oninput = null;
    this.onchange = null;
    this.onkeydown = null;

    this.textContent = '';
    this.className = '';
    this.value = '';
    this.hidden = false;
    this.disabled = false;
    this.open = false;
    this.checked = false;
    this.style = new StubStyle();
  }

  // Element.append takes any number of nodes; a stub that accepted only the first
  // would silently drop siblings and make a two-button footer look like a one-button
  // footer.
  append(...children) {
    for (const child of children) {
      if (child && typeof child === 'object') child.parent = this;
      this.childNodes.push(child);
    }
  }

  setAttribute(key, value) {
    this.attrs[key] = value;
  }

  getAttribute(key) {
    return this.attrs[key];
  }

  addEventListener(type, handler) {
    if (!this.listeners.has(type)) this.listeners.set(type, []);
    this.listeners.get(type).push(handler);
  }

  dispatch(type, event = {}) {
    for (const handler of [...(this.listeners.get(type) || [])]) {
      // preventDefault/stopPropagation are part of the contract handlers rely on, so
      // the stub provides them rather than letting a real handler throw.
      handler({
        type,
        target: this,
        currentTarget: this,
        preventDefault() {},
        stopPropagation() {},
        ...event,
      });
    }
  }

  /** Fire the click handler, as a real click would. */
  click() {
    if (this.onclick) this.onclick({ target: this, currentTarget: this });
  }

  // The slice of HTMLDialogElement that openDialog uses.
  showModal() {
    this.open = true;
  }

  close() {
    this.open = false;
    this.dispatch('close');
  }

  get firstChild() {
    return this.childNodes[0] ?? null;
  }

  get firstElementChild() {
    return this.childNodes.find((n) => n instanceof StubNode) ?? null;
  }

  removeChild(child) {
    this.childNodes = this.childNodes.filter((n) => n !== child);
    if (child && child.parent === this) child.parent = null;
  }

  remove() {
    if (this.parent) this.parent.removeChild(this);
  }

  /** The slice of Element.getBoundingClientRect the dialogs use for backdrop clicks. */
  getBoundingClientRect() {
    return this.boundingBox || { top: 0, left: 0, right: 100, bottom: 100, width: 100, height: 100 };
  }

  /** Depth-first search for the first descendant matching a predicate. */
  find(predicate) {
    for (const child of this.childNodes) {
      if (!(child instanceof StubNode)) continue;
      if (predicate(child)) return child;
      const nested = child.find(predicate);
      if (nested) return nested;
    }
    return null;
  }

  /** Every descendant with the given tag, in document order. */
  all(tag) {
    const out = [];
    const walk = (node) => {
      for (const child of node.childNodes) {
        if (!(child instanceof StubNode)) continue;
        if (child.tagName === tag.toUpperCase()) out.push(child);
        walk(child);
      }
    };
    walk(this);
    return out;
  }
}

class StubText extends StubNode {
  constructor(text) {
    super('#text');
    this.data = text;
  }
}

/** Install the stub as the global DOM. Call before importing view modules. */
export function installStubDom() {
  globalThis.Node = StubNode;
  globalThis.document = {
    body: new StubNode('body'),
    documentElement: new StubNode('html'),
    // api.js derives the base path from this at import time.
    baseURI: 'http://localhost/',
    createElement: (tag) => new StubNode(tag),
    createTextNode: (text) => new StubText(text),
    getElementById: () => null,
  };
  return globalThis.document;
}

/** Serve a JSON payload to any fetch() the module under test makes.
 *
 *  Pass an array as `requests` to collect what was sent — needed to assert on a save
 *  payload rather than only on the resulting DOM. */
export function stubFetch(payload, requests = null) {
  globalThis.fetch = async (url, init = {}) => {
    if (requests) {
      let body = null;
      try {
        body = init.body ? JSON.parse(init.body) : null;
      } catch {
        body = null; // not JSON; leave it unrecorded rather than throwing
      }
      requests.push({ url: String(url), method: init.method || 'GET', body });
    }
    return {
      ok: true,
      status: 200,
      statusText: 'OK',
      text: async () => JSON.stringify(payload),
    };
  };
}
