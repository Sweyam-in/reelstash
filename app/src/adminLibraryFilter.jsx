import React, { useEffect, useSyncExternalStore } from 'react';
import { ChevronDown, LibraryBig } from 'lucide-react';
import { DropdownMenu, DropdownMenuContent, DropdownMenuLabel, DropdownMenuRadioGroup, DropdownMenuRadioItem, DropdownMenuSeparator, DropdownMenuTrigger } from '@spatika/react';
import { getLibraries } from './api';
import { adminTerms } from './adminTerms';
import { HeaderScope } from './adminHeader';

// The admin library scope: "All libraries" or one library. It is one setting for the whole admin
// console (remembered in this browser); pages that can narrow by library show it next to their title.

const KEY = 'reelstash-admin-library';
const listeners = new Set();
let libraries = null;
let loading = null;

function readId() {
  try { return Number(localStorage.getItem(KEY)) || null; } catch { return null; }
}

function subscribe(fn) { listeners.add(fn); return () => listeners.delete(fn); }
function notify() { listeners.forEach(fn => fn()); }

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

// libraryId is the selected library, or null for all of them (also when the remembered library no longer exists).
export function useAdminLibrary() {
  const list = useSyncExternalStore(subscribe, getLibrariesSnapshot);
  const storedId = useSyncExternalStore(subscribe, readId);
  useEffect(() => { if (libraries === null) loadLibraries(); }, []);
  const library = list?.find(l => l.id === storedId) || null;
  return { libraries: list || [], loaded: list !== null, library, libraryId: library?.id ?? null, terms: adminTerms(library), setLibraryId: setAdminLibraryId };
}

// The scope switcher in the top bar, beside the page title. Pages that work on one library at a time
// (allowAll=false) show that library and fall back to the first one. Hidden with a single library.
export function LibraryScope({ allowAll = true }) {
  const { libraries: list, library, setLibraryId } = useAdminLibrary();
  if (list.length < 2) return null;
  const current = library || (allowAll ? null : list[0]);
  return <HeaderScope><DropdownMenu>
    <DropdownMenuTrigger asChild><button type="button" className="rs-scope-trigger" aria-label="Library scope">
      <span className="rs-scope-in">in</span><LibraryBig size={14} /><span className="rs-scope-name">{current ? current.name : 'All libraries'}</span><ChevronDown size={14} />
    </button></DropdownMenuTrigger>
    <DropdownMenuContent align="start">
      <DropdownMenuLabel>Show admin pages for</DropdownMenuLabel>
      <DropdownMenuRadioGroup value={String(current?.id ?? '')} onValueChange={value => setLibraryId(Number(value) || null)}>
        {allowAll && <DropdownMenuRadioItem value="">All libraries</DropdownMenuRadioItem>}
        {allowAll && <DropdownMenuSeparator />}
        {list.map(l => <DropdownMenuRadioItem key={l.id} value={String(l.id)}>{l.name}</DropdownMenuRadioItem>)}
      </DropdownMenuRadioGroup>
    </DropdownMenuContent>
  </DropdownMenu></HeaderScope>;
}

// Hidden while there is only one library: there is nothing to choose between.
export function AdminLibraryFilter({ libraries: list, libraryId, onChange, label = 'Filter by library' }) {
  if (list.length < 2) return null;
  return <select aria-label={label} value={libraryId ?? ''} onChange={e => onChange(Number(e.target.value) || null)}>
    <option value="">All libraries</option>
    {list.map(l => <option key={l.id} value={l.id}>{l.name}</option>)}
  </select>;
}
