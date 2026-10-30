// Dashboard: device cards, health summary, scan button (docs/UI.md §7.1).
import { el, mount, fmtAgo, convertTemp, convertTempDelta, temperatureUnitName } from '../dom.js';
import { api } from '../api.js';
import { deviceCard, fleetTable, deviceTitle, countText, healthFilter, emptyState, skeletonCards, errorBox, transportLabel, toast, confirmDialog, openDialog } from '../components.js';
import { icon } from '../icons.js';
// Tag handling is shared with the notification editor's group scope (`FEATURES.md` B12), because
// "which groups are there" is one question and two answers to it is one answer too many.
import { fleetTags, tagList } from '../tags.js';

// Module-level so the choices survive navigating into a drive and back.
let filter = null;
let query = '';
let showMissing = false;
let transportFilter = '';
let sortKey = 'health';
let ascending = true;
let view = 'cards';
let grouped = false;
let benchOnly = false;

/**
 * The stored view preference (`cards` or `table`), applied before the first render.
 *
 * Unlike the filter and the sort beside it — which are deliberately module-only, so they survive a
 * trip into a drive and back but not a reload — this one is a *preference*, and the app already keeps
 * its display preferences (the theme, the temperature unit) in the database, so they survive a reload
 * and travel in the config export. An earlier version kept this in the module too, which meant a
 * reader who wanted the table got cards again on every refresh.
 */
export function setView(stored) {
  if (stored === 'cards' || stored === 'table') view = stored;
}

// Worst first. The rank is what makes "sort by risk" mean something: the server's own ORDER BY
// uses the same order, and this is the client's copy of it for the sorts below.
const HEALTH_RANK = { bad: 0, caution: 1, unknown: 2, good: 3 };

/**
 * The sort options, each with the direction that reads as "most important first".
 *
 * The default direction is per-field rather than one global toggle, because "first" means
 * different things: someone sorting by capacity wants the biggest drive at the top, and someone
 * sorting by name wants A. A single toggle makes the control feel broken on first use whichever
 * way it is set.
 */
const SORTS = [
  { key: 'health', label: 'Risk', ascending: true,
    value: (d) => HEALTH_RANK[d.health] ?? 9 },
  { key: 'name', label: 'Name', ascending: true, value: deviceTitle },
  { key: 'bay', label: 'Bay', ascending: true, value: (d) => d.bay || '' },
  { key: 'model', label: 'Model', ascending: true, value: (d) => d.model || '' },
  { key: 'capacity', label: 'Capacity', ascending: false, value: (d) => d.capacity_bytes ?? -1 },
  { key: 'temperature', label: 'Temperature', ascending: false, value: (d) => d.temperature_c ?? -1 },
  { key: 'power_on_hours', label: 'Age', ascending: false, value: (d) => d.power_on_hours ?? -1 },
  { key: 'last_scan', label: 'Last scan', ascending: false, value: (d) => d.last_scan || '' },
  { key: 'score', label: 'Score', ascending: true,
    // A drive with no score is not the best-scoring drive in the fleet. -1 means "not scored",
    // so it sorts last rather than first.
    value: (d) => (typeof d.score === 'number' && d.score >= 0 ? d.score : 999) },
  { key: 'transport', label: 'Transport', ascending: true, value: (d) => d.transport || '' },
];

/**
 * The order transports are listed in.
 *
 * A fixed order rather than alphabetical, so the control keeps its shape as drives come and go, and
 * in the order an operator would say them. Anything unrecognised sorts after these, by name.
 */
const TRANSPORT_ORDER = ['sata', 'nvme', 'sas', 'usb'];

function transportRank(value) {
  const at = TRANSPORT_ORDER.indexOf(String(value || '').toLowerCase());
  return at === -1 ? TRANSPORT_ORDER.length : at;
}

function nameOf(d) {
  return deviceTitle(d);
}

/** Compare two sort values: numbers numerically, strings case-insensitively. */
function cmp(a, b) {
  if (typeof a === 'number' && typeof b === 'number') return a - b;
  return String(a).localeCompare(String(b), undefined, { sensitivity: 'base' });
}

/**
 * Sort a copy of the list.
 *
 * Ties always break on name, ascending, so the order is stable and a rack of identical drives
 * does not shuffle between renders. That matters more than it sounds: a list that reorders itself
 * when nothing has changed makes you re-read it every time you look.
 */
