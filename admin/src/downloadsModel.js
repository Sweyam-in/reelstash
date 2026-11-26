// Pure helpers for the Downloads workspace: what a qBittorrent state means, which sidebar
// filter a torrent belongs to, and how speeds and times read.

import { formatBytes } from './format.js';

const STATES = {
  downloading: ['Downloading', 'info', 'downloading'],
  forcedDL: ['Downloading (forced)', 'info', 'downloading'],
  metaDL: ['Fetching metadata', 'info', 'downloading'],
  forcedMetaDL: ['Fetching metadata', 'info', 'downloading'],
  stalledDL: ['Stalled', 'warning', 'downloading'],
  queuedDL: ['Queued', 'neutral', 'downloading'],
  allocating: ['Allocating', 'info', 'downloading'],
  checkingDL: ['Checking', 'neutral', 'downloading'],
  checkingUP: ['Checking', 'neutral', 'seeding'],
  checkingResumeData: ['Checking', 'neutral', 'downloading'],
  uploading: ['Seeding', 'success', 'seeding'],
  stalledUP: ['Seeding', 'success', 'seeding'],
  forcedUP: ['Seeding (forced)', 'success', 'seeding'],
  queuedUP: ['Queued to seed', 'neutral', 'seeding'],
  pausedDL: ['Stopped', 'neutral', 'stopped'],
  stoppedDL: ['Stopped', 'neutral', 'stopped'],
  pausedUP: ['Completed', 'neutral', 'stopped'],
  stoppedUP: ['Completed', 'neutral', 'stopped'],
  moving: ['Moving', 'info', 'moving'],
  error: ['Error', 'danger', 'errored'],
  missingFiles: ['Files missing', 'danger', 'errored'],
};

/** { label, tone, group } for a torrent's qBittorrent state. */
export function torrentState(state) {
  const [label, tone, group] = STATES[state] || ['Unknown', 'neutral', 'other'];
  return { label, tone, group };
}

export const isStopped = t => torrentState(t.state).group === 'stopped';
export const isComplete = t => (t.progress ?? 0) >= 1;

function asNumber(value) {
  const n = typeof value === 'number' ? value : Number(value);
  return Number.isFinite(n) ? n : 0;
}

/** Sidebar filters, in order. */
export const FILTERS = [
  { id: 'all', label: 'All', test: () => true },
  { id: 'downloading', label: 'Downloading', test: t => torrentState(t.state).group === 'downloading' },
  { id: 'seeding', label: 'Seeding', test: t => torrentState(t.state).group === 'seeding' },
  { id: 'completed', label: 'Completed', test: isComplete },
  { id: 'stopped', label: 'Stopped', test: isStopped },
  { id: 'active', label: 'Active', test: t => asNumber(t.dlspeed) > 0 || asNumber(t.upspeed) > 0 },
  { id: 'stalled', label: 'Stalled', test: t => t.state === 'stalledDL' || t.state === 'stalledUP' },
  { id: 'errored', label: 'Errors', test: t => torrentState(t.state).group === 'errored' },
];

export function filterCounts(torrents) {
  const out = {};
  for (const f of FILTERS) out[f.id] = torrents.filter(f.test).length;
  return out;
}

export function torrentTags(t) {
  return (t.tags || '').split(',').map(s => s.trim()).filter(Boolean);
}

/**
 * The torrents a view shows: filter id, category ('' = any, '-' = none), tag and a search over
 * name, category, tags and info hash.
 */
export function visibleTorrents(torrents, { filter = 'all', category = '', tag = '', query = '' } = {}) {
  const f = FILTERS.find(x => x.id === filter) || FILTERS[0];
  const q = query.trim().toLowerCase();
  return torrents.filter(t => f.test(t)
    && (category === '' || (category === '-' ? !t.category : t.category === category))
    && (tag === '' || torrentTags(t).includes(tag))
    && (!q || `${t.name} ${t.category || ''} ${t.tags || ''} ${t.hash || ''}`.toLowerCase().includes(q)));
}

