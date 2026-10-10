import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { getMyLibraries, setCurrentLibrary, setDefaultLibrary } from './api';
import { modeTerms } from './libraryModes';
import { LEAVE_LOCKED_LIBRARY_EVENT } from './components/AppLock';

// The media app always browses one library at a time: its mode picks the words and pages, and
// every API call is narrowed to it (api.js sends it as X-Library). Admins without any library
// yet browse everything unfiltered.
//
// A tab opens on the user's default library (Settings), else the first one; switching is
// remembered for the rest of that tab's session.

const SESSION_KEY = 'reelstash-library';
const LibraryContext = createContext({ libraries: [], library: null, mode: 'entertainment', terms: modeTerms('entertainment'), isAdmin: false, defaultLibraryId: null, select: () => {}, setDefault: async () => {} });

function sessionLibraryId() {
  try { return Number(sessionStorage.getItem(SESSION_KEY)) || null; } catch { return null; }
}

export function LibraryProvider({ isAdmin, children }) {
  const [libraries, setLibraries] = useState(null);
  const [defaultLibraryId, setDefaultLibraryId] = useState(null);
  const [selectedId, setSelectedId] = useState(sessionLibraryId);

  useEffect(() => {
    let active = true;
    getMyLibraries()
      .then(result => { if (active) { setLibraries(result.items || []); setDefaultLibraryId(result.defaultLibraryId || null); } })
      .catch(() => { if (active) setLibraries([]); });
    return () => { active = false; };
  }, []);

  const select = useCallback(id => {
    setSelectedId(id);
    try { sessionStorage.setItem(SESSION_KEY, String(id)); } catch { /* storage blocked */ }
  }, []);

  const library = libraries?.find(l => l.id === selectedId) || libraries?.find(l => l.id === defaultLibraryId) || libraries?.[0] || null;
  // Set before children render, so their first fetches already carry the library.
  setCurrentLibrary(library?.id);

  // "Go to another library" on a locked library's PIN screen.
  useEffect(() => {
    const leave = () => {
      const open = libraries?.find(l => !l.locked && l.id !== library?.id);
      if (open) select(open.id);
    };
    window.addEventListener(LEAVE_LOCKED_LIBRARY_EVENT, leave);
    return () => window.removeEventListener(LEAVE_LOCKED_LIBRARY_EVENT, leave);
  }, [libraries, library, select]);

  const setDefault = useCallback(async id => {
    await setDefaultLibrary(id);
    setDefaultLibraryId(id);
  }, []);

  const value = useMemo(() => {
    const mode = library?.mode || 'entertainment';
    return { libraries: libraries || [], library, mode, terms: modeTerms(mode), isAdmin, defaultLibraryId, select, setDefault };
  }, [libraries, library, isAdmin, defaultLibraryId, select, setDefault]);

  if (libraries === null) return <div className="auth-loading">Loading libraries…</div>;
  return <LibraryContext.Provider value={value}>{children}</LibraryContext.Provider>;
}

export const useLibrary = () => useContext(LibraryContext);