function sortBy(list, key, ascending) {
  const spec = SORTS.find((s) => s.key === key) || SORTS[0];
  const dir = ascending ? 1 : -1;
  return [...list].sort((a, b) => {
    const byValue = cmp(spec.value(a), spec.value(b));
    return byValue !== 0 ? byValue * dir : cmp(nameOf(a), nameOf(b));
  });
}

/**
 * The replacement ranking (`FEATURES.md` A24).
 *
 * The dashboard already sorts by today's severity. This answers a different question — "which one
 * do I buy this month?" — which is about direction and remaining life as much as about the verdict.
 *
 * Only rendered when something is actually worth replacing. A ranking of healthy drives is a
 * ranking of nothing, and a section that is always present stops being read — the same reasoning as
 * the disconnected toggle and the sort row.
 *
 * The reasons are the point. A reader can disagree with a band and act on it; they cannot do either
 * with an unexplained number, which is why the server returns bands rather than a score.
 */
const REPLACE_BANDS = {
  now: ['Replace now', 'bad'],
  soon: ['Replace soon', 'caution'],
  watch: ['Watch', 'unknown'],
};

// A shortlist, not a list. Past a handful, the answer to "which one first?" stops being an answer.
const REPLACE_FIRST_LIMIT = 5;

export function replaceFirstCard(ranking, ctx) {
  const ranked = (ranking || []).filter((r) => r.band !== 'fine');
  if (!ranked.length) return null;

  const shown = ranked.slice(0, REPLACE_FIRST_LIMIT);
  const rest = ranked.length - shown.length;

  return el('div', { class: 'card stack replace-first' }, [
    el('div', { class: 'row' }, [
      el('h2', { text: 'Replace first' }),
      el('span', { class: 'spacer' }),
      el('span', { class: 'muted', text: 'by urgency, not by verdict' }),
    ]),
    el('ol', { class: 'replace-list' }, shown.map((r) => {
      const [label, tone] = REPLACE_BANDS[r.band] || [r.band, 'unknown'];
      return el('li', {}, [
        el('div', { class: 'row' }, [
          el('span', { class: `badge badge-${tone}`, text: label }),
          el('button', {
            class: 'link', type: 'button', onclick: () => ctx.openDevice(r.serial),
          }, [r.name || r.serial]),
        ]),
        // The reasons are the drive's own terms, so they read as an explanation rather than as a
        // verdict this app invented.
        el('div', { class: 'muted replace-reasons', text: (r.reasons || []).join(' · ') }),
      ]);
    })),
    rest > 0
      ? el('p', { class: 'muted', text: rest === 1
        ? '1 more drive has something against it.'
        : `${rest} more drives have something against them.` })
      : null,
  ]);
}

/**
 * The fleet export (`FEATURES.md` A23), behind one button.
 *
 * One control rather than two anchors. The format and the scope are a single decision made once, and
 * two links at the end of a controls row read as two more ways to look at the page rather than as a
 * file to keep. Putting them behind a dialog is also what gives each format room to say what it is
 * *for* — "the inventory you sort when planning a purchase" belongs beside the choice, not in a
 * tooltip nobody hovers.
 *
 * None for a lone drive. With one drive the export is the single-drive report with fewer columns, and
 * that report is better for the one thing a lone drive needs. That is also why it sits in the
 * controls row, which is drawn only when there is a fleet to control.
 */
export function exportButton(devices, tags) {
  if (devices.length < 2) return null;
  return el('button', {
    class: 'btn btn-sm',
    type: 'button',
    title: 'Save the fleet as a file',
    onclick: () => exportDialog(devices, tags),
  }, [icon('download'), 'Export…']);
}

/** The two formats, each with the question it answers rather than the extension it produces. */
const EXPORT_FORMATS = [
  { value: 'csv', label: 'CSV — a spreadsheet',
    help: 'One row per drive with every column. The one to sort when planning a purchase.' },
  { value: 'html', label: 'Printable page',
    help: 'Every drive on one page. The sheet to print or attach when several go back at once.' },
];

