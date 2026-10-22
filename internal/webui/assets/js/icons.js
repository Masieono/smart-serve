// Inline SVG icon set. No external assets, no icon font (docs/UI.md §1).
import { svg } from './dom.js';

const PATHS = {
  dashboard:
    '<rect x="3" y="3" width="7" height="9" rx="1.5"/><rect x="14" y="3" width="7" height="5" rx="1.5"/>' +
    '<rect x="14" y="12" width="7" height="9" rx="1.5"/><rect x="3" y="16" width="7" height="5" rx="1.5"/>',
  device: '<rect x="3" y="6" width="18" height="12" rx="2"/><circle cx="8" cy="12" r="1.6"/><path d="M13 12h5"/>',
  schedule: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  bell: '<path d="M6 9a6 6 0 1 1 12 0c0 4 1.5 5 1.5 5H4.5S6 13 6 9Z"/><path d="M10 19a2 2 0 0 0 4 0"/>',
  settings:
    '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/>',
  scan: '<path d="M20.5 12a8.5 8.5 0 1 1-3-6.4"/><path d="M20.5 4.5v5h-5"/>',
  alert: '<path d="M12 3 2 20h20L12 3Z"/><path d="M12 9.5v4.5"/><path d="M12 17h.01"/>',
  check: '<circle cx="12" cy="12" r="9"/><path d="m8 12 3 3 5-6"/>',
  x: '<circle cx="12" cy="12" r="9"/><path d="m9 9 6 6M15 9l-6 6"/>',
  help: '<circle cx="12" cy="12" r="9"/><path d="M9.6 9a2.5 2.5 0 1 1 3.4 2.3c-.7.4-1 .9-1 1.7"/><path d="M12 17h.01"/>',
  download: '<path d="M12 4v11"/><path d="m7 11 5 5 5-5"/><path d="M5 20h14"/>',
  trash: '<path d="M4 7h16"/><path d="M9 7V5h6v2"/><path d="M6 7l1 13h10l1-13"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  menu: '<path d="M4 7h16M4 12h16M4 17h16"/>',
  usb: '<path d="M12 21V7"/><path d="m8 10 4-3 4 3"/><circle cx="12" cy="5" r="2"/>',
  save: '<path d="M5 3h11l3 3v15H5z"/><path d="M9 3v6h6V3"/>',
  chevron: '<path d="m9 6 6 6-6 6"/>',
  back: '<path d="m15 6-6 6 6 6"/>',
  'arrow-up': '<path d="M12 20V5"/><path d="m5 11 7-6 7 6"/>',
  'arrow-down': '<path d="M12 4v15"/><path d="m5 13 7 6 7-6"/>',
};

/** Build an icon element. Decorative by default (aria-hidden). */
export function icon(name, extraClass = '') {
  const body = PATHS[name] || PATHS.help;
  // Always carries a base class: the stylesheet sizes icons through `.icon` rather
  // than a bare `svg` selector, which would override the width/height *attributes*
  // of the gauge and the sparkline.
  const classes = extraClass ? `icon ${extraClass}` : 'icon';
  const node = svg(
    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" ` +
      `stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" class="${classes}">${body}</svg>`,
  );
  return node;
}

export const ICON_NAMES = Object.keys(PATHS);