// qBittorrent reports 8640000 (100 days) for "never".
const ETA_INFINITE = 8640000;

// Order of the status badge, so a column click groups "Downloading" together
// instead of following the progress percent. Lower ranks are the active work.
const STATUS_RANK = {
  downloading: 0,
  forcedDL: 1,
  metaDL: 2,
  forcedMetaDL: 2,
  stalledDL: 3,
  queuedDL: 4,
  allocating: 5,
  checkingDL: 6,
  checkingResumeData: 6,
  checkingUP: 7,
  moving: 8,
  uploading: 9,
  forcedUP: 10,
  stalledUP: 11,
  queuedUP: 12,
  pausedUP: 13,
  stoppedUP: 13,
  pausedDL: 14,
  stoppedDL: 14,
  error: 15,
  missingFiles: 16,
};

/**
 * The value a column sorts on. null means the torrent has nothing to rank
 * (no time left, no ratio) and stays at the bottom in either direction.
 */
function sortPrimary(t, key) {
  switch (key) {
    case 'name': return (t.name || '').toLowerCase();
    case 'status': return STATUS_RANK[t.state] ?? Object.keys(STATUS_RANK).length;
    case 'size': return asNumber(t.size);
    case 'down': return asNumber(t.dlspeed);
    case 'up': return asNumber(t.upspeed);
    case 'peers': return asNumber(t.num_seeds) + asNumber(t.num_leechs);
    case 'added': return asNumber(t.added_on);
    case 'eta': {
      if (isComplete(t) || isStopped(t) || t.eta == null || t.eta === '') return null;
      const eta = asNumber(t.eta);
      if (eta < 0 || eta >= ETA_INFINITE) return null;
      return eta;
    }
    case 'ratio': {
      if (t.ratio == null || t.ratio === '') return null;
      const ratio = asNumber(t.ratio);
      return ratio < 0 ? null : ratio;
    }
    default: return asNumber(t.added_on);
  }
}

function comparePrimary(x, y, sign) {
  if (x == null && y == null) return 0;
  if (x == null) return 1;
  if (y == null) return -1;
  if (x < y) return -sign;
  if (x > y) return sign;
  return 0;
}

// The direction a column sorts in on its first click: names A→Z, status with
// downloads first, time left shortest first; sizes and speeds biggest first.
export const SORT_FIRST_DIR = {
  added: 'desc', name: 'asc', status: 'asc', size: 'desc', down: 'desc', up: 'desc',
  eta: 'asc', peers: 'desc', ratio: 'desc',
};
export const DEFAULT_SORT = { key: 'added', dir: 'desc' };

/** Sorts torrents by key and direction ('asc' | 'desc'; default: that column's first direction). */
export function sortTorrents(torrents, key = 'added', dir = SORT_FIRST_DIR[key] || 'desc') {
  const sign = dir === 'asc' ? 1 : -1;
  return [...torrents].sort((a, b) => {
    const byColumn = comparePrimary(sortPrimary(a, key), sortPrimary(b, key), sign);
    if (byColumn) return byColumn;
    // Same status: further along first, whichever way the column is pointing.
    if (key === 'status') {
      const byProgress = asNumber(b.progress) - asNumber(a.progress);
      if (byProgress) return byProgress;
    }
    const an = (a.name || '').toLowerCase();
    const bn = (b.name || '').toLowerCase();
    if (an < bn) return -1;
    if (an > bn) return 1;
    return String(a.hash || '').localeCompare(String(b.hash || ''));
  });
}

/** The next sort after clicking a column header: that column's first direction, then the reverse. */
export function nextSort(current, key) {
  const first = SORT_FIRST_DIR[key] || 'desc';
  if (!current || current.key !== key) return { key, dir: first };
  return { key, dir: current.dir === 'asc' ? 'desc' : 'asc' };
}

