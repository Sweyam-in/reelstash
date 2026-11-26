// Photo-less performers/studios get a monogram whose colour is derived from the name,
// so the same person always looks the same and a grid of them reads as varied, not as a wall of one colour.
const HUES = [8, 24, 44, 150, 172, 198, 218, 244, 268, 292, 322, 344];

export function monogram(name = '') {
  const clean = String(name).trim();
  const words = clean.split(/\s+/).filter(Boolean);
  const initials = (words.length > 1 ? words[0][0] + words[words.length - 1][0] : clean.slice(0, 2) || '?').toUpperCase();
  let hash = 0;
  for (const ch of clean.toLowerCase()) hash = (hash * 31 + ch.charCodeAt(0)) >>> 0;
  return { initials, style: { '--mg-h': HUES[hash % HUES.length] } };
}
