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
| `self_test_result` | A drive's self-test log holds a finished result it did not hold at the last scan (fired once per result) | low — **high** when the result failed |
| `scan_success` | A scheduled scan completed **and found nothing to report**. A heartbeat, not an alert — **opt-in**, see below | low |

Priority influences formatting (ntfy priority header, webhook payload field) and
squelch defaults — not delivery: every channel subscribed to the event gets it.

### "Everything" and the one exception

A channel with no subscriptions ticked receives **every** event. That default is about
exceptions — a verdict changed, a drive vanished, a scan failed — and each of those is
news precisely because it is not what normally happens.

`scan_success` is not that. It is a periodic all-clear, so on a 15-minute schedule it would
mean 96 messages a day. Adding it to the catalog therefore does *not* put it in
"everything": it has to be ticked explicitly (`optInEvents` in `internal/notify/notify.go`).
Otherwise the first thing a user would experience after an upgrade is a burst of messages
they never asked for, and the second thing they would do is turn notifications off.

An explicit `"*"` subscription still means everything, including the heartbeat.

#### What "found nothing to report" means exactly

The heartbeat fires only when the scan produced **no other events**. It is the complement
of the alert stream rather than an addition to it: subscribe to it and you hear from every
scheduled scan exactly once — either the alerts, or *"3 drives scanned in 4.2s"*.

Gating on an empty event list rather than on "every drive is good" is what makes that
exact. A health check would never fire for a drive with no S.M.A.R.T. (verdict `unknown`)
or for one that has been failing for a week (no transition, so no alert) — and both of
those are precisely when a reassuring heartbeat is wanted. For the same reason the body
says nothing about per-device state: it reports how much was looked at and how long it
took, and the alerts do the rest.

A manual scan never produces a heartbeat: whoever pressed the button is watching the
screen, and telling them the thing they just did happened is noise. Same reasoning as
`scan_failure`, which is also scheduled-only.

### Self-test results, and why they are not opt-in

`self_test_result` is the one event that reports *good* news, and it is deliberately not gated the
way `scan_success` is. The difference is what causes it. The heartbeat fires on a timer, so
subscribing to it means accepting a message per schedule whether or not anything happened. A
self-test result only exists because somebody pressed the button (`A19`), so it can never arrive
unsolicited. It belongs in "everything".

**One event type, not two.** `self_test_passed` and `self_test_failed` would be the obvious split,
but the outcome is not what a subscription is choosing between — you either want to hear about your
self-tests or you do not. So the outcome rides in the payload (`self_test_passed`) and sets the
priority: **high** on a failure, **low** otherwise. A failure is the entire reason to run a test,
and it should not arrive looking like a footnote.

**Identity is the drive's type and power-on hours.** A self-test log entry carries no id of its
own, so "this is a result I have not reported" is decided by comparing the newest *finished*
entry's type and hours against the one stored from the last scan. Two identical tests in the same
power-on hour are therefore indistinguishable and the second will not notify. That is a rare miss,
and the alternative is an id the drive does not provide. The verdict is deliberately *not* part of
the identity: the same test re-read with a different verdict is the same test read twice.

**A baseline has to exist before it can change.** The stored value tells a log that was read and
found empty apart from a row the app has never read, and only the first is something a new result
can differ from. Without that distinction, the first scan after an upgrade would announce every
result every drive already held — the same reasoning as "the first scan announces nothing" above,
and the reason those columns are nullable rather than defaulting to empty (migration 0007,
`DATA_MODEL.md`).

**A failed read preserves the baseline.** When smartctl cannot read a drive, the scan records
nothing about its self-test log rather than recording "no result". Otherwise a single transient
failure would make the next successful scan report a test that finished days ago as news.

## Channels

Every channel type implements the same `Sender` interface
(`Send(ctx, cfg, Event) error`), so adding a provider never touches the scan
pipeline. `GET /api/notifications` reports the registered set as `channel_types`,
and the UI's editor offers exactly that list — a build without a provider cannot
offer it.

### Supplying fields from the environment

Every string field of a channel can come from the environment instead of the stored config,
named after the channel's **label**:

```
SMARTSERVE_CHANNEL_<LABEL>_<FIELD>            # the value
SMARTSERVE_CHANNEL_<LABEL>_<FIELD>_FILE       # a file to read the value from
```