function exportDialog(devices, tags) {
  openDialog('Export the fleet', (body, close, foot) => {
    const format = el('select', {}, EXPORT_FORMATS.map((f) =>
      el('option', { value: f.value, text: f.label })));
    const formatHelp = el('p', { class: 'muted field-hint' });
    const syncFormat = () => {
      const chosen = EXPORT_FORMATS.find((f) => f.value === format.value) || EXPORT_FORMATS[0];
      formatHelp.textContent = chosen.help;
    };
    format.addEventListener('change', syncFormat);
    syncFormat();

    // Scope by tag, because that is the one narrowing the API offers and the one that means
    // something: the dashboard's own filter is about what you are looking at, and an export that
    // silently honoured it would hand you a file missing rows you never chose to exclude.
    const scope = el('select', {}, [
      el('option', { value: '', text: `The whole fleet (${devices.length})` }),
      ...tags.map((tag) => el('option', { value: tag, text: `Only ${tag} (${countTag(devices, tag)})` })),
    ]);

    body.append(
      el('div', { class: 'field' }, [el('label', { text: 'Format' }), format, formatHelp]),
      tags.length
        ? el('div', { class: 'field' }, [el('label', { text: 'Drives' }), scope])
        : null,
    );

    foot.append(
      el('button', { class: 'btn', type: 'button', text: 'Cancel', onclick: close }),
      el('button', {
        class: 'btn btn-primary',
        type: 'button',
        text: 'Export',
        onclick: () => {
          saveFile(api.fleetExportUrl(format.value, scope.value), exportName(format.value, scope.value));
          close();
        },
      }),
    );
  });
}

/** How many drives carry a tag. The same comma-separated split the server uses. */
function countTag(devices, tag) {
  return devices.filter((d) => String(d.tags || '').split(',')
    .map((t) => t.trim().toLowerCase()).includes(tag.toLowerCase())).length;
}

/** The name the browser saves under. Matches what the server sets, so the two cannot disagree. */
function exportName(format, tag) {
  const scope = tag ? `-${tag.replace(/[^A-Za-z0-9._-]+/g, '-')}` : '';
  return `smart-serve-fleet${scope}.${format === 'csv' ? 'csv' : 'html'}`;
}

/**
 * Save a URL as a file.
 *
 * An anchor with `download`, clicked and removed — the same mechanism the config export uses. These
 * are files to keep, so the browser's own save dialog is the right one rather than this code holding
 * the bytes.
 */
function saveFile(url, name) {
  const link = el('a', { href: url, download: name, hidden: true });
  document.body.append(link);
  link.click();
  link.remove();
}

/** The tags present on a fleet, in a stable order (`tags.js`). */
export { fleetTags };

/**
 * The enclosure heat card (`FEATURES.md` A25), or null when nothing is moving together.
 *
 * The insight is that an enclosure is shared: eight drives in one chassis have one fan, one intake
 * and one ambient, so a failing fan presents as *all eight* running hotter at once — which per-drive
 * monitoring reports as eight unrelated alerts, precisely the noise that teaches people to ignore
 * alerts. This says it once, naming the group.
 *
 * It reports a **correlation, not a cause**, and the wording says so. A RAID rebuild heats everything
 * too, and so does a warm afternoon; nothing observable from a temperature tells them apart, so the
 * card names what moved and where to look and leaves the diagnosis to the reader, who knows whether
 * a rebuild is running. A false "your enclosure is overheating" would be worse than the eight true
 * per-drive alerts it is meant to replace.
 *
 * The ratio is shown rather than a bare count — six of six is a different finding from six of
 * twenty-four — because that judgement belongs to the reader.
 */
export function heatCard(groups, ctx) {
  if (!groups || !groups.length) return null;

  return el('div', { class: 'card stack heat-card' }, [
    el('div', { class: 'row' }, [
      el('h2', { text: 'Heating together' }),
      el('span', { class: 'spacer' }),
      el('span', { class: 'muted', text: 'a correlation, not a diagnosis' }),
    ]),
    ...groups.map((g) => el('div', { class: 'heat-group' }, [
      el('div', { class: 'row' }, [
        el('span', { class: 'badge badge-caution', text: `+${deltaText(g.delta_c)}` }),
        el('strong', { text: g.group }),
        el('span', { class: 'muted', text: `${g.drives.length} of ${g.total} drives` }),
        g.max_c !== undefined
          ? el('span', { class: 'muted', text: `· hottest now ${maxText(g.max_c)}` })
          : null,
      ]),
      el('div', { class: 'muted heat-reasons', text: `${g.drives.length} drives in this group are ` +
        `at least ${deltaText(g.delta_c)} hotter than at the previous reading. If they share an ` +
        'enclosure, look there first — a fan, a filter, or ambient — rather than at the drives.' }),
      el('div', { class: 'heat-drives' }, g.drives.map((serial) => el('button', {
        class: 'link', type: 'button', onclick: () => ctx.openDevice(serial),
      }, [serial]))),
    ])),
  ]);
}

