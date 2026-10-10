import { formatBytes } from '../format.js';
import { Building2, Captions, Clapperboard, Copy, Film, FolderSync, Images, ScanLine, Sparkles, Tags, Trash2, Users, Wrench } from 'lucide-react';

// Everything an admin can start by hand. `perLibrary` tasks run against one
// library (across every disk it has folders on); the rest cover everything.
export const JOB_KINDS = {
  SCAN: { title: 'Scan library', icon: ScanLine, perLibrary: true, description: 'Walk a library\'s folders, index new and changed videos, and delete missing ones from the library.' },
  METADATA_MATCH: { title: 'Match metadata', icon: Sparkles, perLibrary: true, description: 'Look up metadata from your Metadata Providers for indexed files that only have a filename title. Runs automatically after each scan.' },
  ORGANIZE: { title: 'Organize files', icon: FolderSync, perLibrary: true, description: 'Move a library\'s files into its studio and performer folders.' },
  DUPLICATE_SCAN: { title: 'Find duplicates', icon: Copy, description: 'Hash full files across every location to find identical copies.' },
  LINK_RECOVERY: { title: 'Find provider links', icon: Sparkles, description: 'Find the Metadata Provider record for titles that were matched earlier but carry no provider link. Only links are added; files and details are not changed.' },
  PERFORMER_SYNC: { title: 'Sync people', icon: Users, description: 'Refresh profiles and photos from your Metadata Providers.' },
  STUDIO_SYNC: { title: 'Sync studios', icon: Building2, description: 'Refresh studio details and logos from your Metadata Providers.' },
  SPRITE_SCAN: { title: 'Build scrub previews', icon: Images, description: 'Make the thumbnails shown when you hover the seek bar. Titles get one automatically the first time they play; this does the rest in one go.' },
  PREVIEW_SCAN: { title: 'Build hover previews', icon: Film, description: 'Make the short clips that play when you rest the pointer on a title card.' },
  REMOVE_MISSING: { title: 'Remove missing titles', icon: Trash2, confirm: 'Remove every title whose file is missing from disk? Their watch history, ratings and playlist entries go with them. Files on disk are not touched.', description: 'Clear out titles whose files were deleted or moved away and are still missing after a scan. A title that still has another available copy only loses the missing one. Files on disk are never touched.' },
  THUMBNAIL_FIX: { title: 'Fix black thumbnails', icon: Wrench, description: 'Find thumbnails that are black or blank and replace them with a better frame from the video.' },
  TRACK_SCAN: { title: 'Read audio & subtitle tracks', icon: Captions, description: 'Read the audio and subtitle tracks of every video that has none recorded yet, and count the subtitle files beside each video, so Videos can filter by them. Runs automatically after each scan.' },
  MP4_REPAIR: { title: 'Fix slow-starting videos', icon: Wrench, description: 'Find MP4 files whose video data is split into many pieces, which makes browsers read most of the file before playing, and rewrite them in place as one piece. Nothing is re-encoded. Runs automatically after each scan.' },
};
export const RUNNABLE_KINDS = Object.keys(JOB_KINDS);

// Housekeeping kinds the API hides from the list but that can still be looked up by id.
const OTHER_TITLES = { THUMBNAIL: 'Generate thumbnail', NAMER: 'Match one file', PERFORMER_MATCH: 'Match person', STUDIO_MATCH: 'Match studio', SPRITE: 'Build scrub preview', PREVIEW: 'Build hover preview', HOUSEKEEPING: 'Housekeeping', FILE_TRANSFER: 'Paste files' };
export const jobTitle = kind => JOB_KINDS[kind]?.title || OTHER_TITLES[kind] || kind;

// A library scan names the library and the disk; other jobs name their disk.
export function jobName(job) {
  const where = job.libraryName && job.kind === 'SCAN'
    ? (job.storageLocationName ? `${job.libraryName} (${job.storageLocationName})` : job.libraryName)
    : job.storageLocationName;
  return where ? `${jobTitle(job.kind)} · ${where}` : jobTitle(job.kind);
}
export const jobIcon = kind => JOB_KINDS[kind]?.icon || Tags;
export const isActive = job => job.status === 'PENDING' || job.status === 'RUNNING';

const SCAN_PHASE = {
  queued: 'Starting scan',
  walking: 'Searching the disk',
  indexing: 'Indexing files',
  finishing: 'Checking for missing files',
  done: 'Scan finished',
};

export function fileName(path) {
  if (!path) return '';
  const parts = String(path).split('/');
  return parts[parts.length - 1] || path;
}

