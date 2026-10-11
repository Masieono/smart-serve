# Smart-Serve — S.M.A.R.T. Interpretation (CrystalDiskInfo-parity)

This document defines exactly how raw `smartctl --json` output becomes the
human-friendly verdicts and readouts in the UI. It is the normative reference for
the interpreter; the goal is CrystalDiskInfo-grade clarity.

## 1. Two device families, two paths

| Family | Detected by | Health source | Wear source |
|---|---|---|---|
| ATA/SATA/USB (SAT) | `device.type == "ata"` (or sat transport) | `ata_smart_status` + attribute table (below) | SSD: attribute 231 (SSD Life Left) / 174 (Host/Dev Sleep?) vendor-specific |
| NVMe (incl. USB NVMe) | `device.type == "nvme"` | `nvme_smart_health_information_log` | `percentage_used`, `available_spare` |

## 2. ATA attribute table (the CrystalDiskInfo set)

Interpreted attributes — anything not listed is displayed raw, without verdict logic:

| ID | Name | Role in verdict | Interpretation |
|---|---|---|---|
| 5 | Reallocated Sectors Count | count-type | higher raw = worse; >0 ⇒ caution |
| 9 | Power-On Hours | informational | raw → hours (packed-value rule below) |
| 10 | Spin Retry Count | count-type | >0 ⇒ caution |
| 12 | Power Cycle Count | informational | raw |
| 187 | Reported Uncorrectable Errors | count-type | >0 ⇒ caution |
| 194 (fallback 190) | Temperature | value-type | min/value/max parsed from raw |
| 196 | Reallocation Event Count | count-type | >0 ⇒ caution |
| 197 | Current Pending Sector Count | count-type | >0 ⇒ caution; ≥10 ⇒ bad |
| 198 | Offline Uncorrectable | count-type | >0 ⇒ caution; ≥10 ⇒ bad |
| 199 | UDMA CRC Error Count | count-type | >0 ⇒ caution (usually cable, not drive — rationale says so) |

## 3. Raw-value packed-word rule (the classic CrystalDiskInfo gotcha)

Many vendors (Maxtor, Samsung, HGST, etc.) pack **three 16-bit counters** into the
48-bit raw value; CrystalDiskInfo displays them as "0 0 5". Smart-Serve applies the
same unpacking heuristic for count-type attributes:

1. Let `raw` be the decimal raw value.
2. Candidates: `raw`, `raw >> 16`, `raw >> 32`.
3. Choose the smallest **non-zero** candidate that is < 65,536; if all are zero,
   the count is 0.

Rationale: the intended counter almost always lives in one word; nonsense packed
values are astronomically large when misread. Attribute 9 (power-on hours) uses
the same rule (a drive showing 3 billion hours is misread).

## 4. Verdict algorithm (normative)

```
verdict(device):
  if smartctl reports !smart_status.passed:            → BAD  ("SMART self-assessment failed")

  ATA (SATA/USB-SAT):
    if any of ID5/197/198 count >= 10:                 → BAD  ("<n> reallocated|pending|uncorrectable sectors")
    elif any of ID5/10/187/196/197/198/199 count > 0: → CAUTION ("<n> pending sectors", "CRC errors — check cable", …)
    elif temperature >= temp_threshold (default 55°C):→ CAUTION ("temperature <t>°C")
    else:                                             → GOOD

  NVMe:
    if critical_warning != 0:                          → BAD  ("NVMe critical warning flag 0x<h>")
    elif available_spare <= available_spare_threshold: → BAD  ("spare blocks below threshold")
    elif percentage_used >= 90:                       → CAUTION ("<p>% lifetime used")
    elif temperature >= temp_threshold:               → CAUTION ("temperature <t>°C")
    else:                                             → GOOD
```

All thresholds in this algorithm (`temp_threshold`, the `10`-count Bad thresholds,
and `warn_on_any_nonzero`) are **configurable in the Settings UI** with these
defaults (`DATA_MODEL.md`, `ADR-009`); the values shown are the shipped defaults.

See §8 for the derived 0–100 **health score** shown alongside the verdict.

Rules:

- The verdict is computed **fresh each scan** from that scan's data; the previous
  stored verdict is used only for the `health_change` notification transition.
- `health_rationale` always carries the human sentence shown in parentheses —
  the UI never shows a bare "Caution" without a reason.
- Temperature, hours, cycles, and all attributes are always displayed regardless
  of verdict — verdict logic affects only the badge and notifications.
- A device with `smart_available: false` shows verdict `unknown` with the capture
  error; it is never notified about except `device_appeared`.

## 5. Remaining-longevity estimation (Phase 2 — specified now)

**Honesty first:** S.M.A.R.T. provides *no* true time-to-death metric. Any
"remaining life" number is an estimate with model-specific error bars. The UI will
always present it as "estimated, low/medium confidence" and show the underlying
inputs. The formulas below are normative for the Phase 2 implementation.

### 5.1 NVMe (medium confidence — manufacturer-supplied wear data)

Inputs: `percentage_used` (u), `power_on_hours` (h), `data_units_written`
(×512 KB). If a drive model's rated TBW is known (bundled spec table, Phase 2),
prefer TBW math; otherwise linear-wear projection:

```
remaining_hours ≈ h × (100 − u) / u            (only when u > 5)
remaining_pct  = 100 − u
confidence     = medium
```

Caveat displayed: assumes wear proportional to power-on time; bursty-write drives
degrade faster than hours suggest (the TBW path mitigates this when available).

### 5.2 SATA SSD (low-to-medium confidence)