Precedence is **environment > file > database**, resolved in `internal/notify/secrets.go` at the
moment a config is decoded — so a dispatch and a **Test** see the same values. This is how a
token stays out of the `/data` volume, and it is deliberately *not* limited to the fields the
editor marks as secret: the same mechanism points a channel at a different server per
environment.

Two things worth knowing when editing a channel:

- **Renaming a channel changes which variables it reads.** The editor prints the expected
  prefix and badges every field the deployment supplies, so the mismatch is visible rather
  than silent.
- **A `_FILE` that cannot be read is an error, not a fallback.** The delivery is recorded as
  failed with the reason. It never quietly uses the stored value, which would send a stale
  token while you believed you had rotated it.

`DEPLOYMENT.md` §Secrets has the compose examples; `ADR-022` has the reasoning.

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

A **test** payload (`POST /api/channels/test`, or `/api/channels/{id}/test`) has the
same shape plus `"test": true`, and the synthetic device is named `Smart-Serve test` /
`synthetic`. The flag is on the wire on purpose: an automation watching this webhook has
to be able to tell a test from a real alert and decline to act on it, because a test that
looks exactly like a fault can otherwise trip real paging. The human-readable message is
replaced outright for a test — title `Test from Smart-Serve`, body a short note that the
channel is working plus the version, so an operator can tell which instance sent it.

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

## Verifying a channel

Every adapter is unit-tested against a local stub (`internal/notify/adapters_test.go`),
which proves the request shape, the auth header, the priority mapping and the retry
classification. It cannot prove that *your* credentials reach *your* server — that needs
the real thing, and it is the one part of a notification setup that cannot be tested
from here.

**Use the editor's Test button.** It sends one event through the settings *as they stand
on screen*, saved or not, and does not close the dialog or write anything — so a bad
token can be corrected and retried in place. The API returns the destination's own error
verbatim, which is the fastest way to tell a bad token from a bad URL. (A draft test is
not recorded in the delivery log, because a draft has no stored channel to attribute it
to; saved channels are.)

Each recipe below is: where the credential comes from, what to paste, and what the
common failure looks like.

### ntfy

1. Pick a topic name that is hard to guess — anyone who knows it can publish to it on
   the public server.
2. Paste server (`https://ntfy.sh`, or your own), topic, and an access token if your
   server requires one.
3. Subscribe to the topic in the app or the web UI, then press **Test**.

*Failure looks like:* `403` for a bad or missing token, `404` for a topic your
self-hosted server does not allow.

### Gotify

1. On your Gotify server: **Apps → Create Application**. Copy the **app token**.
2. Paste server (`https://gotify.example.com` — the scheme is required) and the token.
3. Press **Test**; the message should appear with its priority colour.

*Failure looks like:* `401` is almost always the **client** token pasted instead of the
app token. They look alike and both are long strings; a client token authenticates a
*client*, not an application, and cannot post.

### Telegram

1. Message **@BotFather** → `/newbot` → copy the bot token.
2. **Send your new bot a message** (or add it to a group). A bot cannot start a
   conversation, so its `chat_id` does not exist until something talks to it first —
   this is the step people skip.
3. Read the id back: open `https://api.telegram.org/bot<token>/getUpdates` and find
   `chat.id`. A group id is negative; a forum topic's id goes in `thread_id`.
4. Paste token, `chat_id`, optional `thread_id`, and press **Test**.

*Failure looks like:* `400 Bad Request: chat not found` means step 2 was skipped.
`401 Unauthorized` means the token is wrong.

### Discord

1. Channel settings → **Integrations → Webhooks → New Webhook** → copy the webhook URL.
2. Paste it as `webhook_url`, optionally set a display name, and press **Test**.

*Failure looks like:* `401`/`404` for a revoked or mistyped webhook. A webhook URL is a
secret — anyone holding it can post to that channel.

### Email (SMTP)

1. Use your provider's **submission** settings, not its IMAP settings. Port `587` is
   STARTTLS, `465` is implicit TLS; the wrong one is the usual cause of a config that
   hangs or fails with no useful error.
2. Paste host, port, username and password, `from`, and one or more `to` addresses
   separated by commas. Leave the username blank for an unauthenticated relay.
3. Press **Test**.

*Failure looks like:* `535` for bad credentials — many providers need an **app
password** rather than the account password. A rejected `from` usually means the address
is not one the provider lets you send as. A TLS error against an internal server is
usually a self-signed certificate.

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
