import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { ChevronDown, ChevronUp, Heart, Info, Pause, Play, Shuffle, Sparkles, Smartphone, Volume2, VolumeX } from 'lucide-react';
import { useToast } from '@spatika/react';
import { artworkUrl, getMediaItems, updateMediaItem, updatePlayback } from '../api';
import { usePersistedState } from '../hooks/usePersistedState';
import { useAdaptiveSource } from './hooks/useAdaptiveSource';
import '../reels.css';

// Reels: the selected library's portrait videos (the server detects them from the probed frame size
// and rotation), one at a time in a vertical player. Scroll, swipe or use the arrow keys to move
// between them, like YouTube Shorts or Instagram Reels.

const PAGE_SIZE = 20;
const LOAD_AHEAD = 5; // fetch the next page this many reels before the end
const MUTED_KEY = 'reelstash.reels.muted';
const SORTS = [
  { value: 'shuffle', label: 'Shuffle', icon: Shuffle },
  { value: 'newest', label: 'Newest', icon: Sparkles },
];

const readMuted = () => { try { return localStorage.getItem(MUTED_KEY) === 'true'; } catch { return false; } };
const newSeed = () => Math.random().toString(36).slice(2, 10);

// Plays one reel: the original file when the browser can, otherwise the server's transcode.
function ReelPlayer({ item, muted, onAutoplayMuted, paused, onPausedChange }) {
  const videoRef = useRef(null);
  const [failed, setFailed] = useState(false);
  const [progress, setProgress] = useState(0);
  const counted = useRef(false);
  const { mode, retry, setTranscode } = useAdaptiveSource(videoRef, item.id, true, () => setFailed(true));

  useEffect(() => { if (videoRef.current) videoRef.current.muted = muted; }, [muted]);
  useEffect(() => {
    const video = videoRef.current;
    if (!video || !video.currentSrc) return;
    if (paused) video.pause(); else video.play().catch(() => {});
  }, [paused]);

  // Start as soon as there is a frame. Browsers refuse sound before the user has interacted with
  // the page; then the reel plays muted and the mute button shows it.
  function start() {
    const video = videoRef.current;
    if (!video || paused) return;
    video.play().catch(err => {
      if (err?.name !== 'NotAllowedError') return;
      video.muted = true;
      onAutoplayMuted();
      video.play().catch(() => {});
    });
  }
  function onTimeUpdate() {
    const video = videoRef.current;
    if (!video?.duration) return;
    const fraction = video.currentTime / video.duration;
    setProgress(fraction);
    // Watching most of a reel counts as watching it, once per visit.
    if (!counted.current && fraction > 0.9) {
      counted.current = true;
      updatePlayback(item.id, 0, true).catch(() => {});
    }
  }
  function seek(event) {
    const video = videoRef.current;
    if (!video?.duration) return;
    const rect = event.currentTarget.getBoundingClientRect();
    video.currentTime = Math.max(0, Math.min(1, (event.clientX - rect.left) / rect.width)) * video.duration;
  }

  return <>
    <video ref={videoRef} className="rs-reel-video" playsInline loop muted={muted} preload="auto"
      poster={artworkUrl(item.id, 'thumbnail')}
      onLoadedData={start} onTimeUpdate={onTimeUpdate}
      onClick={() => onPausedChange(!paused)} />
    {paused && !failed && <span className="rs-reel-paused" aria-hidden="true"><Play size={34} fill="currentColor" /></span>}
    {failed && <div className="rs-reel-error" role="alert">This video can’t be played.
      <button type="button" onClick={() => { setFailed(false); retry(); }}>Retry</button>
      {mode !== 'hls' && <button type="button" onClick={() => { setFailed(false); setTranscode(true); }}>Play transcoded</button>}
      <Link to={`/library/${item.id}`} state={{ from: '/reels' }}>Open details</Link>
    </div>}
    <div className="rs-reel-progress" role="slider" aria-label="Seek" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(progress * 100)} tabIndex={-1} onClick={seek}><span style={{ width: `${progress * 100}%` }} /></div>
  </>;
}

