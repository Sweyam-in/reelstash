import { useCallback, useEffect, useRef, useState } from 'react';

// Loads data for a page: `loading` is true only before the first result, `refreshing` while a
// later request is in flight, and only the latest request may write state, so a slow earlier
// filter can't overwrite a newer one. `interval` polls, paused while the tab is hidden.
export function useResource(fetcher, deps, { interval = 0, enabled = true } = {}) {
  const [state, setState] = useState({ data: null, error: null, loading: enabled, refreshing: false });
  const latest = useRef(0);
  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;

  const load = useCallback(() => {
    const id = ++latest.current;
    setState(s => ({ ...s, refreshing: true, loading: s.data === null && s.error === null }));
    return Promise.resolve()
      .then(() => fetcherRef.current())
      .then(data => { if (id === latest.current) setState({ data, error: null, loading: false, refreshing: false }); })
      .catch(error => { if (id === latest.current) setState(s => ({ ...s, error, loading: false, refreshing: false })); });
  }, []);

  useEffect(() => {
    if (!enabled) return undefined;
    load();
    return () => { latest.current += 1; };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, load, ...deps]);

  useEffect(() => {
    if (!enabled || !interval) return undefined;
    const timer = setInterval(() => { if (document.visibilityState !== 'hidden') load(); }, interval);
    return () => clearInterval(timer);
  }, [enabled, interval, load]);

  // Local edits (optimistic updates) go through setData; the next load replaces them.
  const setData = useCallback(updater => setState(s => ({ ...s, data: typeof updater === 'function' ? updater(s.data) : updater })), []);
  return { ...state, reload: load, setData };
}
