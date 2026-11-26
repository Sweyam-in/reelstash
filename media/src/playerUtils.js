/** 83 -> "1:23", 3725 -> "1:02:05". Bad input reads as "0:00". */
export function formatClock(seconds) {
  const total = Number.isFinite(seconds) && seconds > 0 ? Math.floor(seconds) : 0;
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}` : `${m}:${String(s).padStart(2, '0')}`;
}

/** Where along a bar (0..1) a pointer at clientX sits, given the bar's bounding rect. */
export function fractionAt(clientX, rect) {
  if (!rect || !rect.width) return 0;
  return Math.min(1, Math.max(0, (clientX - rect.left) / rect.width));
}

/**
 * Which tile of a sprite sheet shows this moment, as CSS background offsets.
 * `sprite` is the server's descriptor: { intervalSeconds, tileWidth, tileHeight, columns, count }.
 */
export function spriteTile(sprite, seconds) {
  const index = Math.max(0, Math.min(sprite.count - 1, Math.floor(seconds / sprite.intervalSeconds)));
  return {
    x: (index % sprite.columns) * sprite.tileWidth,
    y: Math.floor(index / sprite.columns) * sprite.tileHeight,
    width: sprite.tileWidth,
    height: sprite.tileHeight,
  };
}

/** The marker closest to `seconds` within `toleranceSeconds`, or null. */
export function markerNear(markers, seconds, toleranceSeconds) {
  let best = null;
  let bestGap = Infinity;
  for (const marker of markers) {
    const gap = Math.abs(marker.seconds - seconds);
    if (gap <= toleranceSeconds && gap < bestGap) { best = marker; bestGap = gap; }
  }
  return best;
}

/** Keeps a floating label of `width` px fully inside a bar of `barWidth` px, centred on `x` where it can be. */
export function clampLabel(x, width, barWidth) {
  if (barWidth <= width) return 0;
  return Math.min(barWidth - width, Math.max(0, x - width / 2));
}

/**
 * Mean luma (0-255) of RGBA pixel data. Used to warn that a poster is black
 * before the person has to squint at it.
 */
export function meanLuma(rgba) {
  let sum = 0;
  let n = 0;
  for (let i = 0; i + 2 < rgba.length; i += 4) {
    sum += 0.299 * rgba[i] + 0.587 * rgba[i + 1] + 0.114 * rgba[i + 2];
    n += 1;
  }
  return n ? sum / n : 0;
}

export const BLACK_LUMA = 18;
