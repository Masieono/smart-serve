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
  // The last result the drive is still holding on to, shown whether or not a test is running. It
  // answers "did the test I ran yesterday pass?", which is a different question from "is one running
  // now?" — and it used to be answerable only by finding the right row in the log table above.
  const result = el('div', { class: 'selftest-result' });

  const card = el('div', { class: 'card stack panel-wide selftest' }, [
    el('h2', { text: 'Self-test' }),
    note,
    actions,
    progress,
    result,
  ]);

  if (logs && logs.self_test_supported === false) {
    stopPolling();
    note.textContent = 'This drive cannot run self-tests. '
      + (logs.note ? `smartctl: ${logs.note}` : 'Its self-test log could not be read.');
    return card;
  }

  const shortMinutes = logs ? logs.short_minutes : undefined;
  const extendedMinutes = logs ? logs.extended_minutes : undefined;
  // A self-test log this app does not read: the test still runs on the drive, but neither its
  // progress nor its result can be shown here. See `internal/smart/logs.go`.
  const unreadLog = logs ? logs.self_test_log_unread : undefined;

  // Whether a test was in progress on the previous reading, and whether this card has already
  // recorded the one that ended. Both are per-card, and deliberately so: the rescan below finishes
  // before the page re-renders, so the next card is drawn from fresh logs and cannot loop. A
  // remembered-forever guard would instead stop a *later* test from ever being recorded.
  let wasRunning = false;
  let recorded = false;
  // Whether a test *this card* started is still waiting to be accounted for. It drives two things:
  // the note that explains a "nothing is running" reading, and the single rescan that puts the
  // outcome into the log table above — which is drawn from the last scan, so a short test that
  // finished between two readings would otherwise leave no trace anywhere.
  let awaitingResult = false;

  const latest = logs && Array.isArray(logs.self_test_entries)
    ? logs.self_test_entries.find((e) => e.remaining === undefined || e.remaining === null)
    : null;
  if (latest) mount(result, resultLine(latest));

  /** No test running: offer one, and say what it costs. */
  function showIdle() {
    stopPolling();
    clear(progress);
    progress.hidden = true;
    note.textContent = unreadLog
      ? `This drive keeps its self-test log in a form this app does not read (${unreadLog}), so `
        + 'progress and past results cannot be shown here. A test started from this button still '
        + 'runs on the drive.'
      : idleNote(shortMinutes, extendedMinutes);
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
          awaitingResult = true;
          await read();
        } catch (err) {
          toast(err.message, 'error');
          // A long test is refused by drives that do not implement an extended self-test, and the
          // button inviting a second attempt is worse than no button: the answer will not change.
          //
          // Disabled here rather than remembered, and that is deliberate. "Refused once" is not the
          // same fact as "cannot" — a drive can refuse a command it would take a minute later — and
          // nothing smartctl reports in advance says which tests a drive supports. Greying the
          // button out on a guess would hide a capability that works.
          if (kind === 'long') {
            button.disabled = true;
            note.textContent = `This drive refused a long test — ${err.message} The short test `
              + 'checks the electronics and a sample of the media, and is the one to fall back on.';
          } else {
            button.disabled = false;
          }
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

    // A test that has stopped running needs one rescan to put its outcome into the log table above,
    // which is drawn from the last scan. Three things reach here: a test this card watched finish, a
    // test it started that finished before the first reading, and an abort. The middle one is why
    // `awaitingResult` is part of this rather than only `wasRunning` — a short test on a fast drive
    // can be over before the first reading, so a card that waited to *see* it running would never
    // ask for the rescan, and the result would reach the page only if the user thought to press
    // rescan themselves.
    const needsScan = (wasRunning || awaitingResult) && !recorded;
    const justStarted = awaitingResult;
    wasRunning = false;
    awaitingResult = false;

    showIdle();
    if (justStarted && !unreadLog) {
      // The drive reports no test in progress. A short test can finish before the first reading, in
      // which case its result appears once the log has been re-read. Saying which of those it might
      // be beats a button that appears to have done nothing.
      note.textContent = 'The drive reports no test in progress. A short test can finish before '
        + 'the first reading, in which case its result is above.';
    }
    if (needsScan) {
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

/**
 * One finished test, in the drive's own words.
 *
 * The verdict is the drive's own `passed` flag and nothing else. A test the drive reports no verdict
 * on is not a failure — rendering an absent flag as one would put a red badge on a drive that never
 * said anything was wrong, and it is the same rule the rest of the app follows for absent values.
 */
function resultLine(entry) {
  const passed = entry.passed === true;
  const failed = entry.passed === false;
  const tone = passed ? 'good' : failed ? 'bad' : 'unknown';
  const outcome = passed ? 'passed' : failed ? 'failed' : 'no verdict';
  return el('div', { class: 'row selftest-last' }, [
    el('span', { class: 'muted', text: 'Last test' }),
    el('span', { class: 'selftest-kind', text: entry.type || 'Self-test' }),
    el('span', { class: `badge badge-${tone}`, text: outcome }),
    el('span', { class: 'spacer' }),
    entry.hours === undefined || entry.hours === null
      ? null
      : el('span', { class: 'muted', text: `at ${fmtInt(entry.hours)} hours` }),
  ]);
}
