import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom } from './stub-dom.mjs';

installStubDom();
const { confirmDialog, openDialog } = await import('../assets/js/components.js');

/** Open a confirm dialog and return its promise plus the dialog element. */
function openConfirm(...args) {
  const promise = confirmDialog(...args);
  const dialog = document.body.firstElementChild;
  assert.ok(dialog, 'a dialog should have been opened');
  const [cancel, confirm] = dialog.all('button');
  return { promise, dialog, cancel, confirm };
}

test('confirmDialog resolves true when the confirm button is clicked', async () => {
  installStubDom();
  const { promise, dialog, cancel, confirm } = openConfirm('Delete schedule', 'Delete "nightly"?', 'Delete');

  assert.equal(dialog.open, true, 'the dialog should be modal');
  assert.equal(cancel.textContent, 'Cancel');
  assert.equal(confirm.textContent, 'Delete');

  confirm.click();
  assert.equal(await promise, true);
});

test('confirmDialog resolves false when cancelled', async () => {
  installStubDom();
  const { promise, cancel } = openConfirm('Delete schedule', 'Delete "nightly"?', 'Delete');

  cancel.click();
  assert.equal(await promise, false);
});

test('confirmDialog resolves false when dismissed without a button press', async () => {
  installStubDom();
  const { promise, dialog } = openConfirm('Delete schedule', 'Delete "nightly"?', 'Delete');

  dialog.close(); // what the browser does on Escape
  assert.equal(await promise, false);
});

// Regression: the promise used to settle on a zero-delay timer, so it resolved
// "cancelled" on the next macrotask — before the user could possibly click. Every
// caller's `if (!(await confirmDialog(...))) return` bailed out, and deleting a
// schedule (or a channel) silently did nothing while looking like it worked.
test('confirmDialog waits for a decision instead of settling on its own', async () => {
  installStubDom();
  let settled = false;
  const promise = openConfirm('Delete schedule', 'Delete "nightly"?', 'Delete').promise.then((value) => {
    settled = true;
    return value;
  });

  await new Promise((resolve) => setTimeout(resolve, 25));
  assert.equal(settled, false, 'the promise must not settle before the user decides');

  document.body.firstElementChild.all('button')[1].click();
  assert.equal(await promise, true);
});

// --- the backdrop is a press, not a click that ends up outside -----------------

// A click whose target is the dialog and whose coordinates are outside its box used to be read as a
// backdrop click, and that is not the same thing. Selecting text in a field, or dragging a number
// spinner, ends with exactly that: the `click` fires on the nearest common ancestor of press and
// release, which is the dialog, at coordinates past its edge. The schedule form was closing out from
// under a half-filled one, so the press is what decides now.

const BOX = { top: 100, left: 100, right: 400, bottom: 300, width: 300, height: 200 };
const BACKDROP = { clientX: 10, clientY: 10 };

/** Open a dialog with a known box, so "outside" means something. */
function openInBox(options) {
  const { dialog } = openDialog('Edit schedule', () => {}, options);
  dialog.boundingBox = BOX;
  return dialog;
}

test('a press on the backdrop dismisses the dialog', () => {
  installStubDom();
  const dialog = openInBox();

  dialog.dispatch('pointerdown', BACKDROP);
  dialog.dispatch('click', BACKDROP);

  assert.equal(dialog.open, false, 'a click on the backdrop should still close the dialog');
});

test('a drag that starts inside the dialog does not dismiss it', () => {
  installStubDom();
  const dialog = openInBox();
  const field = dialog.all('div')[0]; // anything inside the box

  // The press lands on a field...
  dialog.dispatch('pointerdown', { ...BACKDROP, target: field });
  // ...and the release lands outside, which is what selecting text looks like.
  dialog.dispatch('click', BACKDROP);

  assert.equal(dialog.open, true, 'a drag out of the dialog must not close it');
});

test('a press inside the dialog never dismisses it', () => {
  installStubDom();
  const dialog = openInBox();
  const inside = { clientX: 200, clientY: 200 };

  dialog.dispatch('pointerdown', inside);
  dialog.dispatch('click', inside);

  assert.equal(dialog.open, true);
});

test('dismissOnBackdrop: false keeps a form open through a backdrop click', () => {
  installStubDom();
  const dialog = openInBox({ dismissOnBackdrop: false });

  dialog.dispatch('pointerdown', BACKDROP);
  dialog.dispatch('click', BACKDROP);
  assert.equal(dialog.open, true, 'a form holding unsaved input must survive a stray click');

  // Cancel and Escape are still there — the dialog is not stuck.
  dialog.close();
  assert.equal(dialog.open, false);
});
