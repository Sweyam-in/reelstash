import { useEffect, useRef, useState } from 'react';
import { getBackgroundJobs } from '../../api';
import { useEventsConnected, useServerEvent } from '../../events';
import { isActive } from '../../pages/jobKinds';

// The background-job list. The server pushes a fresh snapshot whenever a job changes, so the
// page updates the moment something happens; polling is only the fallback while that connection
// is down (fast while something runs, slow otherwise).
export function useJobs({ limit = 100, enabled = true } = {}) {
  const [state, setState] = useState({ data: null, error: null });
  const connected = useEventsConnected();
  const active = useRef(false);
  const connectedRef = useRef(connected);
  connectedRef.current = connected;

  const apply = snapshot => { active.current = (snapshot.items || []).some(isActive); setState({ data: snapshot, error: null }); };
  const load = () => getBackgroundJobs({ limit }).then(apply).catch(error => setState(s => ({ ...s, error })));

  useServerEvent(enabled ? 'jobs' : null, snapshot => apply(snapshot));

  useEffect(() => {
    if (!enabled) return undefined;
    load();
    let timer;
    const tick = () => {
      timer = setTimeout(() => {
        if (document.visibilityState === 'hidden' || connectedRef.current) tick(); else load().finally(tick);
      }, active.current ? 2000 : 8000);
    };
    tick();
    return () => clearTimeout(timer);
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, limit]);

  return { ...state, connected, loading: state.data === null && !state.error, reload: load };
}