/** A rise, in the display unit. A span, so it scales without the 32° offset. */
function deltaText(celsius) {
  const value = convertTempDelta(celsius);
  return value === null ? '—' : `${value} °${temperatureUnitName()}`;
}

/** A reading, in the display unit. */
function maxText(celsius) {
  const value = convertTemp(celsius);
  return value === null ? '—' : `${value} °${temperatureUnitName()}`;
}

/**
 * The testing session (`FEATURES.md` D11), or null when nothing is on the bench.
 *
 * A drive tagged `bench` is being evaluated rather than used, so the app leaves it out of the health
 * counts and out of the replace-first ranking — a batch of failing bench drives would otherwise
 * dominate both with drives you already know about, and a bench drive failing is a **result** rather
 * than an alarm. The card exists so that exclusion is not silent: it says what is on the bench and
 * what is being left out, and it is where the batch's verbs live.
 *
 * Rendered only when the bench is not empty, the same rule as the replace-first shortlist. A panel
 * that is always present stops being read.
 *
 * The batch verbs are here rather than on a tag-group heading because grouping is drawn two
 * different ways — card headings and a table separator row — and putting three buttons in a table
 * cell to keep the two views level is worse than having one place for them.
 *
 * `benchTag` is passed in rather than hard-coded: the server owns which tag means "on the bench", and
 * sends it with the device list.
 */
export function benchSession(bench, ctx, benchTag) {
  if (!bench.length) return null;

  return el('div', { class: 'card stack bench-session' }, [
    // The explainer shares the title line rather than taking one of its own: it is a single clause
    // about what being here means, and a paragraph on its own row cost more vertical space than it
    // earned. The count sits at the far end, where a count belongs.
    el('div', { class: 'row wrap' }, [
      el('h2', { text: 'Drives in testing' }),
      el('span', { class: 'muted', text: 'left out of the health counts — a drive that fails while ' +
        'you are testing it is a result, not an alarm' }),
      el('span', { class: 'spacer' }),
      el('span', { class: 'muted', text: countText(bench.length) }),
    ]),
    // Inline rather than one row each. The session *is* the batch, so every drive is still named —
    // but eight rows of list for eight drives is a lot of card for a set of names, and the model is
    // the thing worth keeping to hand.
    el('div', { class: 'bench-chips' }, bench.map((d) => el('button', {
      class: 'chip chip-link',
      type: 'button',
      title: [d.bay, d.model].filter(Boolean).join(' · ') || 'Open this drive',
      onclick: () => ctx.openDevice(d.serial),
    }, [deviceTitle(d)]))),
    el('div', { class: 'row bench-actions' }, [
      el('button', {
        class: 'btn btn-sm',
        type: 'button',
        title: 'Start a short self-test on every drive in the batch',
        onclick: (e) => testAll(bench, ctx, e.currentTarget),
      }, ['Test all']),
      // An anchor with `download`, like every other export: this is a file to keep.
      el('a', {
        class: 'btn btn-sm',
        href: api.fleetExportUrl('csv', benchTag),
        download: `smart-serve-fleet-${benchTag}.csv`,
        title: 'One spreadsheet row per drive in the batch, for an RMA or a purchase decision',
      }, [icon('download'), 'Export batch']),
      el('button', {
        class: 'btn btn-sm',
        type: 'button',
        title: 'Forget every drive in the batch, once they have been reported',
        onclick: (e) => forgetAll(bench, ctx, e.currentTarget),
      }, ['Forget all']),
    ]),
  ]);
}

/**
 * Start a short self-test on every drive in the batch.
 *
 * Sequential, and a failure is counted rather than thrown. One drive that cannot be tested must not
 * abandon the rest of the batch, and eight concurrent `smartctl` calls against one host is a
 * stampede for no gain — the drives take hours either way.
 *
 * Confirmed first, because a self-test is the only thing this app does that writes to a drive
 * (`A19`), and doing it to a whole batch at once is exactly the kind of action that should be
 * deliberate.
 */
