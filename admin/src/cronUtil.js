// Ready-made schedules for the Automation page, and plain-language names for cron expressions.

export const CRON_PRESETS = [
  { id: 'hourly', label: 'Every hour', cron: '0 * * * *' },
  { id: '6h', label: 'Every 6 hours', cron: '0 */6 * * *' },
  { id: '12h', label: 'Twice a day (00:00 and 12:00)', cron: '0 0,12 * * *' },
  { id: 'nightly', label: 'Every night at 03:00', cron: '0 3 * * *' },
  { id: 'weekly', label: 'Every Sunday at 03:00', cron: '0 3 * * 0' },
  { id: 'monthly', label: 'First of the month at 03:00', cron: '0 3 1 * *' },
];

/** The preset a cron expression equals, or null (extra spaces are ignored). */
export function presetFor(cron) {
  const normalized = String(cron || '').trim().replace(/\s+/g, ' ');
  return CRON_PRESETS.find(p => p.cron === normalized) || null;
}

/** "Every night at 03:00" for a preset, the expression itself otherwise. */
export function describeCron(cron) {
  return presetFor(cron)?.label || String(cron || '').trim();
}

/** A quick client-side check before sending: five space-separated fields (the server checks the rest). */
export function looksLikeCron(cron) {
  const value = String(cron || '').trim();
  return /^@(hourly|daily|midnight|weekly|monthly)$/i.test(value) || value.split(/\s+/).length === 5;
}

export const JOB_KINDS = [
  { id: 'SCAN', label: 'Scan for new files', hint: 'Indexes new and changed videos and removes missing ones. Pick one library, or every library.' },
  { id: 'DUPLICATE_SCAN', label: 'Find duplicates', hint: 'Reads every file in full, so run it rarely, at quiet hours.' },
  { id: 'HOUSEKEEPING', label: 'Housekeeping', hint: 'Removes unused images, stale caches and old history with the default tasks. Library-folder tasks never run on a schedule.' },
];
export const jobKindLabel = id => JOB_KINDS.find(k => k.id === id)?.label || id;
