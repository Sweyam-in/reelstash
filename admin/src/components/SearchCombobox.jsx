import React, { useEffect, useRef, useState } from 'react';
import { Plus, RefreshCw, Search } from 'lucide-react';
import { useDebounce } from '../pages/hooks/useDebounce';

export function Thumb({ src, name, round }) {
  const [failed, setFailed] = useState(false);
  if (src && !failed) return <img className={`rs-pm-thumb${round ? ' is-round' : ''}`} src={src} alt="" loading="lazy" onError={() => setFailed(true)} />;
  return <span className={`rs-pm-thumb rs-pm-thumb-fallback${round ? ' is-round' : ''}`}>{(name || '?').slice(0, 2).toUpperCase()}</span>;
}

/**
 * Type-to-search field with results in a dropdown, for quickly adding an existing performer,
 * studio, etc. without a separate "Find" step. `onPick` fires as soon as a result (or the
 * "create new" row) is chosen; the field then clears itself so another can be added right away, unless `keepOpen` is set: then the
 * search and its results stay so several can be picked from one search.
 */
export function SearchCombobox({ placeholder, searchFn, renderOption, onPick, createLabel, minLength = 1, keepOpen = false }) {
  const [text, setText] = useState('');
  const [options, setOptions] = useState(null);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const debounced = useDebounce(text, 250);
  const boxRef = useRef(null);

  useEffect(() => {
    const onOutside = event => { if (boxRef.current && !boxRef.current.contains(event.target)) setOpen(false); };
    document.addEventListener('pointerdown', onOutside);
    return () => document.removeEventListener('pointerdown', onOutside);
  }, []);

  useEffect(() => {
    const typed = debounced.trim();
    if (typed.length < minLength) { setOptions(null); setBusy(false); return undefined; }
    let active = true;
    setBusy(true);
    searchFn(typed).then(result => { if (active) { setOptions(result); setBusy(false); } }).catch(() => { if (active) { setOptions([]); setBusy(false); } });
    return () => { active = false; };
  }, [debounced, minLength, searchFn]);

  const typed = text.trim();
  const pick = option => { onPick(option); if (!keepOpen) { setText(''); setOptions(null); } };
  const exactMatch = typed && (options || []).some(option => option.name.toLowerCase() === typed.toLowerCase());

  return <div className="rs-combobox" ref={boxRef}>
    <div className="rs-combobox-field">
      <Search size={15} aria-hidden="true" />
      <input aria-label={placeholder} value={text} placeholder={placeholder} onChange={e => { setText(e.target.value); setOpen(true); }} onFocus={() => setOpen(true)} onKeyDown={e => { if (e.key === 'Escape') setOpen(false); }} />
      {busy && <RefreshCw size={14} className="rs-spin" aria-hidden="true" />}
    </div>
    {open && typed && <div className="rs-combobox-menu" role="listbox">
      {options === null ? null
        : !options.length && !createLabel ? <p className="rs-pm-muted">No matches.</p>
        : options.map(option => <button type="button" key={option.id ?? option.name} className="rs-combobox-option" onClick={() => pick(option)}>{renderOption(option)}</button>)}
      {createLabel && typed && !exactMatch && options !== null && <button type="button" className="rs-combobox-option rs-combobox-create" onClick={() => pick({ name: typed })}><Plus size={15} aria-hidden="true" /> {createLabel(typed)}</button>}
    </div>}
  </div>;
}