function Reel({ item, index, active, muted, paused, onPausedChange, onToggleMute, onAutoplayMuted, onFavorite }) {
  const meta = [item.studioName, ...(item.performers || []).slice(0, 3)].filter(Boolean);
  return <section className={`rs-reel${active ? ' is-active' : ''}`} data-index={index} aria-label={item.title}>
    <div className="rs-reel-stage">
      <div className="rs-reel-frame">
        {active
          ? <ReelPlayer item={item} muted={muted} paused={paused} onPausedChange={onPausedChange} onAutoplayMuted={onAutoplayMuted} />
          : <img className="rs-reel-video" src={artworkUrl(item.id, 'thumbnail')} alt="" loading="lazy" />}
        <div className="rs-reel-caption">
          <Link to={`/library/${item.id}`} state={{ from: '/reels' }} className="rs-reel-title">{item.title}</Link>
          {meta.length > 0 && <p>{meta.join(' · ')}</p>}
        </div>
      </div>
      <div className="rs-reel-actions">
        <button type="button" className={item.favorite ? 'is-on' : ''} aria-pressed={!!item.favorite} aria-label={item.favorite ? 'Remove from favorites' : 'Add to favorites'} onClick={() => onFavorite(item)}><Heart size={22} fill={item.favorite ? 'currentColor' : 'none'} /></button>
        {active && <button type="button" aria-label={paused ? 'Play' : 'Pause'} onClick={() => onPausedChange(!paused)}>{paused ? <Play size={22} /> : <Pause size={22} />}</button>}
        <button type="button" aria-pressed={muted} aria-label={muted ? 'Unmute' : 'Mute'} onClick={onToggleMute}>{muted ? <VolumeX size={22} /> : <Volume2 size={22} />}</button>
        <Link to={`/library/${item.id}`} state={{ from: '/reels' }} aria-label="Details"><Info size={22} /></Link>
      </div>
    </div>
  </section>;
}