// { percent: number|null (null = indeterminate), label, detail }
export function jobProgress(job) {
  if (job.status === 'PENDING') return { percent: null, label: 'Waiting in queue', detail: '' };
  const scan = job.scan;
  if (job.kind === 'SCAN' && scan) {
    const label = SCAN_PHASE[scan.phase] || 'Scanning';
    const detail = fileName(scan.currentPath);
    if (scan.phase === 'finishing') return { percent: 100, label, detail: '' };
    if (scan.walkComplete && scan.filesSeen > 0) {
      return { percent: Math.min(100, Math.round((scan.filesProcessed / scan.filesSeen) * 100)), label: `${label} · ${scan.filesProcessed} of ${scan.filesSeen}`, detail };
    }
    return { percent: null, label: scan.filesSeen ? `${label} · ${scan.filesSeen} found` : label, detail };
  }
  if (job.kind === 'FILE_TRANSFER' && job.progressTotal > 0) {
    // Progress is in KiB.
    return { percent: Math.min(100, Math.round((job.progressDone / job.progressTotal) * 100)), label: `${formatBytes(job.progressDone * 1024)} of ${formatBytes(job.progressTotal * 1024)}`, detail: fileName(job.progressLabel) };
  }
  if (job.progressTotal > 0) {
    return { percent: Math.min(100, Math.round((job.progressDone / job.progressTotal) * 100)), label: `${job.progressDone} of ${job.progressTotal}`, detail: fileName(job.progressLabel) };
  }
  return { percent: null, label: 'Working…', detail: fileName(job.progressLabel) };
}

// One line describing what a finished job produced.
export function resultSummary(job) {
  if (job.status === 'FAILED') return job.error || 'Failed';
  if (job.status === 'CANCELLED') return 'Stopped by user';
  if (isActive(job)) return '';
  const r = job.result;
  if (job.kind === 'SCAN' && job.scan) {
    const { filesSeen, discovered, changed, missing } = job.scan;
    // A library scan deletes missing files instead of flagging them.
    return `${filesSeen} files · ${discovered} new · ${changed} changed · ${missing} ${job.libraryId ? 'removed' : 'missing'}`;
  }
  if (job.kind === 'METADATA_MATCH' && r) return r.total ? `${r.matched} of ${r.total} looked up${r.failed ? ` · ${r.failed} failed` : ''}` : 'Nothing to match';
  if (job.kind === 'REMOVE_MISSING' && r) {
    const parts = [`${r.removedTitles ?? 0} titles removed`, `${r.removedFiles ?? 0} missing files cleared`];
    if (r.kept) parts.push(`${r.kept} back on disk`);
    if (r.skippedLocationCount) parts.push(`skipped ${r.skippedLocations.join(', ')} (unreadable or scan not finished)`);
    return parts.join(' · ');
  }
  if (job.kind === 'TRACK_SCAN' && r) {
    const parts = [`${r.checked ?? 0} checked`, `${r.probed ?? 0} read`, `${r.withSubtitles ?? 0} with subtitles`];
    if (r.failed) parts.push(`${r.failed} unreadable`);
    return parts.join(' · ');
  }
  if (job.kind === 'MP4_REPAIR' && r) {
    const parts = [`${r.checked ?? 0} checked`, `${r.repaired ?? 0} rewritten`];
    if (r.failed) parts.push(`${r.failed} failed`);
    if (r.skippedLocations?.length) parts.push(`skipped ${r.skippedLocations.join(', ')} (scan or organizer running)`);
    return parts.join(' · ');
  }
  if (job.kind === 'FILE_TRANSFER' && r) {
    const parts = [`${r.mode === 'move' ? 'Moved' : 'Copied'} ${r.transferred ?? 0}`, formatBytes(r.bytes ?? 0)];
    if (r.failed) parts.push(`${r.failed} failed`);
    return parts.join(' · ');
  }
  if (job.kind === 'HOUSEKEEPING' && r) {
    const removed = `${(r.count ?? 0).toLocaleString()} ${r.count === 1 ? 'item' : 'items'}`;
    return r.dryRun ? `Analysis · ${removed} found` : `${removed} removed · ${formatBytes(r.bytes ?? 0)} freed`;
  }
  if (r && typeof r === 'object') {
    const parts = Object.entries(r).filter(([, v]) => typeof v === 'number' || typeof v === 'string').slice(0, 3).map(([k, v]) => `${v} ${k.replace(/([A-Z])/g, ' $1').toLowerCase()}`);
    if (parts.length) return parts.join(' · ');
  }
  return 'Completed';
}
