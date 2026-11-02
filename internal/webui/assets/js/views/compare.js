// Side-by-side drive comparison (docs/UI.md §7.6, FEATURES.md D6).
//
// A dialog rather than a page, because a comparison is a *look* rather than a place: you select two
// drives, read the table, and go back to what you were doing. A route would mean a URL that only
// makes sense with a query string, and a back button that returns you to a list you have already
// left.
//
// The table itself is built from the server's aligned rows, not from N device payloads. The
// alignment *is* the feature — two drives of one model report their attributes in whatever order
// smartctl felt like — and it lives in Go where it can be tested (`internal/httpapi/compare.go`).
import { el, mount } from '../dom.js';
import { api } from '../api.js';
import { openDialog, toast } from '../components.js';
import { icon } from '../icons.js';

/**
 * How many drives may be compared at once.
 *
 * A courtesy copy of the server's limit (`httpapi.CompareMaxDrives`), which is authoritative and
 * answers `400` if the two ever disagree. It is here so that ticking a seventh box *says why* rather
 * than being accepted and then refused by a round trip.
 */
export const COMPARE_MAX_DRIVES = 6;

/**
 * Which cell of a row is the "worst" one, for the reader who is scanning rather than reading.
 *
 * Higher is worse for a normalised value — 100 is a healthy attribute and a low value is a worn one
 * — but only for attributes the drive itself publishes a threshold for. Without a threshold there is
 * no scale to compare on, and picking the numerically highest would call a drive with more power-on
 * hours the worse drive, which is not a judgement this app makes.
 *
 * Returns -1 when no cell can be called worse than the others.
 */
export function worstCell(row) {
  let at = -1;
  let worst = null;
  for (const [index, cell] of (row.cells || []).entries()) {
    if (!cell || !cell.threshold) continue;
    if (worst === null || cell.value < worst) {
      worst = cell.value;
      at = index;
    }
  }
  return at;
}

/** A drive's column heading: what it is, and where it is. */
export function driveHeading(drive) {
  const parts = [drive.name || drive.serial];
  if (drive.model) parts.push(drive.model);
  if (drive.bay) parts.push(drive.bay);
  return parts.join(' · ');
}

/** The rows worth reading first: anything a drive has flagged, then anything the drives disagree on. */
export function interestingRows(rows) {
  return (rows || []).filter((row) => row.flagged || row.differs);
}

/**
 * Open the comparison for a set of serials.
 *
 * `serials` is the order the columns appear in, which is the order the user selected them — a
 * comparison whose columns reshuffle between openings is one nobody can learn to read.
 */
export function openCompare(ctx, serials) {
  openDialog(`Comparing ${serials.length} drives`, (body, close) => {
    const table = el('div', { class: 'compare-body' });
    body.append(table);

    const render = (data) => {
      const drives = data.drives || [];
      const rows = data.rows || [];

      const head = el('tr', {}, [
        el('th', { class: 'compare-attr', text: 'Attribute' }),
        ...drives.map((d) => el('th', {}, [
          el('div', { class: 'compare-drive', text: d.name || d.serial }),
          el('div', { class: 'muted', text: d.model || '' }),
          d.bay ? el('div', { class: 'muted', text: d.bay }) : null,
        ])),
      ]);

      const bodyRows = rows.map((row) => {
        const worstAt = worstCell(row);
        return el('tr', { class: row.flagged ? 'compare-flagged' : (row.differs ? 'compare-differs' : '') }, [
          el('td', { class: 'compare-attr' }, [
            el('span', { class: 'compare-id', text: String(row.id) }),
            el('span', { text: row.name }),
          ]),
          ...drives.map((_, index) => {
            const cell = (row.cells || [])[index];
            // An absent attribute is an em dash, never a zero: an NVMe drive does not have
            // "Reallocated Sector Count: 0", it does not have the attribute at all.
            if (!cell) return el('td', { class: 'num muted', text: '—' });
            const flagged = cell.flagged;
            const worst = !flagged && index === worstAt;
            return el('td', {
              class: `num${flagged ? ' compare-cell-flagged' : (worst ? ' compare-cell-worst' : '')}`,
              title: cell.raw_string ? `Raw ${cell.raw_string}` : '',
            }, [
              el('span', { class: 'compare-value', text: String(cell.value) }),
              // The raw count, when it says something the normalised value does not. For the sector
              // counters it is the whole finding — a healthy drive reports 100 whatever the real
              // count is, so "100" beside "100" hides the drive with twelve bad sectors — while for
              // a temperature the two are the same number and printing it twice is noise.
              cell.raw_value !== cell.value
                ? el('span', { class: 'compare-raw', text: String(cell.raw_value) })
                : null,
            ]);
          }),
        ]);
      });

      const interesting = interestingRows(rows);
      mount(table,
        el('div', { class: 'row wrap compare-summary muted' }, [
          el('span', { text: `${rows.length} attributes compared` }),
          el('span', { text: ` · ${interesting.length} worth reading` }),
          el('span', { text: ' · a flagged cell is the drive\'s own threshold, not this app\'s opinion' }),
        ]),
        el('div', { class: 'table-wrap' }, [
          el('table', { class: 'data compare-table' }, [
            el('thead', {}, [head]),
            el('tbody', {}, bodyRows),
          ]),
        ]),
      );

      if (!rows.length) {
        table.append(el('p', { class: 'muted', text: 'None of these drives has an attribute table to ' +
          'compare. They may never have been read, or they may be NVMe drives, which report a health ' +
          'log rather than ATA attributes.' }));
      }
    };

    render({ drives: [], rows: [] });
    api.compare(serials)
      .then(render)
      .catch((err) => {
        mount(table, el('p', { class: 'muted', text: `Could not compare: ${err.message}` }));
        toast(err.message, 'error');
      });
  }, { dismissOnBackdrop: false });
}

/** The button that opens a comparison, or null when there is nothing to compare yet. */
export function compareButton(selected, ctx) {
  const serials = [...selected];
  if (serials.length < 2) return null;
  return el('button', {
    class: 'btn btn-sm',
    type: 'button',
    title: 'Compare these drives attribute by attribute',
    onclick: () => openCompare(ctx, serials),
  }, [icon('table'), `Compare ${serials.length}`]);
}

/**
 * Adds a drive to the comparison, or explains why it cannot.
 *
 * Returns the new selection. Past the cap the selection is left alone and a toast says why: a tick
 * that silently does nothing is the worst of the three options, and the server would refuse the
 * comparison anyway.
 */
export function addToCompare(selected, serial, checked) {
  const next = new Set(selected);
  if (!checked) {
    next.delete(serial);
    return next;
  }
  if (!next.has(serial) && next.size >= COMPARE_MAX_DRIVES) {
    toast(`At most ${COMPARE_MAX_DRIVES} drives can be compared at once`, 'error');
    return next;
  }
  next.add(serial);
  return next;
}
