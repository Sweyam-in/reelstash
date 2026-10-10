// The choices behind the first-run setup wizard, kept apart from its screens so they can be tested.

export const SETUP_MODES = {
  entertainment: {
    label: 'Entertainment', defaultName: 'Movies & TV',
    blurb: 'Movies and TV shows with seasons, episodes, cast and crew, posters and genres.',
    provider: { id: 'tmdb', name: 'TMDB', keyLabel: 'API key or read access token', help: 'A free key from themoviedb.org → Settings → API.' },
  },
  private: {
    label: 'Private videos', defaultName: 'Videos',
    blurb: 'Personal videos organised by performer, studio and series. Can be locked behind a PIN.',
    // Metadata for private videos comes from a provider plugin, installed under Admin → Plugins.
    provider: { id: '', name: 'A provider plugin', plugin: true },
  },
};

export const modeInfo = mode => SETUP_MODES[mode] || SETUP_MODES.entertainment;

/** Steps in order; the wizard never skips one, but the provider step can be left empty. */
export const SETUP_STEPS = [
  { id: 'mode', title: 'Library type' },
  { id: 'folder', title: 'Media folder' },
  { id: 'metadata', title: 'Metadata' },
  { id: 'scan', title: 'First scan' },
];

/** A friendly disk name from a folder path: "/mnt/media/Films" → "Films". */
export function diskNameFromPath(path) {
  const parts = String(path || '').split('/').filter(Boolean);
  return parts.length ? parts[parts.length - 1] : 'Media';
}

/** Why the folder cannot be used yet, or '' when it can. */
export function folderProblem(check) {
  if (!check) return 'Check the folder first.';
  if (!check.exists || !check.isDir || !check.readable) return check.error || 'ReelStash cannot read this folder.';
  return '';
}

/** One line under the folder check: what a scan would find. */
export function folderSummary(check) {
  if (!check || folderProblem(check)) return '';
  const n = check.videoCount;
  if (n === 0) return 'No videos found yet. You can continue, and add files later.';
  return `${check.truncated ? 'At least ' : ''}${n.toLocaleString()} ${n === 1 ? 'video' : 'videos'} found.`;
}

/** Whole-number progress (0-100) of a scan row from /api/admin/scans, or null while unknown. */
export function scanPercent(scan) {
  if (!scan) return null;
  if (scan.completedAt) return 100;
  if (!scan.walkComplete || !scan.filesSeen) return null;
  return Math.min(99, Math.round((scan.filesProcessed / scan.filesSeen) * 100));
}

export function scanSummary(scan) {
  if (!scan) return 'Starting…';
  if (scan.completedAt) {
    const n = scan.discoveredCount || 0;
    return n === 0 ? 'Finished. No videos were found.' : `Finished. ${n.toLocaleString()} ${n === 1 ? 'video' : 'videos'} added.`;
  }
  if (!scan.walkComplete) return `Looking for videos… ${(scan.filesSeen || 0).toLocaleString()} found so far`;
  return `Reading videos… ${(scan.filesProcessed || 0).toLocaleString()} of ${(scan.filesSeen || 0).toLocaleString()}`;
}
