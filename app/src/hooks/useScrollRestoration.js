import { useEffect, useRef } from 'react';
import { useLocation, useNavigationType } from 'react-router-dom';

// In-memory cache first (works across StrictMode remounts within the tab), sessionStorage as a
// backstop so a reload of the same history entry still restores.
const cache = new Map();
const MAX_ATTEMPTS = 30;

function scrollHost() {
  // The AppShell's own wrapper (.spk-app-main, which holds the sticky top bar and the
  // <main class="spk-app-content">) is what actually scrolls — not the window, and not
  // .spk-app-content itself.
  return document.querySelector('.spk-app-main') || document.scrollingElement;
}

function storageKey(key) { return `rs-scroll:${key}`; }

function readSaved(key) {
  if (cache.has(key)) return cache.get(key);
  try { const raw = sessionStorage.getItem(storageKey(key)); return raw != null ? Number(raw) : null; } catch { return null; }
}

function writeSaved(key, value) {
  cache.set(key, value);
  try { sessionStorage.setItem(storageKey(key), String(value)); } catch { /* private mode, ignore */ }
}

/**
 * Remembers the scroll position of a list page (Library, Favorites, Performers, Studios, …) and
 * restores it when the person comes back with the browser/gesture Back button, so they land where
 * they were instead of at the top. Restoring retries as the infinite list streams more rows in,
 * since the saved offset may be further down than what's rendered yet.
 *
 * An in-app "back" link (e.g. the title page's "← Library") is a fresh navigation, not a POP, so
 * it has no history entry to restore from. Such links pass `state={{ restoreScroll: true }}`, and
 * the position last saved for that same path + query is restored instead.
 *
 * `active` should be true once the page's content is in the DOM (so there's something to scroll).
 * `deps` should include whatever grows as more items load (e.g. `items.length`), so a restore
 * attempt re-fires each time the list gets taller.
 */
export function useScrollRestoration(active, deps = []) {
  const location = useLocation();
  const navType = useNavigationType();
  const key = location.key;
  const pathKey = `path:${location.pathname}${location.search}`;
  const restoreByPath = navType !== 'POP' && Boolean(location.state?.restoreScroll);
  const attemptsRef = useRef(0);
  const doneRef = useRef(false);

  useEffect(() => { attemptsRef.current = 0; doneRef.current = false; }, [key]);

  // Save continuously while the page is showing. Every scroll tick is recorded (not just on
  // unmount): React StrictMode's dev-only double-invoke fires this effect's cleanup right after
  // mount, and a save-on-cleanup would stomp the real position with whatever's on screen at that
  // instant (usually 0, before a restore has even run).
  useEffect(() => {
    if (!active) return undefined;
    const el = scrollHost();
    if (!el) return undefined;
    const save = () => { writeSaved(key, el.scrollTop); writeSaved(pathKey, el.scrollTop); };
    el.addEventListener('scroll', save, { passive: true });
    return () => el.removeEventListener('scroll', save);
  }, [active, key, pathKey]);

  // Restore on the way back, retrying as more rows load in.
  useEffect(() => {
    if (!active || (navType !== 'POP' && !restoreByPath) || doneRef.current) return undefined;
    const saved = restoreByPath ? readSaved(pathKey) : (readSaved(key) ?? readSaved(pathKey));
    if (!saved) { doneRef.current = true; return undefined; }
    const el = scrollHost();
    if (!el) return undefined;
    el.scrollTop = saved;
    attemptsRef.current += 1;
    if (el.scrollTop >= saved - 2 || attemptsRef.current >= MAX_ATTEMPTS) doneRef.current = true;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active, key, pathKey, navType, restoreByPath, ...deps]);
}
