# Smart-Serve — Notifications

> **Status: implemented.** The scheduler lives in `internal/sched` and the delivery
> pipeline in `internal/notify`: transition detection, six channel types (ntfy,
> Gotify, Telegram, Discord, SMTP, generic webhook), squelching, maintenance
> windows, the delivery log (`GET /api/notifications`), and
> `POST /api/channels/{id}/test`.
>
> Acknowledgement (`FEATURES.md` B7) is **deferred** — see §Suppression.

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

## Channels

Every channel type implements the same `Sender` interface
(`Send(ctx, cfg, Event) error`), so adding a provider never touches the scan
pipeline. `GET /api/notifications` reports the registered set as `channel_types`,
and the UI's editor offers exactly that list — a build without a provider cannot
offer it.

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

### Gotify

```json
{ "type": "gotify", "config": { "server": "https://gotify.example.com", "token": "app-token" } }
```

Single `POST <server>/message?token=<token>` with `{title, message, priority}`.
Priority maps onto Gotify's 0–10 scale: low → 3, medium → 6, high → 9. The server
URL must be `http(s)`, so a bare hostname is rejected when the channel is saved
rather than failing on the first alert.

### Telegram

```json
{
  "type": "telegram",
  "config": {
    "token": "123456:ABC…", "chat_id": "-1001234567890",
    "thread_id": "", "api_base": ""
  }
}
```

`POST <api_base>/bot<token>/sendMessage`. `api_base` defaults to
`https://api.telegram.org` and exists so a self-hosted Bot API server works (and so
the transport is testable without reaching Telegram). `thread_id` is sent as a
number, because the Bot API rejects a string.

### Discord

```json
{ "type": "discord", "config": { "webhook_url": "https://discord.com/api/webhooks/…", "username": "" } }
```

Posts an embed rather than plain text: the coloured bar carries the priority
(green/amber/red, matching the app's health colours), and the fields stay aligned in
Discord's own layout.

### Email (SMTP)

```json
{
  "type": "smtp",
  "config": {
    "host": "smtp.example.com", "port": "587",
    "username": "", "password": "",
    "from": "smart-serve@example.com",
    "to": "ops@example.com, me@example.com"
  }
}
```

The only adapter that is not an HTTP POST, but it shares the retry rules and the
transient classification. Port 465 is treated as implicit TLS (the socket is
encrypted from the first byte) and everything else as plaintext-then-STARTTLS —
getting that backwards is the usual reason an SMTP config fails with no useful
error. A blank port means 587; a port that is present but unparseable is rejected,
because a typo in that field must not quietly send mail somewhere else.

The subject is RFC 2047-encoded (it carries a device model) and the body is
quoted-printable (it carries emoji) — a raw 8-bit body is not legal and some servers
reject it outright.

### Adding a provider

1. Implement `Sender` (`Type`, `Send`) and, for structured config, `ConfigValidator`.
2. Register it in `cmd/smartserve/main.go`.
3. Add its field spec to `CHANNEL_TYPES` in
   `internal/webui/assets/js/views/notifications.js` — the editor is data-driven, so
   the form, its required-field validation and its config payload all follow.

Two tests pin the two ends together: `TestRegistryShipsTheExpectedChannelTypes`
(Go) and `the editor offers every registered channel type` (JS) assert the same
list, so a provider cannot be half-added.

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
transparency. Test events bypass squelching — both `POST /api/channels/{id}/test` and
the body-described `POST /api/channels/test` the editor uses to test unsaved settings.
The latter writes no log row at all: a draft has no id, so there is no stored channel to
attribute a delivery to, and testing a draft must not persist anything.

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

## Suppression

Three layers sit on top of the event catalog. Each one records *why* it was quiet in
`notification_log`, because a silence with no explanation is indistinguishable from a
bug.

### Squelching (implemented)

See above — a per-`(channel, event_key)` time window.

### Maintenance windows (implemented)

A window silences **everything** for the drives it covers: an array rebuild, a
migration, a chassis being moved. It exists so that a planned event does not require
disabling a channel and remembering to re-enable it.

A window is either **one-off** or **recurring**:

```json
[
  { "label": "array rebuild", "recurrence": "once",
    "starts_at": 1760000000000, "ends_at": 1760014400000,
    "serials": [] },

  { "label": "nightly backup", "recurrence": "daily",
    "start_minute": 120, "end_minute": 240, "days": [] },

  { "label": "weekend maintenance", "recurrence": "weekly",
    "start_minute": 1380, "end_minute": 120, "days": [1, 5] }
]
```

- **One-off** windows carry absolute instants in unix milliseconds.
- **Recurring** windows carry minutes past midnight. They are read on the **server's**
  clock — the same one the scheduler evaluates cron against — so "02:00" means 02:00
  where the app runs, not where the browser is. `days` holds weekday numbers
  (0 = Sunday) and is only used by `weekly`.
- A window **crossing midnight** is supported (23:00–02:00). The morning half belongs
  to the day the window started on, so a weekly `Friday 23:00–02:00` window covers
  Saturday's small hours.
- Stored as the `maintenance_windows` setting (`DATA_MODEL.md`) and edited from the
  Notifications page, where the drives are picked from a list rather than typed.
  There is no table and no endpoint: a window is a configuration value, not a
  resource with a lifecycle.
- `serials: []` covers every drive; a populated list covers only those, matched
  case-insensitively because serials are hand-typed. An event with no serial (a
  `scan_failure`) is not covered by a scoped window.
- The end is **exclusive**, so a window ending at 06:00 does not silence 06:00.
- **Zero idle cost.** Windows are evaluated at dispatch time against the clock.
  Nothing sleeps until one opens or closes (`FEATURES.md` §E).
- A malformed setting parses as *no windows*. Failing closed would let a typo in a
  settings value silence every alert on the host, which is the worse failure. A
  window whose end is not after its start, or a recurring window with no days, is
  dropped the same way.
- **Both** test endpoints ignore windows — `POST /api/channels/{id}/test` and the
  body-described `POST /api/channels/test`: an operator pressing Test is asking "does
  this channel work right now?".
- The UI labels a recurring window by its schedule rather than "active", because the
  browser's clock is not necessarily the server's.

### Acknowledgement (deferred)

A user would acknowledge a specific `event_key`, muting it until the underlying
condition changes or the acknowledgement expires, leaving the verdict untouched
(`FEATURES.md` B7).

Deferred deliberately rather than dropped: it belongs in the app, but at single-host
scale the state it needs (`alert_acknowledgements`, with expiry and "has the
condition changed since?" bookkeeping) costs more to maintain than the noise it
removes. The event key already exists and is already recorded, which is the part that
would have been expensive to retrofit.

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
| `squelched` | inside a squelch window, or inside a maintenance window; recorded so the log explains the silence |

A maintenance-window suppression names the window (`maintenance window: array
rebuild`), so an operator can tell a deliberate silence from a squelch timer or a
delivery failure.

Retries use a 10 s request timeout with two retries and a 5 s backoff on 5xx or
network errors. A 4xx is never retried — repeating a rejected request only burns
the budget. The dispatcher's 30 s budget caps the worst case, so a dead endpoint
cannot delay the next scheduled scan.

The log write itself deliberately survives context cancellation: the budget
expiring must not erase the evidence of what happened.

## Adapters

`ntfy`, `gotify`, `telegram`, `discord`, `smtp` and `webhook` are implemented
(`internal/notify/adapters.go`, `smtp.go`). Each is a small `Sender`; the dispatcher
knows nothing about any of them.
