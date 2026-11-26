import { useEffect, useState } from 'react';

const PREFIX = 'reelstash-filter:';

function read(key, fallback) {
  try {
    const raw = localStorage.getItem(PREFIX + key);
    return raw == null ? fallback : JSON.parse(raw);
  } catch { return fallback; }
}

/**
 * Like useState, but the value is remembered in localStorage under `key` so it survives
 * reloads and carries over between pages (e.g. the studio/sort filters on Library, Favorites
 * and the Performers/Studios/Series/Categories directories).
 */
export function usePersistedState(key, initial) {
  const [value, setValue] = useState(() => read(key, initial));
  useEffect(() => {
    try { localStorage.setItem(PREFIX + key, JSON.stringify(value)); } catch { /* storage unavailable */ }
  }, [key, value]);
  return [value, setValue];
}
