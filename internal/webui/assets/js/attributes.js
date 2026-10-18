// What the S.M.A.R.T. fields mean, in plain language (docs/UI.md §7.2, FEATURES.md A12).
//
// An attribute table is the densest thing this app shows, and it is unreadable to anyone
// who has not already spent time with CrystalDiskInfo. "Reallocated Sector Count 100 100
// 10 0" is four numbers and no meaning; this module supplies the meaning.
//
// The set is **curated, not exhaustive**, and it deliberately mirrors the fields the
// interpreter actually acts on (docs/SMART_INTERPRETATION.md §1.1 and §2). Explaining a
// row the verdict ignores is noise, and a description that disagreed with the verdict
// would be worse than no description at all. Anything absent here still renders — the
// name smartctl reported is shown as-is, just without a `?` affordance.
//
// `what` is the definition; `watch` is how to read it, which is the part people actually
// need and the part that stops a benign vendor counter from looking like a failure.

/** ATA attribute help, keyed by attribute id. */
export const ATTRIBUTE_HELP = {
  1: {
    what: 'The rate at which the drive hits read errors it cannot correct.',
    watch: 'Vendors encode this differently, so the raw figure is not comparable between drives and is normally ignored.',
  },
  3: {
    what: 'The average time the platters take to reach full speed.',
    watch: 'Mechanical drives only. A rising trend can point at a worn spindle bearing.',
  },
  4: {
    what: 'How many times the drive has spun up and down.',
    watch: 'Informational. It describes age and duty cycle, not health.',
  },
  5: {
    what: 'Sectors the drive found unreadable and moved out of the way into its spare pool.',
    watch: 'Any non-zero value means the drive has lost capacity to defects. It is a warning sign, and how fast it grows is the part that matters.',
  },
  7: {
    what: 'The rate of errors while positioning the read/write heads.',
    watch: 'Vendor-specific encoding; the raw value is not comparable between drives.',
  },
  9: {
    what: 'The total number of hours the drive has been powered on.',
    watch: 'This is age, not health. A high figure on a clean drive is not a problem.',
  },
  10: {
    what: 'How many times the drive needed more than one attempt to reach full spin speed.',
    watch: 'Any non-zero value suggests mechanical wear or a weak power supply.',
  },
  12: {
    what: 'How many times the drive has been switched on.',
    watch: 'Informational. It describes duty cycle rather than condition.',
  },
  169: {
    what: "A vendor's remaining-life figure for a solid-state drive.",
    watch: 'Only some SSDs report this. Where it appears, the normalised value is the percentage of life left.',
  },
  177: {
    what: 'How evenly the drive has spread writes across its flash cells.',
    watch: 'On Samsung SSDs the normalised value is the remaining life, counting down from 100. On other brands the same id holds a raw erase count instead, which is why the app reads it only when the drive identifies as Samsung.',
  },
  183: {
    what: 'Blocks that went bad while the drive was in use.',
    watch: 'Any growth means the flash is degrading.',
  },
  184: {
    what: "Errors in the path between the drive's controller and its memory.",
    watch: 'Any non-zero value is serious, because this counter involves data integrity.',
  },
  187: {
    what: 'Errors the drive could not correct by itself.',
    watch: 'Any non-zero value means data was lost or returned in error.',
  },
  188: {
    what: 'Commands the drive abandoned because they took too long.',
    watch: 'A handful can happen on a busy or badly connected drive. A growing count points at the drive or its connection.',
  },
  190: {
    what: 'The drive temperature, in some vendors’ encoding.',
    watch: 'Where a drive reports both 190 and 194, the app prefers 194.',
  },
  191: {
    what: 'Shock or vibration the drive has recorded through its accelerometer.',
    watch: 'Most useful on a laptop, or on a drive that has been moved while running.',
  },
  192: {
    what: 'How many times the heads were parked.',
    watch: 'Informational. It relates to power-off events rather than to failure.',
  },
  193: {
    what: 'How many times the heads have been loaded onto and unloaded from the platters.',
    watch: 'Some laptop drives have a surprisingly low rating here, so a high count is worth knowing about even though it is not a failure on its own.',
  },
  194: {
    what: "The drive's current temperature.",
    watch: 'Most drives are happiest below about 45 °C. Sustained heat shortens life and is the one thing here you can usually fix.',
  },
  196: {
    what: 'How many times the drive has remapped sectors.',
    watch: 'This counts events, not sectors, because one event can move many. Any non-zero value is worth watching alongside attribute 5.',
  },
  197: {
    what: 'Sectors the drive could not read but has not yet given up on.',
    watch: 'They get retried on the next write, so the count can fall again. A non-zero value is a strong warning; ten or more is serious.',
  },
  198: {
    what: "Sectors that failed the drive's own background scan.",
    watch: 'Unlike pending sectors, these have been given up on. Any non-zero value is a strong warning.',
  },
  199: {
    what: 'Errors on the link between the drive and its controller.',
    watch: 'This is usually the SATA cable or the USB bridge rather than the drive, so reseat or replace it. The count never decreases, so it keeps showing after the cause is fixed.',
  },
  200: {
    what: 'Errors while writing across the drive’s recording zones.',
    watch: 'Vendor-specific encoding.',
  },
  202: {
    what: "A vendor's wear figure for a solid-state drive.",
    watch: 'Some SSDs report remaining life here instead of life used; the app reads it as the percentage left, and rejects anything outside 1–100.',
  },
  231: {
    what: 'Remaining life, and the closest thing to a standard SSD wear figure.',
    watch: 'The normalised value is the percentage of life left, counting down from 100.',
  },
  233: {
    what: "A vendor's remaining-life figure for a solid-state drive.",
    watch: 'The normalised value is the percentage of life left.',
  },
  240: {
    what: 'Hours spent flying the heads over the platters.',
    watch: 'Vendor-specific; some drives report a raw count here rather than hours.',
  },
  241: {
    what: 'Lifetime data written, counted in 512-byte sectors.',
    watch: 'Compare it against the drive’s rated endurance to see how much of that rating you have used.',
  },
  242: {
    what: 'Lifetime data read, counted in 512-byte sectors.',
    watch: 'Informational. Reads do not wear flash the way writes do.',
  },
};

