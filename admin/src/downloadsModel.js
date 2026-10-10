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

/** Sidebar filters, in order. */
export const FILTERS = [
  { id: 'all', label: 'All', test: () => true },
  { id: 'downloading', label: 'Downloading', test: t => torrentState(t.state).group === 'downloading' },
  { id: 'seeding', label: 'Seeding', test: t => torrentState(t.state).group === 'seeding' },
  { id: 'completed', label: 'Completed', test: isComplete },
  { id: 'stopped', label: 'Stopped', test: isStopped },
  { id: 'active', label: 'Active', test: t => (t.dlspeed || 0) > 0 || (t.upspeed || 0) > 0 },
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
 * name, category and tags.
 */
export function visibleTorrents(torrents, { filter = 'all', category = '', tag = '', query = '' } = {}) {
  const f = FILTERS.find(x => x.id === filter) || FILTERS[0];
  const q = query.trim().toLowerCase();
  return torrents.filter(t => f.test(t)
    && (category === '' || (category === '-' ? !t.category : t.category === category))
    && (tag === '' || torrentTags(t).includes(tag))
    && (!q || `${t.name} ${t.category || ''} ${t.tags || ''}`.toLowerCase().includes(q)));
}

// Sort keys: each reads a value that sorts ascending naturally.
const SORTS = {
  added: t => t.added_on || 0,
  name: t => (t.name || '').toLowerCase(),
  status: t => t.progress || 0,
  size: t => t.size || 0,
  down: t => t.dlspeed || 0,
  up: t => t.upspeed || 0,
  eta: t => (isComplete(t) || isStopped(t) || !(t.eta >= 0) ? Infinity : t.eta),
  ratio: t => t.ratio || 0,
};
// The direction a column sorts in on its first click: names A→Z and time left
// shortest first; everything else biggest first.
export const SORT_FIRST_DIR = { added: 'desc', name: 'asc', status: 'desc', size: 'desc', down: 'desc', up: 'desc', eta: 'asc', ratio: 'desc' };
export const DEFAULT_SORT = { key: 'added', dir: 'desc' };

/** Sorts torrents by key and direction ('asc' | 'desc'; default: that column's first direction). */
export function sortTorrents(torrents, key = 'added', dir = SORT_FIRST_DIR[key] || 'desc') {
  const pick = SORTS[key] || SORTS.added;
  const sign = dir === 'asc' ? 1 : -1;
  return [...torrents].sort((a, b) => {
    const x = pick(a), y = pick(b);
    return x < y ? -sign : x > y ? sign : 0;
  });
}

/** The next sort after clicking a column header: first direction, then reversed, then back to newest first. */
export function nextSort(current, key) {
  const first = SORT_FIRST_DIR[key] || 'desc';
  if (current.key !== key) return { key, dir: first };
  if (current.dir === first) return { key, dir: first === 'asc' ? 'desc' : 'asc' };
  return DEFAULT_SORT;
}

/** Bytes per second, or a dash when idle. */
export function formatSpeed(bytesPerSecond) {
  if (!bytesPerSecond) return '—';
  return `${formatBytes(bytesPerSecond)}/s`;
}

// qBittorrent reports 8640000 (100 days) for "never".
const ETA_INFINITE = 8640000;

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
    acc.size += t.size || 0;
    acc.down += t.dlspeed || 0;
    acc.up += t.upspeed || 0;
    return acc;
  }, { size: 0, down: 0, up: 0 });
}
