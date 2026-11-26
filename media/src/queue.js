import { useSyncExternalStore } from 'react';

/**
 * The play queue: an ordered list of titles to watch next, kept in
 * localStorage so it survives navigation and reloads. `index` is the title
 * currently playing; "Up next" is everything after it.
 */
const KEY = 'reelstash-queue-v1';
const MAX = 500;

const empty = { items: [], index: -1, autoplay: true };
let state = load();
const listeners = new Set();

function load() {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) || 'null');
    if (raw && Array.isArray(raw.items)) {
      return { items: raw.items.filter(item => item && item.id != null).slice(0, MAX), index: Number.isInteger(raw.index) ? raw.index : -1, autoplay: raw.autoplay !== false };
    }
  } catch { /* unreadable or unavailable storage: start empty */ }
  return empty;
}

function commit(next) {
  state = next;
  try { localStorage.setItem(KEY, JSON.stringify(state)); } catch { /* storage full or blocked */ }
  listeners.forEach(fn => fn());
}

const slim = item => ({ id: item.id, title: item.title, studioName: item.studioName || '' });
const sameId = (a, b) => String(a) === String(b);

export function shuffled(list, random = Math.random) {
  const copy = [...list];
  for (let i = copy.length - 1; i > 0; i -= 1) {
    const j = Math.floor(random() * (i + 1));
    [copy[i], copy[j]] = [copy[j], copy[i]];
  }
  return copy;
}

/** Replaces the queue and points at the first title. Returns that title, or null when empty. */
export function startQueue(items) {
  const list = items.map(slim).slice(0, MAX);
  commit({ ...state, items: list, index: list.length ? 0 : -1 });
  return list[0] || null;
}

/** Appends a title unless it is already queued. Returns true if it was added. */
export function enqueue(item) {
  if (state.items.some(queued => sameId(queued.id, item.id))) return false;
  if (state.items.length >= MAX) return false;
  commit({ ...state, items: [...state.items, slim(item)] });
  return true;
}

/** Inserts a title right after the one playing (moving it if it was already queued). Returns false if it is the one playing. */
export function playNext(item) {
  const playingId = state.items[state.index]?.id;
  if (playingId != null && sameId(playingId, item.id)) return false;
  const items = state.items.filter(queued => !sameId(queued.id, item.id));
  const playingAt = playingId == null ? -1 : items.findIndex(queued => sameId(queued.id, playingId));
  items.splice(playingAt + 1, 0, slim(item));
  commit({ ...state, items, index: playingAt });
  return true;
}

export function removeFromQueue(id) {
  const playingId = state.items[state.index]?.id;
  const items = state.items.filter(queued => !sameId(queued.id, id));
  const index = playingId == null ? -1 : items.findIndex(queued => sameId(queued.id, playingId));
  commit({ ...state, items, index });
}

export function clearQueue() { commit({ ...state, items: [], index: -1 }); }
export function setAutoplay(autoplay) { commit({ ...state, autoplay }); }

/** Marks a title as the one playing, if it is in the queue (opening it from anywhere else leaves the queue alone). */
export function syncPlaying(id) {
  const index = state.items.findIndex(queued => sameId(queued.id, id));
  if (index !== state.index && index >= 0) commit({ ...state, index });
}

/** The title after the one playing, or null. */
export function nextInQueue() {
  return state.index >= 0 ? state.items[state.index + 1] || null : null;
}

/** The title before the one playing, or null. */
export function previousInQueue() {
  return state.index > 0 ? state.items[state.index - 1] || null : null;
}

export function useQueue() {
  return useSyncExternalStore(
    fn => { listeners.add(fn); return () => listeners.delete(fn); },
    () => state,
  );
}

// Test hook: reset module state between cases.
export function __resetQueueForTests() { state = empty; listeners.clear(); }
