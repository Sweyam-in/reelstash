import { useEffect, useState } from 'react';

// One choice for every title card: posters (2:3) or thumbnails (16:10).
const KEY = 'reelstash-card-style';
export const CARD_STYLE_EVENT = 'reelstash:card-style';

export function readCardStyle() {
  try {
    return localStorage.getItem(KEY) === 'thumbnail' ? 'thumbnail' : 'poster';
  } catch {
    return 'poster';
  }
}

export function saveCardStyle(style) {
  const next = style === 'thumbnail' ? 'thumbnail' : 'poster';
  try { localStorage.setItem(KEY, next); } catch { /* storage blocked */ }
  window.dispatchEvent(new Event(CARD_STYLE_EVENT));
}

export function useCardStyle() {
  const [style, setStyle] = useState(readCardStyle);
  useEffect(() => {
    const sync = () => setStyle(readCardStyle());
    window.addEventListener(CARD_STYLE_EVENT, sync);
    window.addEventListener('storage', sync);
    return () => {
      window.removeEventListener(CARD_STYLE_EVENT, sync);
      window.removeEventListener('storage', sync);
    };
  }, []);
  return [style, value => { setStyle(value === 'thumbnail' ? 'thumbnail' : 'poster'); saveCardStyle(value); }];
}

export const POSTER_RATIO = 2 / 3;
export const THUMB_RATIO = 16 / 10;

export function cardMetrics(style) {
  const posters = style !== 'thumbnail';
  return {
    aspect: posters ? POSTER_RATIO : THUMB_RATIO,
    wideColumn: posters ? 150 : 210,
    narrowColumn: posters ? 118 : 145,
  };
}
