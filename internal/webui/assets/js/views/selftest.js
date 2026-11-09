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
 * Fifteen seconds is the default because a long test on a large drive moves in single digits of
 * percent per hour — polling faster spends device reads and buys nothing visible, and the drive is by
 * definition already busy. It is a *setting* rather than a constant because the trade belongs to the
 * owner: a short test on a fast drive shows real movement every couple of seconds, and a drive behind
 * a bridge that dislikes extra commands would rather be asked less often.
 */
export const POLL_MS = 15000;

let pollSeconds = POLL_MS / 1000;

/**
 * The stored refresh interval, applied before the first render.
 *
 * Out-of-range and unreadable values fall back to the default rather than being clamped: a stored `0`
 * would mean a poll every frame, and a value that is wrong because somebody edited the database is
 * not a preference to honour.
 */
export function setPollSeconds(seconds) {
  const n = Number(seconds);
  pollSeconds = Number.isFinite(n) && n >= 1 && n <= 60 ? n : POLL_MS / 1000;
}

const pollMs = () => pollSeconds * 1000;

/** The poll timer for the card on screen, and the moment the next read is due.
 *
 *  One at a time, cleared before every start: a render replaces the card, and a timer left behind
 *  by the previous one would go on reading the drive for a panel nobody can see.
 *
 *  `dueAt` is a *deadline* rather than an interval, and that is what stops a re-render from starving
 *  the poll. Every render builds a new card, a new card watching a running test arms the timer, and
 *  arming a fresh full interval each time means a page that re-renders more often than the interval
 *  never reaches a read at all. That is not hypothetical here: a `selftest_progress` event, a
 *  device re-read or the freshness re-read all re-render the page, and each one pushed back the very
 *  poll that would have cleared a stale progress bar. Carrying the deadline across the re-render
 *  means the read happens when it was always going to happen.
 */
let timer = null;
let dueAt = null;