/** Bytes per second, or a dash when idle. Numeric strings from the API count too. */
export function formatSpeed(bytesPerSecond) {
  const n = typeof bytesPerSecond === 'number' ? bytesPerSecond : Number(bytesPerSecond);
  if (!Number.isFinite(n) || n === 0) return '—';
  return `${formatBytes(n)}/s`;
}

/** Bytes per second for a meter, with an explicit zero when nothing is moving. */
export function formatRate(bytesPerSecond) {
  const shown = formatSpeed(bytesPerSecond);
  return shown === '—' ? '0 B/s' : shown;
}

/** A compact time span: 45s, 12m, 3h 5m, 2d 4h; ∞ for never. */
export function formatSpan(seconds) {
  if (seconds == null || seconds < 0) return '—';
  if (seconds >= ETA_INFINITE) return '∞';
  const s = Math.round(seconds);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return m % 60 ? `${h}h ${m % 60}m` : `${h}h`;
  const d = Math.floor(h / 24);
  return h % 24 ? `${d}d ${h % 24}h` : `${d}d`;
}

/** What the ETA column shows: only while downloading. */
export function etaLabel(t) {
  if (isComplete(t) || isStopped(t)) return '';
  return formatSpan(t.eta);
}

/** When a torrent was added: a short age, then a date after a week. */
export function formatAdded(unix, now = Date.now()) {
  const n = asNumber(unix);
  if (n <= 0) return '—';
  const seconds = Math.round(now / 1000 - n);
  if (seconds < 60) return 'Just now';
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  if (seconds < 7 * 86400) return `${Math.floor(seconds / 86400)}d ago`;
  const added = new Date(n * 1000);
  const today = new Date(now);
  return added.toLocaleDateString(undefined, {
    month: 'short', day: 'numeric', year: added.getFullYear() === today.getFullYear() ? undefined : 'numeric',
  });
}

export const formatRatio = r => (r == null || r < 0 ? '—' : r >= 100 ? '∞' : r.toFixed(2));

export const formatPercent = p => `${Math.floor((p || 0) * 1000) / 10}%`;

/** A bytes/s limit for the settings form, in KiB/s (0 = unlimited). */
export const toKiB = bytes => Math.round((bytes || 0) / 1024);
export const fromKiB = kib => Math.max(0, Math.round(Number(kib) || 0)) * 1024;

/** How a move record reads in the list. */
export function moveLabel(move) {
  if (!move) return null;
  switch (move.status) {
    case 'MOVING': return { label: 'Moving…', tone: 'info' };
    case 'MOVED': return { label: move.ruleName ? `Moved · ${move.ruleName}` : 'Moved', tone: 'success' };
    case 'FAILED': return { label: 'Move failed', tone: 'danger' };
    case 'REMOVED': return { label: 'Removed', tone: 'neutral' };
    case 'SKIPPED': return { label: move.message || 'Not moved', tone: 'neutral' };
    default: return null;
  }
}

/** A folder location as "Disk › path". */
export function folderLabel(loc) {
  if (!loc) return '';
  if (!loc.storageLocationId) return loc.path || '';
  return [loc.storageLocation, loc.path].filter(Boolean).join(' › ') || loc.storageLocation;
}

/** The Files workspace URL of a folder location. */
export function filesHref(loc) {
  if (!loc?.storageLocationId) return null;
  const parts = (loc.path || '').split('/').filter(Boolean).map(encodeURIComponent);
  return `/files/${loc.storageLocationId}${parts.length ? `/${parts.join('/')}` : ''}`;
}

/** Totals for the selection bar and status line. */
export function totals(torrents) {
  return torrents.reduce((acc, t) => {
    acc.size += asNumber(t.size);
    acc.down += asNumber(t.dlspeed);
    acc.up += asNumber(t.upspeed);
    return acc;
  }, { size: 0, down: 0, up: 0 });
}
