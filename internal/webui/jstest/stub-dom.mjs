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
    this.detached = false;
    this.style = new StubStyle();
  }

  // Mimics the native `Element.append`: anything that is not a Node is stringified into a
  // text node, *including* `null` and `undefined`.
  //
  // The project's own `append` helper filters those out before they reach here, so a null
  // arriving at this method means a call site bypassed the helper — which is exactly how the
  // attribute `?` dialog came to render the literal word "null". An earlier version of this
  // stub dropped such arguments silently, so the entire test suite was blind to it.
  append(...children) {
    for (const child of children) {
      if (child instanceof StubNode) {
        child.parent = this;
        this.childNodes.push(child);
      } else {
        this.childNodes.push(new StubText(String(child)));
      }
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
    const dispatchEvent = this.dispatchEvent(type, event);
    for (const handler of [...(this.listeners.get(type) || [])]) {
      handler(dispatchEvent);
    }
    dispatchEvent.done();
  }

  /**
   * An event object shaped like a real one.
   *
   * `currentTarget` is the trap this stub exists to reproduce: the spec sets it to null as soon as
   * dispatch finishes, so a handler that reads it *after* an `await` gets null. The stub used to
   * hand out a plain property that stayed valid forever, which made the whole suite blind to every
   * `finally { event.currentTarget.disabled = false }` — each one threw a TypeError into an
   * unhandled promise and left its button disabled for the rest of the session.
   */
  dispatchEvent(type, event = {}) {
    let current = this;
    const shaped = {
      type,
      target: this,
      preventDefault() {},
      stopPropagation() {},
      ...event,
      get currentTarget() {
        return current;
      },
    };
    // Not a property the caller can shadow: the point is that it becomes null.
    shaped.done = () => { current = null; };
    return shaped;
  }

  /**
   * Fire the click handler, as a real click would.
   *
   * Returns whatever the handler returned, so a test can await an async handler — and goes through
   * `dispatchEvent`, so the handler sees the same `currentTarget` behaviour the browser gives it.
   */
  click() {
    if (!this.onclick) return undefined;
    const event = this.dispatchEvent('click');
    try {
      return this.onclick(event);
    } finally {
      event.done();
    }
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
    if (child && child.parent === this) {
      child.parent = null;
      // Remembered, because a removed node and a never-appended root both have no parent, and
      // only one of them is connected.
      child.detached = true;
    }
  }

  /**
   * The slice of `Node.isConnected` the self-test card uses to stop polling a card that has left
   * the page. Connected means nothing has removed it and every ancestor is connected too; a bare
   * root with no parent counts, because that is how the tests build a container.
   */
  get isConnected() {
    if (this.detached) return false;
    return this.parent === null || this.parent.isConnected;
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
