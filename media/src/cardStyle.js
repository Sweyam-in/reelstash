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

// Grid density: how big title cards are in library grids, on this device.
const DENSITY_KEY = 'reelstash-card-density';
export const CARD_DENSITY_EVENT = 'reelstash:card-density';
export const DENSITIES = ['compact', 'comfortable', 'large'];
const DENSITY_SCALE = { compact: 0.8, comfortable: 1, large: 1.3 };

export function readCardDensity() {
  try {
    const value = localStorage.getItem(DENSITY_KEY);
    return DENSITIES.includes(value) ? value : 'comfortable';
  } catch {
    return 'comfortable';
  }
}

export function saveCardDensity(density) {
  const next = DENSITIES.includes(density) ? density : 'comfortable';
  try { localStorage.setItem(DENSITY_KEY, next); } catch { /* storage blocked */ }
  window.dispatchEvent(new Event(CARD_DENSITY_EVENT));
}

export function useCardDensity() {
  const [density, setDensity] = useState(readCardDensity);
  useEffect(() => {
    const sync = () => setDensity(readCardDensity());
    window.addEventListener(CARD_DENSITY_EVENT, sync);
    window.addEventListener('storage', sync);
    return () => {
      window.removeEventListener(CARD_DENSITY_EVENT, sync);
      window.removeEventListener('storage', sync);
    };
  }, []);
  return [density, value => { setDensity(DENSITIES.includes(value) ? value : 'comfortable'); saveCardDensity(value); }];
}

export function cardMetrics(style, density = 'comfortable') {
  const posters = style !== 'thumbnail';
  const scale = DENSITY_SCALE[density] || 1;
  return {
    aspect: posters ? POSTER_RATIO : THUMB_RATIO,
    wideColumn: Math.round((posters ? 150 : 210) * scale),
    narrowColumn: Math.round((posters ? 118 : 145) * scale),
  };
}
