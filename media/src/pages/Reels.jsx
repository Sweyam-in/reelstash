import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { ArrowLeft, Check, ChevronDown, ChevronUp, Clock3, Ellipsis, Heart, Info, ListPlus, Pause, Play, Smartphone, Volume2, VolumeX } from 'lucide-react';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger, useToast } from '../ui';
import { addWatchLater, artworkUrl, getMediaItems, removeWatchLater, updateMediaItem, updatePlayback } from '../api';
import AddToPlaylist from '../components/AddToPlaylist';
import { formatClock } from '../playerUtils';
import { usePersistedState } from '../hooks/usePersistedState';
import { useAdaptiveSource } from './hooks/useAdaptiveSource';


// Reels: the selected library's portrait videos (the server detects them from the probed frame size
// and rotation), one at a time in a vertical player. Scroll, swipe or use the arrow keys to move
// between them, like YouTube Shorts or Instagram Reels.

const PAGE_SIZE = 20;
const LOAD_AHEAD = 5; // fetch the next page this many reels before the end
const MUTED_KEY = 'reelstash.reels.muted';
const SORTS = [
  { value: 'shuffle', label: 'Shuffle' },
  { value: 'newest', label: 'Newest' },
  { value: 'unwatched', label: 'Not watched' },
  { value: 'favorites', label: 'Favorites' },
];

// What each order asks the server for, on top of reels=true.
const orderParams = (sort, seed) => ({
  ...(sort === 'shuffle' ? { sort: 'shuffle', seed } : { sort: '' }),
  ...(sort === 'unwatched' ? { watched: 'unwatched' } : {}),
  ...(sort === 'favorites' ? { favorite: 'true' } : {}),
});

const addedLabel = value => {
  if (!value) return '';
  const days = Math.floor((Date.now() - new Date(value).getTime()) / 86400000);
  if (Number.isNaN(days)) return '';
  return days <= 0 ? 'Added today' : days === 1 ? 'Added yesterday' : days < 30 ? `Added ${days} days ago` : `Added ${new Date(value).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })}`;
};

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

function Reel({ item, index, active, muted, paused, onPausedChange, onToggleMute, onAutoplayMuted, onFavorite, onWatchLater, onPlaylist, onWatched }) {
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
        {active && <div className="rs-reel-controls">
          <button type="button" aria-label={paused ? 'Play' : 'Pause'} onClick={() => onPausedChange(!paused)}>{paused ? <Play size={20} /> : <Pause size={20} />}</button>
          <button type="button" aria-pressed={muted} aria-label={muted ? 'Unmute' : 'Mute'} onClick={onToggleMute}>{muted ? <VolumeX size={20} /> : <Volume2 size={20} />}</button>
        </div>}
      </div>
      <div className="rs-reel-actions">
        <div><button type="button" className={item.favorite ? 'is-on' : ''} aria-pressed={!!item.favorite} aria-label={item.favorite ? 'Remove from favorites' : 'Add to favorites'} onClick={() => onFavorite(item)}><Heart size={22} fill={item.favorite ? 'currentColor' : 'none'} /></button><span>Favorite</span></div>
        <div><button type="button" className={item.watchLater ? 'is-on' : ''} aria-pressed={!!item.watchLater} aria-label={item.watchLater ? 'Remove from Watch later' : 'Watch later'} onClick={() => onWatchLater(item)}><Clock3 size={22} /></button><span>Later</span></div>
        <div><button type="button" aria-label="Add to playlist" onClick={() => onPlaylist(item)}><ListPlus size={22} /></button><span>Playlist</span></div>
        <div>
          <DropdownMenu><DropdownMenuTrigger asChild><button type="button" aria-label="More"><Ellipsis size={22} /></button></DropdownMenuTrigger>
            <DropdownMenuContent align="end"><DropdownMenuItem asChild><Link to={`/library/${item.id}`} state={{ from: '/reels' }}><Info size={16} /> Open details</Link></DropdownMenuItem><DropdownMenuItem onSelect={() => onWatched(item)}><Check size={16} /> Mark as watched</DropdownMenuItem></DropdownMenuContent></DropdownMenu>
          <span>More</span>
        </div>
      </div>
    </div>
  </section>;
}