export function stopPolling() {
  if (timer !== null) {
    clearTimeout(timer);
    timer = null;
  }
  dueAt = null;
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
export function selfTestCard(ctx, device, logs, onLogs = null, renderHistory = null) {
  const note = el('p', { class: 'muted selftest-note' });
  const actions = el('div', { class: 'row selftest-actions' });
  const progress = el('div', { class: 'selftest-progress', hidden: true });
  // The drive's self-test history, in this card (`FEATURES.md` A20): one subject, one place, under its
  // own heading, with the newest result as the table's first row.
  const history = el('div', { class: 'selftest-history' });

  const card = el('div', { class: 'card stack panel-wide selftest' }, [
    el('h2', { text: 'Self-test' }),
    note,
    actions,
    progress,
    history,
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
  // The newest finished result when this card was drawn. Re-read from the drive before a test starts,
  // because this card's copy comes from the last scan and is stale the moment a test has run since.
  let baseline = finishedSignature(logs);

  // The drive's history, drawn from whatever log the card was last handed — the stored one on mount,
  // and the *live* one after every read. One function, so the table cannot be showing one reading
  // while the progress bar shows another.
  //
  // The renderer is passed in rather than imported: the log table lives in the device view, and
  // importing it from here would make the two modules depend on each other.
  function drawHistory(summary) {
    if (renderHistory) mount(history, renderHistory(summary));
  }
  drawHistory(logs);

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
      + `from the drive every ${pollSeconds} seconds, and the test ends by itself.`;
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
   * report progress".
   *
   * So it falls back to the only honest figure available: the drive's **own estimate** of how long
   * its tests take, against the clock this card started one at. That is a different kind of number
   * from the drive's percentage and the card says so — it is the drive's estimate, not a reading.
   */
  function showStarted() {
    const minutes = startedKind === 'long' ? extendedMinutes : shortMinutes;
    const elapsed = startedAt === null ? 0 : Date.now() - startedAt;
    note.textContent = 'Started. This drive does not report a percentage while a test runs, so there '
      + `is nothing to read from it. It will keep asking every ${pollSeconds} `
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
    const cost = fmtShort(minutes);
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
          // Read the drive *before* asking it to do anything. Two reasons, both of them real:
          //
          //   - the baseline this card compares against has to be what the drive actually holds. Taken
          //     from the last scan's copy it is stale the moment a test has run since, and a stale
          //     baseline makes the first look after starting read as a *finished* test.
          //   - a drive already testing itself refuses a second request, and "the drive refused it"
          //     is a worse answer than not asking.
          let before = null;
          try {
            before = await api.selfTest(device.serial);
          } catch {
            /* the start is worth more than the baseline; keep what we have */
          }
          if (before && before.running) {
            toast('This drive is already running a self-test', 'error');
            await read();
            return;
          }
          if (before && before.logs) baseline = finishedSignature(before.logs);

          const res = await api.startSelfTest(device.serial, kind);
          toast((res && res.note) || `${label} started`, 'good');
          startedAt = Date.now();
          startedKind = kind;
          awaitingResult = true;
          // Record it, so the fleet can badge this drive (`FEATURES.md` A20) — otherwise the badge
          // would not appear until the next full scan, which on a quiet homelab could be tomorrow.
          //
          // Not `ctx.scanDevice`, which toasts and re-renders: the card has already drawn the
          // progress from a live read, and re-routing would throw the reader out of the card they are
          // watching. And a failure here is not worth an error — the test is running either way, and
          // the only thing lost is a badge.
          api.scanDevice(device.serial).catch(() => {});
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
    }, [icon('scan'), label, cost
      // The estimate on the button itself, which is where the decision is made: the figure answers "how
      // long will this take?" at the moment of asking.
      ? el('span', { class: 'btn-cost', text: `· ${cost}` })
      : null]);
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
      // "The abort was sent" and "the test stopped" are different facts, and only the drive knows the
      // second one (`FEATURES.md` A20). A bridge that does not pass the abort through accepts the
      // command and carries on — so saying "stopped" and then showing a running test again on the next
      // read is what makes a reader think the app is broken rather than the bridge.
      if (res && res.stopped === false) {
        toast('The drive accepted the abort but is still reporting a test in progress. Some USB ' +
          'bridges do not pass the command through, and the drive will finish on its own.', 'error');
      } else {
        toast((res && res.note) || 'Test stopped', 'good');
      }
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
   *
   * `quiet` is for the read this card does when it mounts. That one runs on every visit to the page,
   * so a drive whose self-test state cannot be read would raise a toast every time somebody looked at
   * it — and the stored logs the card is already drawn from are a better answer than an error nobody
   * can act on. A read the *user* asked for (a button, a poll they started) stays loud.
   */
  async function read({ quiet = false } = {}) {
    let state;
    try {
      state = await api.selfTest(device.serial);
    } catch (err) {
      if (!quiet) {
        toast(`Could not read the self-test state: ${err.message}`, 'error');
        showIdle();
      }
      return;
    }

    // The drive's own log, read live. The panel above is drawn from the *last scan*, so a result
    // that has just been written exists only here — and tying it to the big Scan button would make
    // the reader re-read the whole fleet to see the outcome of one test.
    const live = state && state.logs ? state.logs : null;
    if (live) {
      // The history is redrawn from the live log, which is where a just-finished result exists — the
      // stored copy the card was built from predates it.
      drawHistory(live);
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
    const now = Date.now();
    if (dueAt === null) dueAt = now + pollMs();
    if (timer !== null) clearTimeout(timer);
    timer = setTimeout(tick, Math.max(0, dueAt - now));
  }

  async function tick() {
    timer = null;
    // The cycle this deadline belonged to is over; the read below arms the next one if the drive is
    // still testing. Cleared before the connected check so a card that was navigated away from leaves
    // nothing pending for the next visit to inherit.
    dueAt = null;
    // Navigating away detaches the card. A timer that outlived it would go on reading the drive
    // for a panel nobody can see, which is exactly what this feature must not do.
    if (card.isConnected === false) return;
    await read();
  }

  // Paint from the stored logs first, so the card is never blank while the read below is in flight.
  const stored = logs && Array.isArray(logs.self_test_entries)
    ? logs.self_test_entries.find((e) => e.remaining !== undefined)
    : null;
  if (stored) {
    wasRunning = true;
    showRunning(stored);
  } else {
    showIdle();
  }

  // Then read the drive, once, on mount.
  //
  // The stored copy is from the *last scan*, and a test started since then is not in it — so a card
  // that only trusted it reported "no test running" for a drive that was mid-test, and could then ask
  // that drive to start a second test it would refuse.
  //
  // **Always, including when a log this app does not read is present.** `smartctl -x` reads every log,
  // so only the *parser* is limited, and a read that comes back with nothing is not a wasted request —
  // it is the correction. The card paints a running entry from the last scan *before* this read starts,
  // so for a drive that finished its test since that scan the live read is the only thing that takes
  // the bar down. The read is not silent about the limit either: `showIdle` names the unread log.
  //
  // One read, not a poll: `read()` starts the timer itself when the drive turns out to be testing, so
  // visiting an idle drive costs exactly one request and nothing after it (the zero-idle-cost
  // invariant, `DESIGN.md` §2.1).
  void read({ quiet: true });

  return card;
}

/**
 * What a self-test costs, before anyone presses anything.
 *
 * The estimates are the drive's own, from its capability page, and they now ride on the buttons
 * rather than in this sentence — the figure answers "how long will this take?" and that question is
 * asked with the cursor over a button, not while reading a paragraph. What is left here is the part
 * that is not about either button: that nothing happens unless you ask, and what that means for the
 * drive while it happens.
 */
function idleNote(shortMinutes, extendedMinutes) {
  const cost = shortMinutes || extendedMinutes
    ? 'The drive stays usable throughout.'
    : 'This drive does not say how long its tests take, so the buttons cannot show an estimate.';
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

/**
 * The same estimate, compressed for a button.
 *
 * "2 min" rather than "2 minutes": a button is a label, not a sentence, and the long form pushed the
 * two buttons far enough apart that comparing them got harder rather than easier.
 */
function fmtShort(minutes) {
  const n = Number(minutes);
  if (!Number.isFinite(n) || n <= 0) return '';
  if (n < 60) return `${fmtInt(n)} min`;
  const hours = Math.floor(n / 60);
  const rest = n % 60;
  return rest === 0 ? `${hours} h` : `${hours} h ${rest} min`;
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

