import { parseTs } from './timeUtil.js';

// Task groups on the Housekeeping page, in order.
export const GROUPS = [
  { id: 'files', title: 'ReelStash data folders', hue: 'blue', description: 'Generated images, clips and caches on the server. Anything removed here is made again when needed.' },
  { id: 'database', title: 'Database', hue: 'violet', description: 'Records that point at nothing, and history past its retention period.' },
  { id: 'library', title: 'Library folders', hue: 'amber', description: 'Leftover files beside your videos. Off unless you pick them.' },
];

// [settings key, label, hint]
export const RETENTION = [
  ['jobDays', 'Job history', 'Finished jobs and scan records.'],
  ['notificationDays', 'Notifications', 'In-app notifications.'],
  ['capacityDays', 'Disk capacity history', 'Hourly readings behind the storage charts.'],
  ['cacheDays', 'Unused cache', 'Remuxed videos and converted subtitles nobody has played.'],
];

export const plural = (n, word) => `${(n || 0).toLocaleString()} ${word}${n === 1 ? '' : 's'}`;

// The findings to show beside each task, each with its mode: what the last
// analysis found ('analysis'), or, for tasks a later clean-up ran, what that
// clean-up removed ('cleanup').
export function findingsFor(data) {
  const analysis = data?.lastAnalyze?.result ? data.lastAnalyze : null;
  const cleanup = data?.lastCleanup?.result ? data.lastCleanup : null;
  const analyzedAt = parseTs(analysis?.finishedAt)?.getTime() || 0;
  const cleanedAt = parseTs(cleanup?.finishedAt)?.getTime() || 0;
  const findings = {};
  for (const f of analysis?.result.findings || []) findings[f.task] = { ...f, mode: 'analysis' };
  if (cleanup && cleanedAt >= analyzedAt) {
    for (const f of cleanup.result.findings || []) findings[f.task] = { ...f, mode: 'cleanup' };
  }
  return { findings, analyzedAt: analysis?.finishedAt || null };
}

// What the selected tasks would remove, by the last analysis.
export function selectionTotals(selected, findings) {
  const totals = { count: 0, bytes: 0 };
  if (!selected) return totals;
  for (const id of selected) {
    const f = findings[id];
    if (!f || f.mode !== 'analysis' || id === 'vacuum') continue;
    totals.count += f.count || 0;
    totals.bytes += f.bytes || 0;
  }
  return totals;
}
