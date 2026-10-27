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
 *
 * `onLogs` is called with the drive's log *as read live* whenever it changes. The panel above this
 * card is drawn from the last scan, so a test that has just finished exists nowhere else until
 * something rescans — and making a reader press the big Scan button to see the result of a test they
 * just ran is the wrong place to put it.
 */
export function selfTestCard(ctx, device, logs, onLogs = null) {
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
  // When this card started a test, and which one. The drive's own estimate against this clock is the
  // only progress figure available on a drive that does not report a percentage while a test runs.
  let startedAt = null;
  let startedKind = null;
  // The last log the page was handed, so a poll that changes nothing does not touch the DOM.
  let reportedLogs = '';
  // The newest finished result when this card was drawn. A *new* one is how a test that finished on a
  // drive which never reported it running gets noticed — the drive wrote a row, and that is evidence.
  const baseline = finishedSignature(logs);

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
      ? `This drive's self-test log could not be read (${unreadLog}), so progress and past results `
        + 'cannot be shown here. A test started from this button still runs on the drive.'
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

  /**
   * A test this card started that the drive is not reporting a percentage for.
   *
   * Some drives only write a self-test log entry when a test *finishes*, so there is no figure to
   * read while one runs — and "nothing is running" is indistinguishable from "this drive does not
   * report progress", which is how a working test came to look like a button that did nothing.
   *
   * So it falls back to the only honest figure available: the drive's **own estimate** of how long
   * its tests take, against the clock this card started one at. That is a different kind of number
   * from the drive's percentage and the card says so — it is the drive's estimate, not a reading.
   */
  function showStarted() {
    const minutes = startedKind === 'long' ? extendedMinutes : shortMinutes;
    const elapsed = startedAt === null ? 0 : Date.now() - startedAt;
    note.textContent = 'Started. This drive does not report a percentage while a test runs, so there '
      + `is nothing to read from it. It will keep asking every ${Math.round(POLL_MS / 1000)} `
      + 'seconds, and the result appears above as soon as the drive writes one.';
    mount(actions, [
      el('button', {
        class: 'btn btn-danger', type: 'button', title: 'Stop the running test', onclick: stop,
      }, [icon('x'), 'Stop test']),
    ]);

    progress.hidden = false;
    mount(progress, [
      el('div', { class: 'row' }, [
        el('span', { class: 'selftest-kind', text: startedKind === 'long' ? 'Long test' : 'Short test' }),
        el('span', { class: 'spacer' }),
        el('span', { class: 'muted', text: estimateText(elapsed, minutes) }),
      ]),
      // Only when the drive said how long its tests take. A bar with no basis would be a bar at an
      // invented position, which is the thing this app does not do.
      minutes
        ? el('div', { class: 'meter', title: "The drive's own estimate, not a reading from it" }, [
            el('div', { class: 'meter-fill', style: `width:${estimatePercent(elapsed, minutes)}%` }),
          ])
        : null,
    ]);
    schedule();
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
          startedAt = Date.now();
          startedKind = kind;
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

    // The drive's own log, read live. The panel above is drawn from the *last scan*, so a result
    // that has just been written exists only here — and tying it to the big Scan button would make
    // the reader re-read the whole fleet to see the outcome of one test.
    const live = state && state.logs ? state.logs : null;
    if (live) {
      const done = finishedEntry(live.self_test_entries);
      if (done) mount(result, resultLine(done));
      if (onLogs) {
        const signature = logSignature(live);
        if (signature !== reportedLogs) {
          reportedLogs = signature;
          onLogs(live);
        }
      }
    }

    const entry = state ? state.running : null;
    if (entry) {
      wasRunning = true;
      // Visibly running: there is nothing left to wait for, and the rescan is armed by the
      // running → not-running transition instead.
      awaitingResult = false;
      showRunning(entry);
      schedule();
      return;
    }

    // A test this card started has finished: the drive wrote a new result. On a drive that does not
    // report a running test that is the *only* evidence one ran — and it is good evidence, because
    // the drive wrote it.
    const liveEnded = live ? finishedSignature(live) !== baseline : false;

    // This card started a test and nothing has come of it yet. It may still be running, or the drive
    // may not report progress at all — nothing here can tell those apart. So the card says what it
    // can see and keeps asking, rather than guessing and rescanning the fleet on the strength of it.
    if (awaitingResult && !unreadLog && !liveEnded) {
      showStarted();
      return;
    }

    // A test this card *watched* running has ended, and that transition is the evidence. One rescan
    // refreshes the stored document and lets the finished-test notification fire promptly. Guarded
    // per card: the re-rendered card is drawn from stored logs that may still show the test running,
    // so an unguarded rescan would poll, rescan, poll, forever.
    const ended = wasRunning;
    wasRunning = false;
    awaitingResult = false;
    showIdle();
    if (ended && !recorded) {
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
  if (n < 60) return `${fmtInt(n)} minute${n === 1 ? '' : 's'}`;
  const hours = Math.floor(n / 60);
  const rest = n % 60;
  if (rest === 0) return `${hours} hour${hours === 1 ? '' : 's'}`;
  return `${hours} h ${rest} min`;
}

/** The newest finished result a log holds, or null. */
function finishedEntry(entries) {
  return (entries || []).find((e) => e.remaining === undefined || e.remaining === null) || null;
}

/**
 * A cheap identity for a log's newest finished result.
 *
 * Comparing this before and after a poll is how a test that finished on a drive which never reported
 * it *running* gets noticed. Nothing else distinguishes "still going" from "over" on such a drive,
 * and a new row is the drive saying so.
 */
function finishedSignature(summary) {
  const done = finishedEntry(summary && summary.self_test_entries);
  return done ? [done.type, done.status, done.hours].join('|') : '';
}

/** A cheap identity for a whole log, so a poll that changed nothing does not re-draw the panel. */
function logSignature(summary) {
  const first = (summary.self_test_entries || [])[0] || {};
  return [summary.self_test_count, summary.error_count, first.type, first.status, first.hours].join('|');
}

/**
 * How far along a test looks, as a percentage of the drive's own estimate.
 *
 * Capped short of full: the estimate is the drive's guess, and a bar sitting at 100% while the test
 * is still running reads as a hang — the exact failure a progress figure exists to prevent.
 */
function estimatePercent(elapsedMs, minutes) {
  const total = Number(minutes) * 60000;
  if (!Number.isFinite(total) || total <= 0) return 0;
  return Math.min(95, Math.round((elapsedMs / total) * 100));
}

/** The elapsed time, and the drive's estimate it is measured against. */
function estimateText(elapsedMs, minutes) {
  const elapsed = fmtDuration(Math.max(1, Math.round(elapsedMs / 60000)));
  if (!minutes) return `running for ${elapsed}`;
  return `running for ${elapsed} · the drive estimates about ${fmtDuration(minutes)}`;
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
