// Shared display formatters for admin pages.

export function formatBytes(bytes) {
  if (bytes == null || !Number.isFinite(bytes)) return '—';
  if (bytes === 0) return '0 B';
  const unit = Math.max(0, Math.min(Math.floor(Math.log(Math.abs(bytes)) / Math.log(1024)), 4));
  return `${(bytes / (1024 ** unit)).toFixed(unit ? 1 : 0)} ${['B', 'KB', 'MB', 'GB', 'TB'][unit]}`;
}

export function formatResolution(height) {
  if (!height) return '—';
  if (height >= 2160) return '4K';
  return `${height >= 1440 ? 1440 : height >= 1080 ? 1080 : height >= 720 ? 720 : height}p`;
}
