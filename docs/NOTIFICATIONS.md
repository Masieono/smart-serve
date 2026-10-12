# Smart-Serve — Notifications

> **Status: implemented (MVP).** The scheduler lives in `internal/sched` and the
> delivery pipeline in `internal/notify`: transition detection, ntfy and webhook
> senders, squelching, the delivery log (`GET /api/notifications`), and
> `POST /api/channels/{id}/test`. Phase 2 adapters (Gotify, Telegram, Discord,
> SMTP) implement the same `Sender` interface and change nothing else.

## Event catalog

All notification-worthy events are emitted **after** a scan completes (or fails).
There is no separate monitoring loop — this is the scan executor publishing to the
notifier bus.

| Event type | Trigger | Default priority |
|---|---|---|
| `health_change` | A device's verdict differs from its previous stored verdict (e.g. good→caution) | high |
| `temperature_high` | Any device above `temperature_threshold_c` (default 55 °C) | medium |
| `device_disappeared` | Device in DB, absent from lsblk in this scan (fired once per disappearance) | medium |
| `device_appeared` | Device seen for the first time, or reappearing after `disappeared` | low |
| `scan_failure` | A scheduled scan (not a manual one) failed entirely (lsblk/smartctl errors) | medium |

Priority influences formatting (ntfy priority header, webhook payload field) and
squelch defaults — not delivery: every channel subscribed to the event gets it.

## Channels (MVP)

### ntfy (first-class)

Config schema:

```json
{
  "type": "ntfy",
  "label": "server alerts",
  "config": {
    "server": "https://ntfy.sh",        // or self-hosted URL
    "topic": "my-smart-alerts",
    "token": "",                         // optional access token
    "priority_mapping": "default"        // default maps low/medium/high → 3/4/5
  }
}
```

Delivery: single HTTP POST per event to `<server>/<topic>` with the event as the
message body. Markdown formatting for multi-line drive summaries. Example:

```
⚠️ Drive health changed: boot (Samsung SSD 870 EVO)
good → caution
5 pending sectors — SMART attributes 197
Serial S5GXNX0R512345J, temp 41°C, last scan 03:00
```

### Generic webhook

Config schema:

```json
{
  "type": "webhook",
  "config": {
    "url": "https://example.com/hook",
    "method": "POST",
    "headers": { "Authorization": "Bearer ..." },
    "template": ""       // optional Go text/template override
  }
}
```

Default payload (JSON):

```json
{
  "event": "health_change",
  "priority": "high",
  "device_serial": "S5GXNX0R512345J",
  "device_name": "boot",
  "device_model": "Samsung SSD 870 EVO 1TB",
  "old_health": "good",
  "new_health": "caution",
  "rationale": "5 pending sectors",
  "temperature_c": 41,
  "scan_ts": 1730000000000,
  "smartserve_version": "1.0.0"
}
```

Delivery: 10 s timeout, 2 retries with 5 s backoff on 5xx/network errors; non-2xx
final failure logged to `notification_log` as `failed` (never blocks the scan).

## Squelching (dedupe)

Rule: for each `(channel, event_key)` pair, where `event_key` is:

- `health_change:<serial>:<new_health>` — refires only if health changes again
  (good→caution→bad fires twice; caution persists quietly).
- `temperature_high:<serial>` — refires only after `squelch_seconds`
  (default 3600) while the condition persists.
- `device_disappeared:<serial>` / `device_appeared:<serial>` — one-shot per
  appearance cycle by construction.
- `scan_failure:<schedule_id>` — one per scan attempt (this one is allowed to
  repeat; a broken scanner must be loud).

Squelched sends are recorded as `status: squelched` in `notification_log` for
transparency. Test events (`POST /api/channels/{id}/test`) bypass squelching.

**As implemented**, only `temperature_high` consults a time window. Everything
else carries a zero window and is therefore never squelched, because for those
events the dedupe is structural rather than temporal:

- A `health_change` is only generated when the stored verdict actually differs
  from the previous one (ADR-008), so a persistent bad state produces nothing to
  squelch. Applying a window on top would be actively wrong: a drive that
  recovers and degrades again is two real events, and the key — which encodes the
  *new* verdict — would otherwise silence the second one.
- `device_disappeared` / `device_appeared` fire on the transition itself, so
  repeating them is impossible by construction.
- `scan_failure` is meant to repeat: every failed scheduled scan is news, and a
  scanner broken for a week must not go quiet after the first hour.

The event key is still recorded for all of them — it is what makes the delivery
log readable, and what an acknowledgement would key on in Phase 2.

## Suppression: acknowledgement & maintenance windows (Phase 2)

Two additional suppression layers sit on top of squelching:

- **Acknowledgement** — a user can acknowledge a specific `event_key`; it is then
  muted until the underlying condition changes or the acknowledgement expires.
  The verdict is unchanged — this only silences the alert (`FEATURES.md` B7).
- **Maintenance windows** — a time range (optionally filtered to certain devices)
  during which *no* notifications fire (e.g. while rebuilding an array)
  (`FEATURES.md` B8). Windows are computed as a next-boundary sleep, preserving the
  zero-idle-cost invariant (`FEATURES.md` §E).

## Delivery mechanics

- Sequential sends within the post-scan notifier pass; total notifier budget
  capped (default 30 s) so a hung channel can't stall the next scheduled scan.
- All delivery outcomes land in `notification_log` for debugging and for a
  "recent notifications" view in Settings (Phase 2 UI surface).

## Post-scan hand-off

The notifier is a **post-scan hook**, not a second pipeline. `scan.Manager` runs
it after the scan is committed, and hands it two device-state maps: the stored
state from *before* the scan and the state *after* it, both keyed by serial.

That pair is what makes transition detection fall out naturally. Because an absent
drive is flagged `is_missing` rather than deleted (`DATA_MODEL.md`), a state that
is `Missing` after the scan has just disappeared, and one that is present after
being `Missing` has just reappeared — no separate bookkeeping, no extra table.

Two deliberate quietenings:

- **The first scan announces nothing.** With no prior state every drive is
  technically "new", and reporting a fresh install as N discoveries is noise.
- **A missing drive's temperature is ignored.** The stored value is the last one
  ever seen; alerting on it would report history as if it were now.

The hook is wrapped so a panic inside it is contained and published as a
`notify_error` SSE event. A notification bug must never take down the scan
pipeline.

## Delivery outcomes

| Status | Meaning |
|---|---|
| `sent` | the destination returned 2xx |
| `failed` | it returned 4xx (permanent — a config problem, so no retry) or 5xx/network after two retries |
| `squelched` | inside a squelch window; recorded so the log explains the silence |

Retries use a 10 s request timeout with two retries and a 5 s backoff on 5xx or
network errors. A 4xx is never retried — repeating a rejected request only burns
the budget. The dispatcher's 30 s budget caps the worst case, so a dead endpoint
cannot delay the next scheduled scan.

The log write itself deliberately survives context cancellation: the budget
expiring must not erase the evidence of what happened.

## Phase 2 adapters

`gotify`, `telegram`, `discord`, `smtp` — each an adapter implementing the same
`Notifier` interface: `Send(ctx, Event, DeviceContext) (Outcome, error)`. Adding a
channel never touches the scan pipeline.
