import React, { useEffect, useState } from 'react';
import { Bookmark, Check, Pencil, Play, Plus, Trash2, X } from 'lucide-react';
import { useToast } from '../ui';
import { createMarker, deleteMarker, updateMarker } from '../api';
import { formatClock } from '../playerUtils';
import { useSprite } from './PlayerTimeline';

// A marker's picture: the scrub-preview tile nearest its moment, scaled to the card.
function spriteBackground(sprite, seconds) {
  if (!sprite) return undefined;
  const index = Math.max(0, Math.min(sprite.count - 1, Math.floor(seconds / sprite.intervalSeconds)));
  const columns = Math.max(1, sprite.columns);
  const rows = Math.max(1, Math.ceil(sprite.count / columns));
  return {
    backgroundImage: `url(${sprite.url})`,
    backgroundSize: `${columns * 100}% ${rows * 100}%`,
    backgroundPosition: `${columns > 1 ? (index % columns) / (columns - 1) * 100 : 0}% ${rows > 1 ? Math.floor(index / columns) / (rows - 1) * 100 : 0}%`,
  };
}

// The playhead as text, for "Add at 11:42".
function usePlayhead(videoRef, mediaId) {
  const [seconds, setSeconds] = useState(0);
  useEffect(() => {
    const video = videoRef.current;
    if (!video || typeof video.addEventListener !== 'function') return undefined;
    const sync = () => setSeconds(Number.isFinite(video.currentTime) ? video.currentTime : 0);
    const events = ['timeupdate', 'seeked', 'loadedmetadata'];
    events.forEach(name => video.addEventListener(name, sync));
    sync();
    return () => events.forEach(name => video.removeEventListener(name, sync));
  }, [videoRef, mediaId]);
  return seconds;
}

/**
 * The person's markers for this title: jump to a moment, add one at the current
 * position, rename or delete. Markers are private to the signed-in user.
 */
export default function Markers({ mediaId, videoRef, markers, onChange, onJump }) {
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  const [title, setTitle] = useState('');
  const sprite = useSprite(mediaId, true);
  const playhead = usePlayhead(videoRef, mediaId);
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
      <form className="rs-marker-add" onSubmit={add}>
        <Bookmark size={16} aria-hidden="true" />
        <input aria-label="Marker name" placeholder="Name this moment (optional)" maxLength={120} value={title} onChange={event => setTitle(event.target.value)} />
        <button type="submit" disabled={busy}><Plus size={15} aria-hidden="true" /> Add at {formatClock(playhead)}</button>
      </form>
    </div>
    {sorted.length === 0
      ? <p className="rs-markers-empty">No markers yet. Pause on a moment you want to come back to and add one.</p>
      : <ul className="rs-marker-list">
        {sorted.map(marker => <li key={marker.id}>
          <button type="button" className="rs-marker-card" onClick={() => onJump(marker.seconds)} aria-label={`Play from ${formatClock(marker.seconds)}`}>
            <span className="rs-marker-thumb" style={spriteBackground(sprite, marker.seconds)}><span className="rs-marker-time"><Play size={11} fill="currentColor" aria-hidden="true" /> {formatClock(marker.seconds)}</span></span>
          </button>
          {editing?.id === marker.id
            ? <form className="rs-marker-edit" onSubmit={event => { event.preventDefault(); rename(marker); }}>
              <input autoFocus aria-label="Marker name" maxLength={120} value={editing.title} onChange={event => setEditing({ id: marker.id, title: event.target.value })} onKeyDown={event => { if (event.key === 'Escape') setEditing(null); }} />
              <button type="submit" className="rs-marker-icon" aria-label="Save name"><Check size={15} /></button>
              <button type="button" className="rs-marker-icon" aria-label="Cancel" onClick={() => setEditing(null)}><X size={15} /></button>
            </form>
            : <div className="rs-marker-row">
              <span className="rs-marker-title" title={marker.title}>{marker.title || 'Untitled moment'}</span>
              <button type="button" className="rs-marker-icon" aria-label={`Rename ${marker.title}`} onClick={() => setEditing({ id: marker.id, title: marker.title })}><Pencil size={14} /></button>
              <button type="button" className="rs-marker-icon" aria-label={`Delete ${marker.title}`} onClick={() => remove(marker)}><Trash2 size={14} /></button>
            </div>}
        </li>)}
      </ul>}
  </section>;
}
