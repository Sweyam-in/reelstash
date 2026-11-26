import { useCallback, useMemo } from 'react';
import { useSearchParams } from 'react-router-dom';

// The media app opens admin editors with links such as /admin/videos?edit=42&tab=match.
// useEditLink reads that request once and clears it from the URL when the editor closes,
// so a reload does not reopen it.
export function useEditLink() {
  const [params, setParams] = useSearchParams();
  const raw = params.get('edit');
  const id = useMemo(() => (raw && /^\d+$/.test(raw) ? Number(raw) : null), [raw]);
  const tab = params.get('tab') || null;
  const clear = useCallback(() => {
    setParams(prev => {
      const next = new URLSearchParams(prev);
      next.delete('edit');
      next.delete('tab');
      return next;
    }, { replace: true });
  }, [setParams]);
  return { id, tab, clear };
}
