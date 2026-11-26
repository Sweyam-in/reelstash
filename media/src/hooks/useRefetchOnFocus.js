import { useEffect, useRef } from 'react';

// Re-runs `callback` when the user comes back to this browser tab, at most once per `minGapMs`.
// Replaces manual "Refresh" buttons on pages whose data can change while the tab is in the background.
export default function useRefetchOnFocus(callback, minGapMs = 30000) {
  const latest = useRef(callback);
  const lastRun = useRef(Date.now());
  latest.current = callback;
  useEffect(() => {
    const onReturn = () => {
      if (document.visibilityState === 'hidden') return;
      if (Date.now() - lastRun.current < minGapMs) return;
      lastRun.current = Date.now();
      latest.current();
    };
    document.addEventListener('visibilitychange', onReturn);
    window.addEventListener('focus', onReturn);
    return () => {
      document.removeEventListener('visibilitychange', onReturn);
      window.removeEventListener('focus', onReturn);
    };
  }, [minGapMs]);
}