async function testAll(bench, ctx, button) {
  const ok = await confirmDialog('Test the whole batch',
    `Start a short self-test on ${countText(bench.length)}? Each drive will be busy for a few ` +
    'minutes, and the results appear on the drive pages.', 'Test all');
  if (!ok) return;

  button.disabled = true;
  let started = 0;
  const failed = [];
  for (const d of bench) {
    try {
      await api.startSelfTest(d.serial, 'short');
      started += 1;
    } catch (err) {
      failed.push(`${deviceTitle(d)}: ${err.message}`);
    }
  }
  button.disabled = false;

  if (failed.length) {
    // Named rather than counted: "2 of 6 failed" tells you nothing about which two to go and look at.
    toast(`Started ${started} of ${bench.length}. ${failed.join('; ')}`, 'error');
  } else {
    toast(`Short self-test started on ${countText(started)}`, 'good');
  }
  ctx.refresh();
}

/**
 * Forget every drive in the batch (`FEATURES.md` D4, batch form).
 *
 * One confirmation for the batch rather than one per drive, and the serials are listed in it — this
 * is the only irreversible action in the app, so the dialog has to be specific about what goes.
 *
 * Driven from the client rather than a bulk endpoint on purpose: a request that deletes every drive
 * matching a tag is one mis-click away from clearing the fleet, and a loop reports partial failure
 * honestly instead of pretending the batch was atomic.
 */
async function forgetAll(bench, ctx, button) {
  const names = bench.map((d) => d.name || d.serial).join(', ');
  const ok = await confirmDialog('Forget the whole batch',
    `This removes ${countText(bench.length)} — ${names} — with their history and notes. A scan will ` +
    'not bring them back unless they are connected again. This cannot be undone.', 'Forget all');
  if (!ok) return;

  button.disabled = true;
  let gone = 0;
  const failed = [];
  for (const d of bench) {
    try {
      await api.forgetDevice(d.serial);
      gone += 1;
    } catch (err) {
      failed.push(`${deviceTitle(d)}: ${err.message}`);
    }
  }
  button.disabled = false;

  if (failed.length) {
    toast(`Forgot ${gone} of ${bench.length}. ${failed.join('; ')}`, 'error');
  } else {
    toast(`Forgot ${countText(gone)}`, 'good');
  }
  ctx.refresh();
}

/**
 * Split the visible drives into tag groups (`FEATURES.md` D9).
 *
 * A drive appears under **each** of its tags. The alternative — first tag wins — silently drops a
 * drive from a group it is tagged for, and a grouping that hides a drive is worse than one that
 * shows it twice: the whole reason to group a fleet is not to miss one. The cost is that the group
 * counts do not sum to the fleet size, which the header says outright rather than leaving to be
 * discovered.
 *
 * Untagged drives come last, because they are the ones with something left to do to them.
 *
 * Deliberately not a filter. `FEATURES.md` draws that distinction, and it is the right one: a
 * filter answers "show me only this", which the health chips and the transport select already do,
 * while grouping answers "let me see the shape of the fleet".
 */
export function groupByTag(shown) {
  const groups = new Map();
  const untagged = [];

  for (const d of shown) {
    const tags = tagList(d.tags);
    if (!tags.length) {
      untagged.push(d);
      continue;
    }
    for (const tag of tags) {
      if (!groups.has(tag)) groups.set(tag, []);
      groups.get(tag).push(d);
    }
  }

  const out = [...groups.entries()]
    .sort((a, b) => a[0].localeCompare(b[0], undefined, { sensitivity: 'base' }))
    .map(([label, devices]) => ({ label, devices }));
  if (untagged.length) out.push({ label: 'Untagged', devices: untagged });
  return out;
}

/**
 * The drives to show, in the order to show them (`FEATURES.md` D5).
 *
 * Exported and pure, because this *is* the feature: the filtering and ordering rules are what D5
 * added, and keeping them out of the render means they can be tested without a DOM, a fetch, or a
 * card. Every option defaults to "show everything, worst first", so a caller can pass only what
 * it means to change.
 */
