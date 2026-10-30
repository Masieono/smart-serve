// The tag rules (`FEATURES.md` D9), shared by the dashboard's export scope and the notification
// editor's group scope (`B12`).
//
// This file exists because two views needed the same answer to "which groups are there", and two
// answers to one question is how they come to disagree — the same argument `store/tags.go` makes for
// the split rule, which the Go side and this one have to keep matching.

import test from 'node:test';
import assert from 'node:assert/strict';

const { tagList, fleetTags } = await import('../assets/js/tags.js');

test('tagList splits, trims and drops blanks', () => {
  assert.deepEqual(tagList('tank, spare'), ['tank', 'spare']);
  assert.deepEqual(tagList(' tank , , backup '), ['tank', 'backup']);
  assert.deepEqual(tagList(''), []);
  assert.deepEqual(tagList('   '), []);
  // A drive with no tags at all, which the API sends as an empty string and a caller may send as
  // null — neither is a group called "null".
  assert.deepEqual(tagList(null), []);
  assert.deepEqual(tagList(undefined), []);
});

test('fleetTags lists every tag once, in a stable order', () => {
  const fleet = [
    { serial: 'a', tags: 'tank, spare' },
    { serial: 'b', tags: 'spare,bench' },
    { serial: 'c', tags: '   ' },
  ];
  assert.deepEqual(fleetTags(fleet), ['bench', 'spare', 'tank']);
  assert.deepEqual(fleetTags([]), []);
  assert.deepEqual(fleetTags(undefined), []);
});

// Every comparison the app makes about tags ignores case (`store.HasTag`), so the *list* has to as
// well. Listing `Tank` and `tank` separately would offer two checkboxes that do the same thing, and
// whichever one a user ticked would match the other's drives.
test('fleetTags de-duplicates case-insensitively, keeping the first spelling', () => {
  const fleet = [
    { serial: 'a', tags: 'Tank' },
    { serial: 'b', tags: 'tank, TANK ' },
    { serial: 'c', tags: 'spare' },
  ];
  assert.deepEqual(fleetTags(fleet), ['spare', 'Tank']);
});

// The order must not depend on the order the drives arrived in, or the dropdown reshuffles between
// two draws of the same page.
test('fleetTags sorts the same way whatever order the drives come in', () => {
  const tags = ['media', 'Bench', 'tank'];
  const forwards = tags.map((t, i) => ({ serial: String(i), tags: t }));
  const backwards = [...forwards].reverse();
  assert.deepEqual(fleetTags(forwards), fleetTags(backwards));
});
