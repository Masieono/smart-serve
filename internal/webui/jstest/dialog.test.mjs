import test from 'node:test';
import assert from 'node:assert/strict';
import { installStubDom } from './stub-dom.mjs';

installStubDom();
const { confirmDialog } = await import('../assets/js/components.js');

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