export function visibleDevices(all, opts = {}) {
  const {
    health = null, query = '', transport = '', showMissing = false, sortKey = 'health',
    benchOnly = false,
  } = opts;

  const spec = SORTS.find((s) => s.key === sortKey) || SORTS[0];
  // A caller may override the direction, but the *field's* own idea of "first" is the default.
  // That keeps the rule in one place: a caller that only names a field cannot get it backwards,
  // and the toolbar reads its default from the same row of the table.
  const ascending = opts.ascending === undefined ? spec.ascending : opts.ascending;

  // A drive that has been unplugged is kept (its notes and history matter) but is flagged
  // is_missing. Hidden unless asked for.
  const pool = showMissing ? all : all.filter((d) => !d.is_missing);
  const needle = String(query).trim().toLowerCase();

  const shown = pool.filter((d) => {
    if (transport && (d.transport || '') !== transport) return false;
    if (health && d.health !== health) return false;
    // `on_bench` is the server's reading of the bench tag (`FEATURES.md` D11), so this filter never
    // has to know the tag's name — or split a tag string the way the grouping does.
    if (benchOnly && !d.on_bench) return false;
    if (!needle) return true;
    // Bay is searchable, because hunting a drive by where it physically is is the whole point of
    // recording one.
    return [d.name, d.model, d.serial, d.device_path, d.bay, (d.mountpoints || []).join(' ')]
      .filter(Boolean)
      .some((field) => String(field).toLowerCase().includes(needle));
  });

  return sortBy(shown, sortKey, ascending);
}

export async function render(container, ctx) {
  mount(container, skeletonCards(3));
  try {
    draw(container, ctx, await api.devices());
  } catch (err) {
    mount(container, errorBox(`Could not load devices: ${err.message}`));
  }
}

