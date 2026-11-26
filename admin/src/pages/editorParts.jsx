import React, { useCallback, useEffect, useState } from 'react';
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription,
  AlertDialogFooter, AlertDialogHeader, AlertDialogTitle, Button,
} from '@spatika/react';
import { Plus, X } from 'lucide-react';

// Shared pieces of the person, studio, series and video editors.

/** A list of short names edited as chips: type one and press Enter or Add; × removes it. */
export function ChipEditor({ label, values, onChange, suggestions = [], placeholder, hint }) {
  const [text, setText] = useState('');
  function add() {
    const value = text.trim();
    if (value && !values.some(existing => existing.toLowerCase() === value.toLowerCase())) onChange([...values, value]);
    setText('');
  }
  const listId = `rs-chip-${label.replace(/\W/g, '')}`;
  return <div>
    <p className="rs-eyebrow">{label}</p>
    <p className="rs-pm-chips rs-pm-chips-edit">
      {values.map(value => <span key={value}>{value}<button type="button" aria-label={`Remove ${value}`} onClick={() => onChange(values.filter(v => v !== value))}><X size={12} /></button></span>)}
      {!values.length && <small className="rs-pm-muted">None</small>}
    </p>
    <div className="rs-match-search">
      <input list={suggestions.length ? listId : undefined} aria-label={placeholder || `Add to ${label}`} value={text} placeholder={placeholder} onChange={e => setText(e.target.value)} onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); add(); } }} />
      {!!suggestions.length && <datalist id={listId}>{suggestions.map(name => <option key={name} value={name} />)}</datalist>}
      <button type="button" className="rs-button rs-button-outline" disabled={!text.trim()} onClick={add}><Plus size={15} /> Add</button>
    </div>
    {hint && <p className="rs-pm-muted">{hint}</p>}
  </div>;
}

const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);

/**
 * The Details tab's edits, held by the editor dialog so they survive switching tabs and can be checked
 * before it closes. `fieldsOf` picks the editable fields from the saved record; the draft starts over when
 * another record opens and after a save (`reset`).
 */
export function useDetailsDraft(record, fieldsOf) {
  const [draft, setDraft] = useState(null);
  const id = record?.id;
  useEffect(() => { setDraft(null); }, [id]);
  const base = record ? fieldsOf(record) : null;
  const value = draft ?? base;
  const dirty = !!(draft && base && !same(draft, base));
  const set = useCallback(patch => setDraft(current => ({ ...(current ?? fieldsOf(record)), ...patch })), [record]); // eslint-disable-line react-hooks/exhaustive-deps
  const reset = useCallback(() => setDraft(null), []);
  return { value, dirty, set, reset };
}

/** Wraps a dialog's onOpenChange so closing with unsaved details asks first. */
export function useCloseGuard(dirty, onOpenChange, onDiscard) {
  const [asking, setAsking] = useState(false);
  const requestOpenChange = useCallback(next => {
    if (!next && dirty) setAsking(true);
    else onOpenChange(next);
  }, [dirty, onOpenChange]);
  const prompt = <AlertDialog open={asking} onOpenChange={setAsking}>
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>Discard unsaved changes?</AlertDialogTitle>
        <AlertDialogDescription>Your edits on the Details tab have not been saved.</AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>Keep editing</AlertDialogCancel>
        <AlertDialogAction onClick={() => { setAsking(false); onDiscard?.(); onOpenChange(false); }}>Discard</AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>;
  return [requestOpenChange, prompt];
}

/** Save and Discard for a Details form; both wake up once something changed. */
export function SaveRow({ dirty, busy, invalid, onDiscard, children }) {
  return <div className="rs-pm-save">
    <Button type="submit" disabled={busy || !dirty || invalid}>{busy ? 'Saving…' : 'Save changes'}</Button>
    {dirty && !busy && <Button type="button" variant="ghost" onClick={onDiscard}>Discard</Button>}
    {dirty && <span className="rs-pm-dirty-note" role="status">Unsaved changes</span>}
    {children}
  </div>;
}
