// Self-test triggering (FEATURES.md A19, docs/UI.md §7.2).
//
// The only part of this app that changes a drive, and the only part that polls one. Both are
// deliberate, and both are bounded. A test starts only from a button press — never on a schedule,
// never during a scan, never on a threshold — and the poll runs only while a test is in progress
// and the card is still on screen.
//
// Progress comes from the drive rather than from a clock. smartctl's self-test log carries a
// percentage remaining only while a test runs, so the figure is the drive's own, and an absent
// one means "no test running" rather than "no progress yet" (internal/model/logs.go, Running).

import { el, mount, clear, fmtInt } from '../dom.js';
import { api } from '../api.js';
import { confirmDialog, toast } from '../components.js';
import { icon } from '../icons.js';

/**
 * How often to ask the drive how far along it is.
 *
 * A long test on a large drive moves in single digits of percent per hour, so polling faster
 * would spend device reads and buy nothing visible. Fifteen seconds is still short enough that a
 * two-minute short test shows real movement.
 */
export const POLL_MS = 15000;

/** The poll timer for the card on screen.
 *
 *  One at a time, cleared before every start: a render replaces the card, and a timer left behind
 *  by the previous one would go on reading the drive for a panel nobody can see. */
let timer = null;

export function stopPolling() {
  if (timer !== null) {
    clearTimeout(timer);
    timer = null;
  }
}

/**
 * The self-test card.
 *
 * `logs` is the log summary from the last scan, which is enough to draw the card without touching
 * the drive: whether it supports self-tests at all, its own time estimates, and — if a test was
 * already running when the drive was last scanned — the fact that one is.
 */
