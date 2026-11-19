import React, { useEffect, useSyncExternalStore } from 'react';
import { getLibraries } from '../../api';
import { adminTerms } from '../../adminTerms';

// The admin library scope: "All libraries" or one library. One setting for the whole console,
// remembered in this browser. Pages that can narrow by library show a LibraryScopeSelect in their
// toolbar; it is hidden while there is only one library.

const KEY = 'reelstash-admin-library';
const listeners = new Set();
let libraries = null;
let loading = null;

function readId() {
  try { return Number(localStorage.getItem(KEY)) || null; } catch { return null; }
}

const subscribe = fn => { listeners.add(fn); return () => listeners.delete(fn); };
const notify = () => listeners.forEach(fn => fn());

export function setAdminLibraryId(id) {
  try { if (id) localStorage.setItem(KEY, String(id)); else localStorage.removeItem(KEY); } catch { /* storage blocked */ }
  notify();
}

function loadLibraries() {
  if (!loading) {
    loading = getLibraries()
      .then(result => { libraries = result.items || []; notify(); })
      .catch(() => { libraries = []; notify(); })
      .finally(() => { loading = null; });
  }
  return loading;
}

const getLibrariesSnapshot = () => libraries;

// libraryId is the selected library, or null for all (also when the remembered library is gone).
export function useAdminLibrary() {
  const list = useSyncExternalStore(subscribe, getLibrariesSnapshot);
  const storedId = useSyncExternalStore(subscribe, readId);
  useEffect(() => { if (libraries === null) loadLibraries(); }, []);
  const library = list?.find(l => l.id === storedId) || null;
  return { libraries: list || [], loaded: list !== null, library, libraryId: library?.id ?? null, terms: adminTerms(library), setLibraryId: setAdminLibraryId };
}

// Pages that work on one library at a time (allowAll=false) fall back to the first one.
export function LibraryScopeSelect({ allowAll = true }) {
  const { libraries: list, library, setLibraryId } = useAdminLibrary();
  if (list.length < 2) return null;
  const current = library || (allowAll ? null : list[0]);
  return <select className="console-select" aria-label="Library" value={current?.id ?? ''} onChange={e => setLibraryId(Number(e.target.value) || null)}>
    {allowAll && <option value="">All libraries</option>}
    {list.map(l => <option key={l.id} value={l.id}>{l.name}</option>)}
  </select>;
}
