// Display formatting shared by every console page. Byte counts are integers; sizes are formatted
// from them with integer maths, never stored as floats.
export { formatBytes, formatResolution } from '../../format.js';
export { timeAgo, timeUntil, formatDuration, parseTs } from '../../pages/timeUtil.js';

const numberFormat = new Intl.NumberFormat(undefined);
const compactFormat = new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 });

export function formatNumber(value) {
  if (value === null || value === undefined || Number.isNaN(value)) return '—';
  return numberFormat.format(value);
}

export function formatCompact(value) {
  if (value === null || value === undefined || Number.isNaN(value)) return '—';
  return value < 10_000 ? numberFormat.format(value) : compactFormat.format(value);
}

export function formatPercent(value, digits = 0) {
  if (value === null || value === undefined || Number.isNaN(value)) return '—';
  return `${value.toFixed(digits).replace(/\.0+$/, '')}%`;
}

export function formatHours(hours) {
  if (!hours) return '0 h';
  return hours >= 100 ? `${Math.round(hours).toLocaleString()} h` : `${hours.toFixed(1)} h`;
}

export const plural = (n, word, many) => `${formatNumber(n)} ${n === 1 ? word : (many || `${word}s`)}`;

export function formatDateTime(value) {
  if (!value) return '—';
  const normalized = String(value).replace(' ', 'T').replace(/([+-]\d{2})$/, '$1:00');
  const date = new Date(normalized);
  if (Number.isNaN(date.getTime())) return '—';
  return date.toLocaleString(undefined, { day: 'numeric', month: 'short', year: 'numeric', hour: 'numeric', minute: '2-digit' });
}

export function initials(name, email) {
  const source = (name || email || '?').trim();
  const parts = source.split(/[\s@._-]+/).filter(Boolean);
  if (name && parts.length >= 2) return `${parts[0][0]}${parts[1][0]}`.toUpperCase();
  return source.slice(0, 2).toUpperCase();
}

export const userLabel = user => user.displayName || user.username || (user.email ? user.email.split('@')[0] : `User ${user.id}`);

// Disk usage as a 0..1 fraction, or null before the first capacity reading.
export const usedFraction = disk => (disk.totalCapacity ? Math.min(1, (disk.usedCapacity || 0) / disk.totalCapacity) : null);
export const LOW_SPACE = 0.1;
export const isLowSpace = disk => !!disk.totalCapacity && disk.availableCapacity / disk.totalCapacity < LOW_SPACE;