- Use attribute 231 (SSD Life Left / Media Wear) when the drive exposes it → same
  linear projection as NVMe.
- Some vendors expose wear in attribute 174 or E7-model counters — the bundled
  spec table lists per-vendor attribute choices (Phase 2 data entry).
- Otherwise: no wear estimate — show "unknown (drive does not report wear)".
  **Never guess.**

### 5.3 HDD (low confidence — trend-based only)

HDDs report no wear metric. The only defensible estimate is failure-signal
trending over the snapshot history:

```
rate_r = Δ(reallocated + pending) / Δ(days)        over the retention window
if rate_r > 0:
  projected_days_to_caution ≈ (10 − current_count) / rate_r
  confidence = low                                (failure prediction is
                                                    notoriously unreliable)
else:
  longevity = "no degradation trend observed"      (no number shown)
```

The UI must render this as a *degradation trend* chart with the projection marked
as extrapolation — not a countdown. The Backblaze-drive-stats literature is the
honest framing: SMART is a weak predictor until it fails outright; reallocated/
pending sector *growth* is the strongest available signal.

### 5.4 What we will never do

- Show a countdown ("dead in 84 days") without a confidence label.
- Estimate longevity for devices that report no wear/trend data.
- Let the estimate influence notifications — only verdict *transitions* notify
  (`NOTIFICATIONS.md`).

## 6. Temperature

- ATA: attribute 194 (Temperature_Celsius); fallback 190 (Airflow_Temperature).
  Raw may be packed (min/value/max in words) — the packed-word rule (Section 3)
  applies; the middle word is the current value when packing is detected.
- NVMe: `temperature` (+ `temperature_sensors[]` when present).
- Threshold from settings (`temperature_threshold_c`, default 55 °C) — affects
  verdict (caution) and the `temperature_high` notification.

## 7. Testing strategy for the interpreter

- **Fixtures**: captured real `smartctl -x --json` outputs (one per family: healthy
  ATA HDD, failing HDD with 197>0, SSD, NVMe healthy, NVMe spare-low, USB-SAT
  drive, packed-raw Samsung, unreadable USB bridge) stored under `testdata/`;
  interpreter tests assert verdicts + rationales against these fixtures.
- **Fuzzing**: the packed-word rule and verdict thresholds get unit-test coverage
  across synthetic raw values (0, 1, 0xFFFF0000, 0xFFFFFFFFFFFF, …).
- CrystalDiskInfo parity checklist: for each fixture, the verdict and displayed
  attribute values must match what CrystalDiskInfo reports for the same drive
  (validated manually during development; documented per-fixture).

## 8. Health score (0–100)

The **verdict** (Good/Caution/Bad) is authoritative and drives alerts. The **health
score** is a supplementary continuous metric for at-a-glance reading — a single
number and grade that quantifies *how* healthy a drive is. It is deterministic,
recomputed each scan, and always accompanied by a **penalty breakdown** so the user
can see exactly why the number is what it is.

### 8.1 Bands (kept consistent with the verdict)

The score is clamped into the band matching the verdict, so the two never
contradict each other:

| Verdict | Score band | Grade label |
|---|---|---|
| Good | 75–100 | Excellent (90–100) / Good (75–89) |
| Caution | 40–74 | Fair |
| Bad | 0–39 | Poor (25–39) / Critical (0–24) |
| Unknown (`smart_available: false`) | — (no score shown) | — |

### 8.2 Penalty model

Start at `100`, subtract penalties, then clamp to the verdict band.

**ATA / SATA / USB-SAT** (raw values after the packed-word rule, §3):

| Factor | Penalty | Cap |
|---|---|---|
| Reallocated sectors (5) | 1 per sector | 30 |
| Current pending sectors (197) | 2 per sector | 30 |
| Offline uncorrectable (198) | 2 per sector | 30 |
| Reallocation events (196) | 0.5 per event | 10 |
| UDMA CRC errors (199) | 0.5 per error | 10 |
| Spin retry count (10) | 3 per retry | 15 |
| Temperature above threshold | 1 per °C | 10 |

**NVMe:**

| Factor | Penalty |
|---|---|
| `critical_warning != 0` | force Bad band; 10 per flag bit (cap 40) |
| `percentage_used` | 0.4 per % above 5 (cap 45) |
| `available_spare` below `available_spare_threshold` | force Bad band |
| `available_spare` below 100% | 0.3 per missing % (cap 30) |

**Hard overrides:** `smart_status.passed == false` (ATA) or a critical warning (NVMe)
forces the Bad band and caps the score at 10.

### 8.3 Worked examples

- Healthy drive, 0 penalties → **100 (Excellent)**.
- 1 pending sector → base 98, verdict Caution → clamped to 74 → **74 (Fair)**.
- 10 pending sectors → base 80, verdict Bad → clamped to 39 → **39 (Poor)**.
- CRC errors only (cable issue) → base 90, verdict Caution → **74 (Fair)**, breakdown
  names "UDMA CRC errors — check cable".
- NVMe at 60% used → penalty 22 → base 78, verdict Good → **78 (Good)**.
- SMART self-assessment failed → **≤10 (Critical)**.

### 8.4 Transparency requirement

The UI **must** render the breakdown (which factors reduced the score and by how
much) on hover/tap of the gauge (`UI.md` §6). A score without its rationale is
forbidden — it would be exactly the "mystery number" this design avoids.

### 8.5 Phase 2 refinement

The trend-aware score (Phase 2) additionally penalises a *worsening* direction in
the snapshot history (e.g. growing pending-sector count) even while the current
verdict is still Good — an early-warning signal beyond CrystalDiskInfo.


