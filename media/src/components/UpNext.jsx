import React, { useEffect, useRef, useState } from 'react';
import { Play, X } from 'lucide-react';
import { artworkUrl } from '../api';
import { episodeCode } from '../trackPrefs';

/**
 * Shown over the player when a title ends and the next episode is waiting. Counts down, then plays it;
 * "Cancel" (or Escape) stays on the finished title.
 */
export default function UpNext({ item, seconds = 8, onPlay, onCancel }) {
  const [left, setLeft] = useState(seconds);
  const fired = useRef(false);
  useEffect(() => {
    setLeft(seconds);
    fired.current = false;
    const timer = setInterval(() => setLeft(value => value - 1), 1000);
    return () => clearInterval(timer);
  }, [item?.id, seconds]);
  useEffect(() => {
    if (left <= 0 && !fired.current) { fired.current = true; onPlay(); }
  }, [left, onPlay]);
  useEffect(() => {
    const onKey = event => { if (event.key === 'Escape') onCancel(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onCancel]);

  const code = episodeCode(item);
  return <div className="rs-up-next" role="dialog" aria-label="Up next">
    <img src={artworkUrl(item.id, 'thumbnail', item.posterImageId)} alt="" onError={event => { event.currentTarget.style.display = 'none'; }} />
    <div>
      <p className="rs-eyebrow">Up next in {Math.max(0, left)}s</p>
      <strong>{item.title}</strong>
      {(code || item.seriesName) && <small>{[code, item.seriesName].filter(Boolean).join(' · ')}</small>}
      <span className="rs-up-next-actions">
        <button type="button" className="rs-button rs-button-primary" onClick={() => { fired.current = true; onPlay(); }}><Play size={16} fill="currentColor" /> Play now</button>
        <button type="button" className="rs-button rs-button-outline" onClick={onCancel}><X size={16} /> Cancel</button>
      </span>
    </div>
  </div>;
}