/**
 * NVMe health-log help, keyed by the field name the API sends.
 *
 * Keyed by field rather than by label so the table can be reworded without silently
 * detaching its explanations.
 */
export const NVME_HELP = {
  critical_warning: {
    what: 'A bit field of problems the controller has flagged for itself.',
    watch: 'Any non-zero value means at least one warning bit is set, and the verdict reacts to it.',
  },
  available_spare: {
    what: 'The percentage of spare flash still available to replace worn-out blocks.',
  },
  available_spare_threshold: {
    what: 'The level at which the controller starts warning about spare flash.',
    watch: 'Once available spare reaches this figure, the drive is close to the end of its useful life.',
  },
  percentage_used: {
    what: "How much of the drive's rated write endurance has been used.",
    watch: 'This is the manufacturer’s own estimate rather than a measurement, which is why the app reports it as an estimate.',
  },
  power_on_hours: { what: 'The total number of hours the drive has been powered on.' },
  power_cycles: { what: 'How many times the drive has been switched on.' },
  unsafe_shutdowns: {
    what: 'Times the drive lost power without a clean shutdown.',
    watch: 'A high count usually points at a power supply or cabling problem rather than at the drive.',
  },
  controller_busy_time: { what: 'Minutes the controller has spent processing input and output.' },
  warning_temp_time: {
    what: "Minutes spent above the drive's warning temperature.",
    watch: 'Persistent warmth is worth fixing; this is how long the drive has been warm.',
  },
  critical_comp_time: {
    what: 'Minutes spent above the critical temperature.',
    watch: 'Any non-zero value means the drive has had to throttle itself to protect its components.',
  },
  data_units_read: {
    what: 'Lifetime data read, in units of 1,000 × 512-byte sectors.',
    watch: 'The table shows the humanised size alongside the raw count.',
  },
  data_units_written: {
    what: 'Lifetime data written, in the same units.',
    watch: 'Compare it against the drive’s rated endurance to see how much of that rating you have used.',
  },
  host_reads: { what: 'The number of read commands the host has sent.' },
  host_writes: { what: 'The number of write commands the host has sent.' },
  media_errors: {
    what: 'Unrecovered data-integrity errors.',
    watch: 'Any non-zero value means data was lost or returned in error.',
  },
  num_err_log_entries: {
    what: "How many entries the drive's error log holds.",
    watch: 'Any non-zero value is worth investigating; the raw output shows the log itself.',
  },
};
/** Help for an ATA attribute id, or null when this table has nothing to say about it. */
export function attributeHelp(id) {
  return ATTRIBUTE_HELP[Number(id)] || null;
}

/** Help for an NVMe health-log field, or null. */
export function nvmeHelp(field) {
  return NVME_HELP[field] || null;
}

/**
 * Whether the drive *itself* reports this attribute as failing.
 *
 * This is the drive's own alarm condition — its normalised value has fallen to or below
 * the threshold the drive publishes for itself — and not a threshold this app invented.
 * That distinction is why it is the only attribute-level judgement made in the UI: the
 * verdict and the score are computed server-side from the interpreted set
 * (`SMART_INTERPRETATION.md` §2), and re-deriving "is this bad?" here would let the
 * attribute table and the verdict disagree about the same drive.
 *
 * A threshold of 0 means the attribute has no threshold, not that it fails at zero —
 * most informational attributes report 0 — so those are never flagged.
 */
export function attributeFlagged(attr) {
  if (!attr) return false;
  const value = Number(attr.value);
  const threshold = Number(attr.threshold);
  if (!Number.isFinite(value) || !Number.isFinite(threshold)) return false;
  return threshold > 0 && value <= threshold;
}

