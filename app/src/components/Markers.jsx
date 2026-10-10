import React, { useState } from 'react';
import { Bookmark, Check, Pencil, Play, Trash2, X } from 'lucide-react';
import { useToast } from '@spatika/react';
import { createMarker, deleteMarker, updateMarker } from '../api';
import { formatClock } from '../playerUtils';

/**
 * The person's markers for this title: jump to a moment, add one at the current
 * position, rename or delete. Markers are private to the signed-in user.
 */
export default function Markers({ mediaId, videoRef, markers, onChange, onJump }) {
  const { toast } = useToast();
  const [title, setTitle] = useState('');
  const [busy, setBusy] = useState(false);
  const [editing, setEditing] = useState(null); // { id, title }

  const currentSeconds = () => {
    const value = videoRef.current?.currentTime;
    return Number.isFinite(value) ? value : 0;
  };
  const sorted = [...markers].sort((a, b) => a.seconds - b.seconds || a.id - b.id);

  async function add(event) {
    event.preventDefault();
    const seconds = Math.round(currentSeconds() * 10) / 10;
    setBusy(true);
    try {
      const created = await createMarker(mediaId, seconds, title);
      onChange([...markers, created]);
      setTitle('');
      toast({ title: `Marker added at ${formatClock(seconds)}`, tone: 'success' });
    } catch (err) {
      toast({ title: 'Could not add marker', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  async function rename(marker) {
    const next = editing?.title ?? marker.title;
    setEditing(null);
    if (next.trim() === marker.title) return;
    try {
      const updated = await updateMarker(mediaId, marker.id, { title: next });
      onChange(markers.map(item => (item.id === marker.id ? updated : item)));
    } catch (err) {
      toast({ title: 'Could not rename marker', description: err.message, tone: 'danger' });
    }
  }

  async function remove(marker) {
    try {
      await deleteMarker(mediaId, marker.id);
      onChange(markers.filter(item => item.id !== marker.id));
    } catch (err) {
      toast({ title: 'Could not delete marker', description: err.message, tone: 'danger' });
    }
  }

  return <section className="rs-markers" aria-label="Markers">
    <div className="rs-markers-head">
      <div><p className="rs-eyebrow">YOUR MARKERS</p><h3>Jump to a moment</h3></div>
      <form onSubmit={add}>
        <input aria-label="Marker name" placeholder="Name this moment (optional)" maxLength={120} value={title} onChange={event => setTitle(event.target.value)} />
        <button type="submit" className="rs-button rs-button-outline" disabled={busy}><Bookmark size={16} /> Add at current time</button>
      </form>
    </div>
    {sorted.length === 0
      ? <p className="rs-markers-empty">No markers yet. Pause on a moment you want to come back to and add one.</p>
      : <ul className="rs-marker-list">
        {sorted.map(marker => <li key={marker.id}>
          <button type="button" className="rs-marker-time" onClick={() => onJump(marker.seconds)} aria-label={`Play from ${formatClock(marker.seconds)}`}><Play size={13} fill="currentColor" /> {formatClock(marker.seconds)}</button>
          {editing?.id === marker.id
            ? <form className="rs-marker-edit" onSubmit={event => { event.preventDefault(); rename(marker); }}>
              <input autoFocus aria-label="Marker name" maxLength={120} value={editing.title} onChange={event => setEditing({ id: marker.id, title: event.target.value })} onKeyDown={event => { if (event.key === 'Escape') setEditing(null); }} />
              <button type="submit" className="rs-marker-icon" aria-label="Save name"><Check size={15} /></button>
              <button type="button" className="rs-marker-icon" aria-label="Cancel" onClick={() => setEditing(null)}><X size={15} /></button>
            </form>
            : <>
              <span className="rs-marker-title" title={marker.title}>{marker.title}</span>
              <button type="button" className="rs-marker-icon" aria-label={`Rename ${marker.title}`} onClick={() => setEditing({ id: marker.id, title: marker.title })}><Pencil size={14} /></button>
              <button type="button" className="rs-marker-icon" aria-label={`Delete ${marker.title}`} onClick={() => remove(marker)}><Trash2 size={14} /></button>
            </>}
        </li>)}
      </ul>}
  </section>;
}
