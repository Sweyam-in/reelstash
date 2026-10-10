import { formatBytes } from './format.js';

// Pure helpers for FILE_TRANSFER jobs (the file manager's background paste).
// Progress is stored in KiB; payload is { mode, items, destPath, ... }.

export const TRANSFER_KIND = 'FILE_TRANSFER';
export const isActiveTransfer = job => job.status === 'PENDING' || job.status === 'RUNNING';

const plural = n => `${n} item${n === 1 ? '' : 's'}`;

function where(payload) {
  const dest = payload?.destPath ? payload.destPath.split('/').pop() : '';
  return dest ? ` to ${dest}` : '';
}

// One line naming the paste: "Moving 3 items to Movies" / "Copied a.mkv".
export function transferTitle(job) {
  const p = job.payload || {};
  const items = p.items || [];
  const what = items.length === 1 ? items[0] : plural(items.length);
  const move = p.mode === 'move';
  const verb = isActiveTransfer(job) ? (move ? 'Moving' : 'Copying') : (move ? 'Moved' : 'Copied');
  if (job.status === 'FAILED') return `${move ? 'Move' : 'Copy'} failed`;
  if (job.status === 'CANCELLED') return `${move ? 'Move' : 'Copy'} stopped`;
  return `${verb} ${what}${where(p)}`;
}

// { percent: number|null (null = indeterminate), detail }
export function transferProgress(job) {
  if (job.status === 'PENDING') return { percent: null, detail: 'Waiting to start' };
  if (job.status === 'RUNNING') {
    if (!(job.progressTotal > 0)) return { percent: null, detail: job.progressLabel || 'Starting' };
    const percent = Math.min(100, Math.round((job.progressDone / job.progressTotal) * 100));
    const bytes = `${formatBytes(job.progressDone * 1024)} of ${formatBytes(job.progressTotal * 1024)}`;
    return { percent, detail: job.progressLabel ? `${bytes} · ${job.progressLabel}` : bytes };
  }
  if (job.status === 'FAILED') return { percent: null, detail: job.error || 'Failed' };
  if (job.status === 'CANCELLED') return { percent: null, detail: 'Stopped by you. Items already done stay where they landed.' };
  const r = job.result || {};
  const failed = r.failures?.length ? ` · ${r.failures.length} failed: ${r.failures[0]}` : '';
  return { percent: 100, detail: `${plural(r.transferred ?? 0)} · ${formatBytes(r.bytes ?? 0)}${failed}` };
}

// Overall percent across running transfers (null when none has a size yet).
export function overallPercent(jobs) {
  let done = 0;
  let total = 0;
  for (const job of jobs) {
    if (job.status === 'RUNNING' && job.progressTotal > 0) { done += job.progressDone; total += job.progressTotal; }
  }
  return total > 0 ? Math.min(100, Math.round((done / total) * 100)) : null;
}

// Toast for a transfer that just finished.
export function transferToast(job) {
  const r = job.result || {};
  if (job.status === 'FAILED') return { title: transferTitle(job), description: job.error, tone: 'danger' };
  if (job.status === 'CANCELLED') return { title: transferTitle(job), tone: 'warn' };
  const failures = r.failures || [];
  return {
    title: transferTitle(job),
    description: failures.length ? `${failures.length} failed: ${failures[0]}` : undefined,
    tone: failures.length ? 'warn' : 'success',
  };
}
