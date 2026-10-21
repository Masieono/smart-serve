# The knowledge base format

`FEATURES.md` A21. This file describes the JSON document that `internal/knowledge` reads, validates
and matches. It is the whole interface: the table bundled with the binary, the copy fetched from a
URL, and the entries typed into Settings are **all this document**, so a contributor edits exactly the
shape the app reads.

The canonical copy lives at `internal/knowledge/base.json`, which is also the default refresh URL.
Editing that file and opening a pull request is the way to contribute a family; pointing the refresh
URL at your own copy is the way to keep your own.

## What the app does with it

- **Rated endurance** appears on the device page beside the figures it explains: how much the drive is
  rated for, how much it has written, and what fraction of the rating that is. It is only ever a
  denominator — the app does not move a verdict because a rating says so.
- **Advisories** appear as a card on the device page, and only when the advisory's firmware matches
  the firmware *this* drive reports. An advisory for a revision the drive is not running is noise, and
  noise is what makes people stop reading advisories.
- **Nothing else.** The knowledge base changes no verdict, no score and no alert. It is context, and
  the drive's own reported data is what the app reasons from.

## Precedence

Three layers, later wins:

1. The table bundled with the build.
2. A copy fetched from the refresh URL.
3. The entries you type in Settings.

An entry **replaces** the one for the same family rather than merging field by field. Field-level
merging would make it impossible to *remove* an advisory, and "I deleted it and it is still there" is
a bug report nobody can act on. So if you want to keep the bundled advisories and change only the
rating, copy the entry and edit the number.

## The document

```json
{
  "version": 1,
  "updated": "2026-10-20",
  "note": "Free text. A file found on its own explains itself.",
  "drives": [
    {
      "family": "Samsung SSD 870 EVO",
      "matches": ["WD40EFAX", "WD60EFAX"],
      "rated_tbw": 600,
      "rated_tbw_by_capacity_gb": { "250": 150, "500": 300, "1000": 600, "2000": 1200 },
      "rated_workload_tb_per_year": 180,
      "source": "Samsung 870 EVO datasheet, 2021",
      "notes": "Anything a reader should know that is not a number.",
      "advisories": [
        {
          "firmware": "SVT01B6Q, SVT02B6Q",
          "severity": "warning",
          "title": "One line saying what is wrong",
          "detail": "What it does, and what to do about it.",
          "url": "https://example.test/vendor-page"
        }
      ]
    }
  ]
}
```

### `version`

Required, and must be `1`. A document written for a later version is **refused** rather than
half-understood: an entry whose meaning changed is worse than a missing one.

### `family`

Required, and the entry's name: what the app shows, and the key the layers merge on. It is matched
against a drive's model and model family **case-insensitively as a substring**, because model strings
are messy — `"WDC WD40EFRX-68N32N0"` is a WD40EFRX, and many drives report no family at all.

The **longest** matching pattern wins, so a specific entry beats a general one whatever order the file
lists them in.

### `matches`

Optional. Extra patterns, for a series that shares a suffix rather than a name — the SMR Red line is
`WD20EFAX` through `WD80EFAX`, four characters at the end that no single `family` string can express.
When present, these *replace* `family` as the match patterns; `family` stays the name.

**Every pattern must be at least five characters.** A pattern short enough to match unrelated drives
is refused outright, because a wrong entry is worse than a missing one: it carries a number that looks
authoritative.

### `rated_tbw` and `rated_tbw_by_capacity_gb`

The endurance in terabytes written, flat or per capacity. Prefer the per-capacity table when the
vendor publishes one, because it usually varies: a 250 GB and a 4 TB drive in one family are rated
differently, and quoting the family's headline number for both would be wrong for one of them.

Keys are the **decimal gigabytes the drive reports** — 500, 512, 1000, 1024 — rounded. The app takes
the nearest bucket, accepts it within 10%, and treats within 2% as exact (so it can say "600 TBW"
rather than "about 600 TBW"). A capacity no bucket is close to gets **no figure**: a rating for a
different capacity is a number about a different drive, and inventing one is the failure this whole
table exists to avoid.

### `rated_workload_tb_per_year`

The rotating-drive equivalent: the workload the vendor rates the drive for. Separate from TBW because
they are different promises — one is a total, the other a rate.

### `source`

Where the figure came from. **Always set this.** The app shows it beside the number, because a figure
nobody can check is a figure nobody should believe.

### `advisories`

Each needs a `title` and a `severity` of `info`, `warning` or `critical`. `detail` is what it does and
what to do about it — a title alone tells a reader they have a problem and not what to do, which is
worse than saying nothing.

`firmware` names the affected revision, matched case-insensitively and exactly. A comma-separated list
covers several revisions, because a fix usually covers everything before it and listing them is
clearer than inventing a range syntax. **Leave it empty for a family-wide advisory** — that is how the
SMR Red entry is written, because the problem is the model rather than a revision.

A drive that reported no firmware **cannot match a firmware-specific advisory**: that would be
guessing at the one fact the advisory is about.

## Validation

Parsing is strict, and the source is why: this file arrives from a URL or a text box, and an entry
silently dropped for a typo looks *exactly* like the app not knowing a drive — the one failure a
knowledge base cannot afford, because it is indistinguishable from having no data at all.

So unknown fields are refused, and the editor shows the parse error verbatim beside the box you typed
it in. The message names the field and the entry:

```
drives[3] (My Drive 9000): rated_tbw_by_capacity_gb[1TB] is not a capacity in GB
```

## Refreshing

The refresh is a **button in Settings, never a schedule.** The project is built on costing nothing
while idle, and a knowledge base that quietly polled a URL would be the first thing to break that —
for data that changes a few times a year. The card shows how old the stored copy is, so you can decide
whether it is worth pressing.

A fetched document is **parsed before it is stored**, so a bad file leaves the previous layer in place
rather than replacing something the app can read with something it cannot.
