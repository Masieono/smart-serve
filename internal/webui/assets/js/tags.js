// The tag rules, in one place.
//
// A tag is free text on a drive (`FEATURES.md` D9): the storage is one comma-separated field and
// there is no table of tags anywhere, so "which tags are there" is a question asked of the drives.
// Two views ask it — the dashboard, to scope an export, and the notification editor, to scope a
// channel to a group (`B12`) — which is why it lives here rather than in either of them.
//
// The Go side splits the same field in `store.TagList`, and the two have to agree: a drive that
// grouped one way on screen and another way in an export would be a drive in a group the export
// does not know about.

/** A drive's tag string as a list of names: comma-separated, trimmed, blanks dropped. */
export function tagList(tags) {
  const out = [];
  for (const tag of String(tags == null ? '' : tags).split(',')) {
    const name = tag.trim();
    if (name) out.push(name);
  }
  return out;
}

/**
 * Every tag present on a fleet, once each and in a stable order.
 *
 * De-duplicated **case-insensitively**, first spelling winning, because every comparison the app
 * makes about tags ignores case (`store.HasTag`): offering `Tank` and `tank` as two entries would
 * be offering two checkboxes that do the same thing, and whichever one a user ticked would match
 * the other's drives.
 *
 * Sorted with `sensitivity: 'base'` so the order is stable across renders — a list that reshuffles
 * between two draws of the same page is a list nobody can learn.
 */
export function fleetTags(devices) {
  const found = new Map();
  for (const device of devices || []) {
    for (const name of tagList(device && device.tags)) {
      const key = name.toLowerCase();
      if (!found.has(key)) found.set(key, name);
    }
  }
  return [...found.values()].sort((a, b) => a.localeCompare(b, undefined, { sensitivity: 'base' }));
}