// The reel on screen, described beside the stage: studio, title, shape, people and tags.
function ReelInfo({ item }) {
  const shape = item.width && item.height ? `${item.width} × ${item.height}` : '';
  const line = [item.durationSeconds ? formatClock(item.durationSeconds) : '', shape, addedLabel(item.dateAdded)].filter(Boolean).join(' · ');
  const tags = [...(item.categories || []), ...(item.tags || [])].slice(0, 6);
  return <aside className="rs-reels-info" aria-label="About this reel">
    {item.studioName && <span className="rs-reels-studio">{item.studioName}</span>}
    <h2>{item.title}</h2>
    {line && <p>{line}</p>}
    {!!item.performers?.length && <div className="rs-reels-people">{item.performers.slice(0, 4).map(name => <Link key={name} to={`/search?q=${encodeURIComponent(name)}`}><span aria-hidden="true">{name.split(/\s+/).map(part => part[0]).slice(0, 2).join('')}</span>{name}</Link>)}</div>}
    {!!tags.length && <div className="rs-reels-tags">{tags.map(tag => <span key={tag}>{tag}</span>)}</div>}
    <Link className="rs-reels-open" to={`/library/${item.id}`} state={{ from: '/reels' }}>Open full title page →</Link>
  </aside>;
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
  const [playlistFor, setPlaylistFor] = useState(null);
  const [muted, setMuted] = useState(readMuted);
  const feedRef = useRef(null);
  const loadingMore = useRef(false);

  const fetchPage = useCallback(offset => getMediaItems({ reels: 'true', ...orderParams(sort, seed), limit: PAGE_SIZE, offset }), [sort, seed]);

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
  const itemsRef = useRef(items);
  itemsRef.current = items;
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
      else if (key === 'f') { const reel = itemsRef.current[active]; if (reel) toggleFavorite(reel); }
      else return;
      event.preventDefault();
    }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [active, goTo, toggleMute]); // eslint-disable-line react-hooks/exhaustive-deps

  async function toggleFavorite(item) {
    try {
      const updated = await updateMediaItem(item.id, { favorite: !item.favorite });
      setItems(current => current.map(entry => entry.id === item.id ? { ...entry, favorite: updated.favorite } : entry));
    } catch (err) {
      toast({ title: 'Could not update favorite', description: err.message, tone: 'danger' });
    }
  }

  async function toggleWatchLater(item) {
    const next = !item.watchLater;
    try {
      if (next) await addWatchLater(item.id); else await removeWatchLater(item.id);
      setItems(current => current.map(entry => entry.id === item.id ? { ...entry, watchLater: next } : entry));
      toast({ title: next ? 'Saved to Watch later' : 'Removed from Watch later', tone: 'success' });
    } catch (err) {
      toast({ title: 'Could not update Watch later', description: err.message, tone: 'danger' });
    }
  }
  async function markWatched(item) {
    try { await updatePlayback(item.id, 0, true); toast({ title: 'Marked as watched', tone: 'success' }); }
    catch (err) { toast({ title: 'Could not mark as watched', description: err.message, tone: 'danger' }); }
  }

  const sortPicker = <div className="rs-reels-sort" role="group" aria-label="Order">
    {SORTS.map(({ value, label }) => <button type="button" key={value} aria-pressed={sort === value} onClick={() => setSort(value)}>{label}</button>)}
  </div>;
  const current = items[active];
  const upNext = items.slice(active + 1, active + 5);

  if (!loading && (error || !items.length)) {
    return <div className="rs-reels rs-reels-empty">
      <Smartphone size={40} />
      <h1>{error ? 'Reels could not load' : 'No reels yet'}</h1>
      <p>{error || 'Portrait videos in this library show up here automatically after a scan.'}</p>
    </div>;
  }

  return <div className="rs-reels">
    <div className="rs-reels-top">
      <Link to="/" className="rs-reels-home"><ArrowLeft size={18} aria-hidden="true" />Home</Link>
      <h1>Reels</h1>
      <div className="rs-reels-order" role="group" aria-label="Order">{SORTS.map(({ value, label }) => <button type="button" key={value} aria-pressed={sort === value} onClick={() => setSort(value)}>{label}</button>)}</div>
      <span className="rs-reels-gap" />
      {total > 0 && <span className="rs-reels-count">{active + 1} of {total.toLocaleString()}</span>}
    </div>
    {sortPicker}
    {current && <ReelInfo item={current} />}
    <div className="rs-reels-feed" ref={feedRef} aria-busy={loading}>
      {items.map((item, index) => <Reel key={item.id} item={item} index={index} active={index === active}
        muted={muted} paused={paused} onPausedChange={setPaused} onToggleMute={toggleMute}
        onAutoplayMuted={() => setMuted(true)} onFavorite={toggleFavorite} onWatchLater={toggleWatchLater} onPlaylist={setPlaylistFor} onWatched={markWatched} />)}
    </div>
    <aside className="rs-reels-queue" aria-label="Up next">
      <button type="button" className="rs-reels-step" aria-label="Previous reel" disabled={active <= 0} onClick={() => goTo(active - 1)}><ChevronUp size={20} /></button>
      <span className="rs-reels-queue-label">Up next</span>
      {upNext.map((reel, offset) => <button type="button" key={reel.id} className="rs-reels-queue-item" onClick={() => goTo(active + 1 + offset)}>
        <span className="rs-reels-queue-thumb" style={{ backgroundImage: `url(${artworkUrl(reel.id, 'thumbnail')})` }} />
        <span><strong>{reel.title}</strong><small>{reel.durationSeconds ? formatClock(reel.durationSeconds) : ''}</small></span>
      </button>)}
      <button type="button" className="rs-reels-step is-next" aria-label="Next reel" disabled={active >= items.length - 1} onClick={() => goTo(active + 1)}><ChevronDown size={20} /></button>
      <span className="rs-reels-hint">↑ ↓ to move · Space to pause · F to favorite</span>
    </aside>
    <AddToPlaylist mediaItemId={playlistFor?.id} open={!!playlistFor} onOpenChange={open => { if (!open) setPlaylistFor(null); }} />
    <div className="rs-reels-nav">
      <button type="button" aria-label="Previous reel" disabled={active <= 0} onClick={() => goTo(active - 1)}><ChevronUp size={22} /></button>
      <button type="button" aria-label="Next reel" disabled={active >= items.length - 1} onClick={() => goTo(active + 1)}><ChevronDown size={22} /></button>
    </div>
  </div>;
}
