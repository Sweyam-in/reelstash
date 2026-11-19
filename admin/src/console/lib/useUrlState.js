import { useCallback, useMemo } from 'react';
import { useSearchParams } from 'react-router-dom';

// Filter, sort and page state kept in the URL so it survives navigation and reloads and can be
// shared. Defaults are left out of the URL to keep links short. Any change other than `page`
// resets the page.
export function useUrlState(defaults) {
  const [params, setParams] = useSearchParams();
  const state = useMemo(() => {
    const next = { ...defaults };
    for (const key of Object.keys(defaults)) {
      const value = params.get(key);
      if (value !== null) next[key] = value;
    }
    return next;
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params]);

  const update = useCallback((patch, { keepPage = false } = {}) => {
    setParams(prev => {
      const next = new URLSearchParams(prev);
      for (const [key, value] of Object.entries(patch)) {
        if (value === undefined || value === null || value === '' || value === defaults[key]) next.delete(key);
        else next.set(key, String(value));
      }
      if (!keepPage && !('page' in patch) && 'page' in defaults) next.delete('page');
      return next;
    }, { replace: true });
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [setParams]);

  const reset = useCallback(() => {
    setParams(prev => {
      const next = new URLSearchParams(prev);
      for (const key of Object.keys(defaults)) next.delete(key);
      return next;
    }, { replace: true });
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [setParams]);

  return [state, update, reset];
}