export function selfTestCard(ctx, device, logs) {
  const note = el('p', { class: 'muted selftest-note' });
  const actions = el('div', { class: 'row selftest-actions' });
  const progress = el('div', { class: 'selftest-progress', hidden: true });

  const card = el('div', { class: 'card stack panel-wide selftest' }, [
    el('h2', { text: 'Self-test' }),
    note,
    actions,
    progress,
  ]);

  if (logs && logs.self_test_supported === false) {
    stopPolling();
    note.textContent = 'This drive cannot run self-tests. '
      + (logs.note ? `smartctl: ${logs.note}` : 'Its self-test log could not be read.');
    return card;
  }

  const shortMinutes = logs ? logs.short_minutes : undefined;
  const extendedMinutes = logs ? logs.extended_minutes : undefined;

  // Whether a test was in progress on the previous reading, and whether this card has already
  // recorded the one that ended. Both are per-card, and deliberately so: the rescan below finishes
  // before the page re-renders, so the next card is drawn from fresh logs and cannot loop. A
  // remembered-forever guard would instead stop a *later* test from ever being recorded.
  let wasRunning = false;
  let recorded = false;

  /** No test running: offer one, and say what it costs. */
  function showIdle() {
    stopPolling();
    clear(progress);
    progress.hidden = true;
    note.textContent = idleNote(shortMinutes, extendedMinutes);
    mount(actions, [
      runButton('Short test', 'short', shortMinutes,
        'Checks the electronics and a sample of the media. The drive stays usable throughout.'),
      runButton('Long test', 'long', extendedMinutes,
        'Reads every sector. The drive stays usable, but slower, and it cannot be paused.'),
    ]);
  }

  /** A test running: say how far along, and offer the way out. */
  function showRunning(entry) {
    note.textContent = 'The drive is testing itself. You can keep using the app — progress is read '
      + `from the drive every ${Math.round(POLL_MS / 1000)} seconds, and the test ends by itself.`;
    mount(actions, [
      el('button', {
        class: 'btn btn-danger', type: 'button',
        title: 'Stop the running test', onclick: stop,
      }, [icon('x'), 'Stop test']),
    ]);

    const left = entry ? entry.remaining : undefined;
    progress.hidden = false;
    mount(progress, [
      el('div', { class: 'row' }, [
        el('span', { class: 'selftest-kind', text: (entry && entry.type) || 'Self-test' }),
        el('span', { class: 'spacer' }),
        el('span', { class: 'muted', text: left === undefined ? 'running' : `${fmtInt(left)}% left` }),
      ]),
      // The bar is only drawn when the drive gave a figure. A bar with no percentage would be a
      // bar at an unknown position, and a missing figure is not zero.
      left === undefined ? null : el('div', { class: 'meter' }, [
        el('div', {
          class: 'meter-fill',
          style: `width:${Math.max(0, Math.min(100, 100 - Number(left)))}%`,
        }),
      ]),
    ]);
  }

  function runButton(label, kind, minutes, detail) {
    return el('button', {
      class: 'btn', type: 'button', title: detail,
      onclick: async (event) => {
        // First statement, deliberately: the confirmation below is an `await`, and
        // `event.currentTarget` is null by the time it resolves.
        const button = event.currentTarget;
        const what = label.toLowerCase();
        const ok = await confirmDialog(
          `Run a ${what}?`,
          `The drive tests itself${minutes ? `, which it estimates at about ${fmtDuration(minutes)}` : ''}. `
          + `${detail} You can stop it at any time from this page.`,
          `Run ${what}`,
        );
        if (!ok) return;
        button.disabled = true;
        try {
          const res = await api.startSelfTest(device.serial, kind);
          toast((res && res.note) || `${label} started`, 'good');
          await read();
        } catch (err) {
          toast(err.message, 'error');
          button.disabled = false;
        }
      },
    }, [icon('scan'), label]);
  }

  async function stop() {
    const ok = await confirmDialog(
      'Stop the running test',
      'The drive abandons the test where it is. Nothing is written to the self-test log, so the '
      + 'last result stays whatever the previous finished test was. A long test cannot be resumed.',
      'Stop test',
    );
    if (!ok) return;
    try {
      const res = await api.abortSelfTest(device.serial);
      toast((res && res.note) || 'Test stopped', 'good');
      await read();
    } catch (err) {
      toast(err.message, 'error');
    }
  }

  /**
   * Read the drive's current state and draw from that.
   *
   * Drawn from what the *drive* says rather than from what was asked of it: a test the drive
   * refused, or one that finished while nobody was looking, must not leave a progress bar
   * counting down towards nothing.
   */
  async function read() {
    let state;
    try {
      state = await api.selfTest(device.serial);
    } catch (err) {
      toast(`Could not read the self-test state: ${err.message}`, 'error');
      showIdle();
      return;
    }

    const entry = state ? state.running : null;
    if (entry) {
      wasRunning = true;
      showRunning(entry);
      schedule();
      return;
    }

    showIdle();
    // The log table above this card is drawn from the last scan, which predates whatever just
    // happened to the test. One rescan puts the outcome in it — and it is needed after an abort
    // too, or the stored log would go on claiming a test is running until the next scheduled scan
    // replaced it.
    if (wasRunning && !recorded) {
      wasRunning = false;
      recorded = true;
      ctx.scanDevice(device.serial);
    }
  }

  /** Ask again later, unless the card has left the page in the meantime. */
  function schedule() {
    stopPolling();
    timer = setTimeout(tick, POLL_MS);
  }

  async function tick() {
    timer = null;
    // Navigating away detaches the card. A timer that outlived it would go on reading the drive
    // for a panel nobody can see, which is exactly what this feature must not do.
    if (card.isConnected === false) return;
    await read();
  }

  // A test that was already running when the drive was last scanned is followed straight away, so
  // reloading the page mid-test picks the progress back up instead of showing a stale percentage
  // until something else happens to change it.
  const stored = logs && Array.isArray(logs.self_test_entries)
    ? logs.self_test_entries.find((e) => e.remaining !== undefined)
    : null;
  if (stored) {
    wasRunning = true;
    showRunning(stored);
    schedule();
  } else {
    showIdle();
  }

  return card;
}

/**
 * What a self-test costs, before anyone presses anything.
 *
 * The estimates are the drive's own, from its capability page. A guess from this app would be
 * worse than no figure at all, because the only reason to show one is to know how long the drive
 * will be busy.
 */
function idleNote(shortMinutes, extendedMinutes) {
  const parts = [];
  if (shortMinutes) parts.push(`a short test about ${fmtDuration(shortMinutes)}`);
  if (extendedMinutes) parts.push(`a long test about ${fmtDuration(extendedMinutes)}`);
  const cost = parts.length
    ? `This drive estimates ${parts.join(' and ')}.`
    : 'This drive does not say how long its tests take.';
  return `${cost} Nothing runs on its own: a self-test only starts when you ask for one.`;
}

/** A drive's own estimate, in the units a person would say it in. */
function fmtDuration(minutes) {
  const n = Number(minutes);
  if (!Number.isFinite(n) || n <= 0) return '';
  if (n < 60) return `${fmtInt(n)} minutes`;
  const hours = Math.floor(n / 60);
  const rest = n % 60;
  return rest === 0 ? `${hours} hours` : `${hours} h ${rest} min`;
}