function draw(container, ctx, data) {
  const all = (data.devices || []).filter((d) => !d.hidden);
  // A drive that has been unplugged is kept (its notes and history matter) but
  // is flagged is_missing. Hide those from the dashboard unless asked for.
  const connected = all.filter((d) => !d.is_missing);
  const missing = all.filter((d) => d.is_missing);

  // A drive on the bench is being evaluated rather than used (`FEATURES.md` D11), so it is left out
  // of the counts and out of the replace-first ranking the server sends. Stated below rather than
  // silent: the counts are what you glance at, and a number that quietly excluded drives would be
  // worse than one that is slightly too alarming.
  const bench = connected.filter((d) => d.on_bench);
  const inFleet = connected.filter((d) => !d.on_bench);
  // The tag the server treats as the bench. Read from the payload rather than hard-coded, so the two
  // cannot drift about which drives are on the bench.
  const benchTag = data.bench_tag || 'bench';

  const counts = { good: 0, caution: 0, bad: 0, unknown: 0 };
  for (const d of inFleet) counts[d.health] = (counts[d.health] || 0) + 1;

  const search = el('input', {
    type: 'search',
    id: 'device-search',
    class: 'devices-search',
    placeholder: 'Filter by name, model or serial  (/)',
    value: query,
    oninput: () => {
      query = search.value.trim().toLowerCase();
      paint();
    },
  });

  // One rectangle for the page identity, the health counts and the search box. The
  // counts are a filter for the grid below, so they belong beside the thing they
  // filter rather than in a row of their own.
  //
  // The transports actually present, rather than a vocabulary this app invented: a host with no SAS
  // drives should not be offered "SAS" and then show nothing when it is picked. Ordered by kind
  // rather than alphabetically, so the control does not reshuffle as drives come and go.
  const transportOptions = [...new Set(all.map((d) => d.transport || ''))]
    .sort((a, b) => transportRank(a) - transportRank(b) || a.localeCompare(b))
    .map((t) => ({ value: t, label: transportLabel({ transport: t }) }));

  const sortSelect = el('select', {
    title: 'Order the list',
    onchange: () => {
      sortKey = sortSelect.value;
      // Each field carries its own idea of "first" — see SORTS.
      ascending = (SORTS.find((s) => s.key === sortKey) || SORTS[0]).ascending;
      draw(container, ctx, data);
    },
  }, SORTS.map((s) => el('option', { value: s.key, text: s.label, selected: s.key === sortKey })));

  // The direction says what it is doing in words, and carries a real arrow. A bare ↑ in a ghost
  // button beside a dropdown was the weakest possible way to say "this is the other half of the
  // sort" — it read as decoration rather than as a control, which is exactly how it was reported.
  const direction = el('button', {
    class: 'btn btn-sm',
    type: 'button',
    title: ascending ? 'Ascending — click for descending' : 'Descending — click for ascending',
    onclick: () => {
      ascending = !ascending;
      draw(container, ctx, data);
    },
  }, [icon(ascending ? 'arrow-up' : 'arrow-down'), ascending ? 'Ascending' : 'Descending']);

  /** One half of the cards/table switch. */
  function viewButton(key, label) {
    return el('button', {
      class: 'btn btn-sm',
      type: 'button',
      'aria-pressed': view === key ? 'true' : 'false',
      onclick: () => {
        view = key;
        draw(container, ctx, data);
        // Stored server-side, like the theme and the temperature unit, so the choice survives a
        // reload. Sent after the re-render rather than before it: the switch has to be instant, and a
        // save that fails is reported rather than rolling the view back — the reader asked for the
        // table, and showing it to them is the more useful half of the answer.
        api.putSettings({ dashboard_view: key })
          .catch((err) => toast(`Could not remember the view: ${err.message}`, 'error'));
      },
    }, [label]);
  }

  const transportSelect = transportOptions.length > 1
    ? el('select', {
        title: 'Show only one kind of connection',
        onchange: () => {
          transportFilter = transportSelect.value;
          paint();
        },
      }, [
        el('option', { value: '', text: 'All' }),
        ...transportOptions.map((t) => el('option', {
          value: t.value, text: t.label, selected: t.value === transportFilter,
        })),
      ])
    : null;

  // Only offered when something is actually tagged: a single "Untagged" group is not a grouping,
  // it is the list with a heading on it.
  const canGroup = all.some((d) => String(d.tags || '').trim() !== '');

  // The fleet export (`FEATURES.md` A23), one button behind which the format and the scope live.
  const exportControls = exportButton(all, fleetTags(all));

  const head = el('div', { class: 'card devices-head' }, [
    el('div', { class: 'devices-head-row' }, [
      el('h1', { text: 'Devices' }),
      healthFilter(counts, filter, (next) => {
        filter = next;
        draw(container, ctx, data);
      }),
      el('span', { class: 'spacer' }),
      search,
      // No scan button here: the sidebar carries the global one, and every card has
      // its own rescan. A third copy in the header was pure duplication.
      missing.length
        ? el('button', {
            class: 'btn btn-sm',
            type: 'button',
            'aria-pressed': showMissing ? 'true' : 'false',
            title: 'Drives recorded earlier that were not present in the last scan. Open one to ' +
              'forget it entirely.',
            onclick: () => {
              showMissing = !showMissing;
              draw(container, ctx, data);
            },
          }, [
            icon('help'),
            showMissing ? `Hide ${missing.length} disconnected` : `Show ${missing.length} disconnected`,
          ])
        : null,
      // A filter, not a toggle: bench drives are always listed. This is how you look at the batch on
      // its own without grouping, and it is also the answer to "why do the counts not add up?".
      bench.length
        ? el('button', {
            class: 'btn btn-sm',
            type: 'button',
            'aria-pressed': benchOnly ? 'true' : 'false',
            title: `Show only the drives tagged ${benchTag}, which are being tested rather than used`,
            onclick: () => {
              benchOnly = !benchOnly;
              draw(container, ctx, data);
            },
          }, [`Bench only (${bench.length})`])
        : null,
    ]),
    // Sorting and filtering one drive is meaningless, so the row is only drawn when there is a
    // choice to make.
    all.length > 1
      ? el('div', { class: 'devices-controls' }, [
          // Sort as one labelled pair: the field and its direction are a single decision, and the
          // label is what ties them together now that the direction says what it is doing in words.
          el('div', { class: 'control-group' }, [
            el('span', { class: 'control-label', text: 'Sort' }),
            sortSelect,
            direction,
          ]),
          transportSelect
            ? el('div', { class: 'control-group' }, [
                el('span', { class: 'control-label', text: 'Transport' }),
                transportSelect,
              ])
            : null,
          // Cards for a handful of drives, a table for a rack. It sits at the far end because it
          // changes the shape of the answer rather than what is being asked.
          el('span', { class: 'spacer' }),
          canGroup
            ? el('button', {
                class: 'btn btn-sm',
                type: 'button',
                'aria-pressed': grouped ? 'true' : 'false',
                title: 'Group the list by tag, so a pool reads as one block',
                onclick: () => {
                  grouped = !grouped;
                  draw(container, ctx, data);
                },
              }, ['Group by tag'])
            : null,
          // Cards or table, as one segmented switch rather than two loose buttons: they are a
          // mutually exclusive choice, and two identical-looking buttons make the reader check
          // which one is pressed to find out what the list is currently doing.
          el('div', { class: 'view-switch', role: 'group', 'aria-label': 'List layout' }, [
            viewButton('cards', 'Cards'),
            viewButton('table', 'Table'),
          ]),
          // A rule, because "how is this shown" and "what happens to this list" are different
          // questions: an export is a file to keep, and sitting shoulder to shoulder with the view
          // switch it read as one more way to look at the page.
          exportControls
            ? el('span', { class: 'controls-divider', 'aria-hidden': 'true' })
            : null,
          exportControls,
        ])
      : null,
    // One line rather than four. The two notes below are the answer to a question the counts above
    // raise — a reader who adds them up and gets the wrong number is owed the reason — so they
    // belong beside those counts rather than in rows of their own. The standing instructions about
    // rescanning and forgetting are gone: a line of help on every page load is read once and then
    // only takes up room, and both actions are labelled on the things they act on.
    el('div', { class: 'devices-head-sub muted' }, [
      el('span', { text: `Last scan ${fmtAgo(data.last_scan_ts)}` }),
      grouped
        ? el('span', { text: ' · A drive with more than one tag appears under each of them, so the ' +
            'group counts can add up to more than the fleet size.' })
        : null,
      bench.length
        ? el('span', { text: ` · ${countText(bench.length)} are tagged ${benchTag} and left out of ` +
            'those counts — a drive being tested failing is a result rather than an alarm. They are ' +
            'still listed below and still in every export.' })
        : null,
    ]),
  ]);

  if (!all.length) {
    mount(container, head, emptyState(
      'No drives discovered yet',
      'Press Scan to enumerate every device visible to the host, internal and USB.',
      el('button', { class: 'btn btn-primary', type: 'button',
        onclick: () => ctx.scan() }, [icon('scan'), 'Scan now']),
    ));
    return;
  }

  const grid = el('div', { class: 'card-grid' });

  function paint() {
    const shown = visibleDevices(all, {
      health: filter, query, transport: transportFilter, showMissing, sortKey, ascending, benchOnly,
    });
    const groups = grouped && shown.length ? groupByTag(shown) : null;

    // The holder is a card grid, a grouped stack or a table holder — never a mix. A table inside a
    // grid cell is laid out as a single column and loses the columns it exists for, and a card
    // grid inside a card grid inherits the wrong column sizing.
    grid.className = view === 'table' ? 'fleet-holder' : (groups ? 'grouped-holder' : 'card-grid');

    if (!shown.length) {
      mount(grid, emptyState('Nothing to show', 'No devices match the current filter.'));
      return;
    }

    const cardFor = (d) => deviceCard(d, ctx.openDevice, () => ctx.scanDevice(d.serial));

    if (view === 'table') {
      mount(grid, fleetTable(shown, {
        sortKey, ascending, groups, onOpen: ctx.openDevice,
        onScan: (serial) => ctx.scanDevice(serial),
        onSort: sortFromHeader,
      }));
      return;
    }

    mount(grid, groups
      ? groups.flatMap((g) => [
          el('div', { class: 'group-head' }, [
            el('h2', { text: g.label }),
            el('span', { class: 'muted', text: countText(g.devices.length) }),
          ]),
          el('div', { class: 'card-grid' }, g.devices.map(cardFor)),
        ])
      : shown.map(cardFor));
  }

  function sortFromHeader(key) {
    // Clicking the column already sorted reverses it; clicking another starts from that field's
    // own idea of "first", the same default the select uses.
    ascending = key === sortKey
      ? !ascending
      : (SORTS.find((s) => s.key === key) || SORTS[0]).ascending;
    sortKey = key;
    draw(container, ctx, data);
  }
  paint();

  // Order is "a condition before a suggestion": something happening now, then the batch in hand,
  // then the advice about what to buy.
  mount(container, head, heatCard(data.correlation, ctx), benchSession(bench, ctx, benchTag),
    replaceFirstCard(data.ranking, ctx), grid);
}