export default function Reels() {
  const { toast } = useToast();
  const [sort, setSort] = usePersistedState('reels-sort', 'shuffle');
  const seed = useMemo(newSeed, [sort]); // a fresh shuffle each visit, stable while paging
  const [items, setItems] = useState([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [active, setActive] = useState(0);
  const [paused, setPaused] = useState(false);
  const [muted, setMuted] = useState(readMuted);
  const feedRef = useRef(null);
  const loadingMore = useRef(false);

  const fetchPage = useCallback(offset => getMediaItems({ reels: 'true', sort: sort === 'shuffle' ? 'shuffle' : '', seed, limit: PAGE_SIZE, offset }), [sort, seed]);

  useEffect(() => {
    let cancelled = false;
    setLoading(true); setError(null); setItems([]); setActive(0); setPaused(false);
    feedRef.current?.scrollTo({ top: 0 });
    fetchPage(0).then(result => {
      if (cancelled) return;
      setItems(result.items || []); setTotal(result.total || 0);
    }).catch(err => { if (!cancelled) setError(err.message); }).finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [fetchPage]);

  // Keep a page ahead of the viewer.
  useEffect(() => {
    if (loading || loadingMore.current || items.length >= total || active < items.length - LOAD_AHEAD) return;
    loadingMore.current = true;
    fetchPage(items.length).then(result => {
      setItems(current => {
        const seen = new Set(current.map(item => item.id));
        return [...current, ...(result.items || []).filter(item => !seen.has(item.id))];
      });
      setTotal(result.total || 0);
    }).catch(() => {}).finally(() => { loadingMore.current = false; });
  }, [active, items.length, total, loading, fetchPage]);

  // The reel filling most of the feed is the one that plays.
  useEffect(() => {
    const feed = feedRef.current;
    if (!feed || !items.length) return undefined;
    const observer = new IntersectionObserver(entries => {
      for (const entry of entries) {
        if (entry.isIntersecting) {
          const index = Number(entry.target.dataset.index);
          setActive(current => { if (current !== index) setPaused(false); return index; });
        }
      }
    }, { root: feed, threshold: 0.6 });
    feed.querySelectorAll('.rs-reel').forEach(node => observer.observe(node));
    return () => observer.disconnect();
  }, [items]);

  const goTo = useCallback(index => {
    const feed = feedRef.current;
    if (!feed || index < 0 || index >= items.length) return;
    feed.scrollTo({ top: index * feed.clientHeight, behavior: 'smooth' });
  }, [items.length]);

  const toggleMute = useCallback(() => setMuted(current => {
    try { localStorage.setItem(MUTED_KEY, String(!current)); } catch { /* not remembered */ }
    return !current;
  }), []);

  // One wheel gesture moves one reel, as in Shorts: a mouse notch is too short to pass the snap
  // point, and a trackpad flick would otherwise fly past several.
  const activeRef = useRef(active);
  activeRef.current = active;
  useEffect(() => {
    const feed = feedRef.current;
    if (!feed) return undefined;
    let travelled = 0;
    let lockedUntil = 0;
    function onWheel(event) {
      if (event.ctrlKey || Math.abs(event.deltaY) < Math.abs(event.deltaX)) return;
      event.preventDefault();
      const now = performance.now();
      if (now < lockedUntil) return;
      travelled += event.deltaY;
      if (Math.abs(travelled) < 30) return;
      goTo(activeRef.current + Math.sign(travelled));
      travelled = 0;
      lockedUntil = now + 650;
    }
    feed.addEventListener('wheel', onWheel, { passive: false });
    return () => feed.removeEventListener('wheel', onWheel);
  }, [goTo, loading]);

  useEffect(() => {
    function onKey(event) {
      if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey) return;
      if (event.target.closest?.('input, textarea, select, [contenteditable="true"]')) return;
      const key = event.key.toLowerCase();
      if (key === 'arrowdown' || key === 'j') goTo(active + 1);
      else if (key === 'arrowup' || key === 'k') goTo(active - 1);
      else if (key === ' ') setPaused(current => !current);
      else if (key === 'm') toggleMute();
      else return;
      event.preventDefault();
    }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [active, goTo, toggleMute]);

  async function toggleFavorite(item) {
    try {
      const updated = await updateMediaItem(item.id, { favorite: !item.favorite });
      setItems(current => current.map(entry => entry.id === item.id ? { ...entry, favorite: updated.favorite } : entry));
    } catch (err) {
      toast({ title: 'Could not update favorite', description: err.message, tone: 'danger' });
    }
  }

  const sortPicker = <div className="rs-reels-sort" role="group" aria-label="Order">
    {SORTS.map(({ value, label, icon: Icon }) => <button type="button" key={value} aria-pressed={sort === value} onClick={() => setSort(value)}><Icon size={14} /> {label}</button>)}
  </div>;

  if (!loading && (error || !items.length)) {
    return <div className="rs-reels rs-reels-empty">
      <Smartphone size={40} />
      <h1>{error ? 'Reels could not load' : 'No reels yet'}</h1>
      <p>{error || 'Portrait videos in this library show up here automatically after a scan.'}</p>
    </div>;
  }

  return <div className="rs-reels">
    {sortPicker}
    <div className="rs-reels-feed" ref={feedRef} aria-busy={loading}>
      {items.map((item, index) => <Reel key={item.id} item={item} index={index} active={index === active}
        muted={muted} paused={paused} onPausedChange={setPaused} onToggleMute={toggleMute}
        onAutoplayMuted={() => setMuted(true)} onFavorite={toggleFavorite} />)}
    </div>
    <div className="rs-reels-nav">
      <button type="button" aria-label="Previous reel" disabled={active <= 0} onClick={() => goTo(active - 1)}><ChevronUp size={22} /></button>
      <button type="button" aria-label="Next reel" disabled={active >= items.length - 1} onClick={() => goTo(active + 1)}><ChevronDown size={22} /></button>
    </div>
  </div>;
}
