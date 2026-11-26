import React, { useEffect, useMemo, useRef, useState } from 'react';
import { flushSync } from 'react-dom';
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { LayoutGrid, Rows3, AlertTriangle, ArrowLeft, ArrowRight, ArrowUpRight, Bookmark, BookmarkCheck, BookmarkPlus, Building2, Check, Dices, Ellipsis, Film, Layers3, Sparkles, Users, Globe, Heart, Bell, BellRing, ListPlus, ListVideo, Maximize2, Play, Plus, RefreshCw, Search, SearchX, Settings2, Shuffle, SkipBack, SkipForward, SlidersHorizontal, Star, X, Zap } from 'lucide-react';
import { monogram } from '../components/Monogram';
import { splitBio } from '../bioFacts';
import { Button, Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger, Input, Tabs, TabsList, TabsTrigger, useToast } from '../ui';
import {
  addCollectionItem, addWatchLater, artworkUrl, createCollection, createMarker, getMarkers, previewUrl, removeWatchLater, getCategories, getCollections, getContinueWatching, getNextInSeries, getNextUp, subtitleUrl, getHomeRecommendations, getHomeSeries, getHomeSummary, startWatchSession, updateWatchSession, getMediaItem, getRelatedMedia, getMediaItems,
  createSeries, getPerformer, getPerformers, getSeries, getSeriesItem, getStudio, getStudios, performerImageUrl, setPerformerFavorite, setPerformerNotify, studioImageUrl, updateMediaItem, updatePlayback, getRelatedPerformers, getWatchActivity } from '../api';
import PerformerSettings, { NewPerformerDialog } from './PerformerSettings';
import { useLibrary } from '../libraryContext';
import MediaSettings from './MediaSettings';
import StudioSettings from './StudioSettings';
import SeriesSettings from './SeriesSettings';
import SeriesCover from '../components/SeriesCover';
import { useDebounce } from './hooks/useDebounce';
import { useAdaptiveSource, TRANSCODE_QUALITIES } from './hooks/useAdaptiveSource';
import { useMediaTracks } from './hooks/useMediaTracks';
import { nextUpKicker, resumeKicker } from '../trackPrefs';
import UpNext from '../components/UpNext';
import { FilterChip, FilterPanel, FilterSelect } from '../components/FilterControls';
import { usePersistedState } from '../hooks/usePersistedState';
import { useScrollRestoration } from '../hooks/useScrollRestoration';
import { useColumnCount, useInfiniteList, VirtualWindow } from '../virtual';
import { cardMetrics, useCardStyle } from '../cardStyle';
import PlayerControls from '../components/PlayerControls';
import Markers from '../components/Markers';
import QueuePanel from '../components/QueuePanel';
import AddToPlaylist from '../components/AddToPlaylist';
import { isFullscreen as isPlayerFullscreen, onFullscreenChange, toggleFullscreen } from '../fullscreen';
import { enqueue, nextInQueue, previousInQueue, setAutoplay, startQueue, syncPlaying, useQueue } from '../queue';
import { fetchRandomTitles, playAll } from '../queueActions';
import { adminHref } from '../appLinks';
import { formatClock } from '../playerUtils';

function useMediaList(params) {
  const [items, setItems] = useState([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const key = JSON.stringify(params);
  const refresh = React.useCallback(async () => {
    if (!params) { setItems([]); setLoading(false); return; }
    setLoading(true);
    setError('');
    try {
      const result = await getMediaItems(params);
      setItems(result.items || []);
      setTotal(result.total ?? (result.items || []).length);
    } catch (cause) { setError(cause.message); }
    finally { setLoading(false); }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  useEffect(() => { refresh(); }, [refresh]);
  return { items, total, loading, error, refresh };
}

function displayTitle(item) {
  const title = item?.title || 'Untitled video';
  const clean = item?.studioName && title.toLowerCase().startsWith(`${item.studioName.toLowerCase()}: `)
    ? title.slice(item.studioName.length + 2) : title;
  return clean.replace(/\s*\([^()]{35,}\)\s*$/, '').trim() || title;
}

// Every title, person and studio gets one of eight "Spectrum" tones from its id, so a grid reads as
// varied colour instead of a grey wall, and the same thing always wears the same colour.
const TONE_COUNT = 8;
const toneOf = key => hashString(String(key)) % TONE_COUNT;
const toneClass = key => `rs-tone-${toneOf(key)}`;

function formatRuntime(seconds) {
  if (!seconds || seconds < 60) return '';
  const minutes = Math.round(seconds / 60);
  return minutes >= 60 ? `${Math.floor(minutes / 60)}h ${String(minutes % 60).padStart(2, '0')}m` : `${minutes}m`;
}

const NEW_FOR_MS = 14 * 24 * 3600 * 1000;
const isNew = item => Boolean(item.dateAdded) && Date.now() - new Date(item.dateAdded).getTime() < NEW_FOR_MS;

// Up to two initials for poster fallbacks ("After Hours" -> "AH", "scene-100" -> "S1").
function titleInitials(title) {
  const words = String(title).replace(/[^\p{L}\p{N}\s-]/gu, '').split(/[\s-]+/).filter(Boolean);
  return (words.length > 1 ? words[0][0] + words[1][0] : (words[0] || '?').slice(0, 2)).toUpperCase();
}

function artSources(item, preferBackdrop, style) {
  const thumb = artworkUrl(item.id, 'thumbnail', item.posterImageId);
  const poster = item.posterImageId ? artworkUrl(item.id, 'poster', item.posterImageId) : null;
  const sources = [];
  if (preferBackdrop && item.backdropImageId) sources.push(artworkUrl(item.id, 'backdrop', item.backdropImageId));
  if (style === 'thumbnail' && !preferBackdrop) sources.push(thumb, poster);
  else sources.push(poster, thumb);
  return [...new Set(sources.filter(Boolean))];
}

function Artwork({ item, className = '', preferBackdrop = false }) {
  const [style] = useCardStyle();
  const identity = `${item.id}:${item.posterImageId || ''}:${item.backdropImageId || ''}:${preferBackdrop}:${style}`;
  const [bound, setBound] = useState(identity);
  const [step, setStep] = useState(0);
  if (bound !== identity) { setBound(identity); setStep(0); }
  const sources = artSources(item, preferBackdrop, style);
  const src = sources[Math.min(step, sources.length - 1)];
  const [loadedSrc, setLoadedSrc] = useState('');
  const reveal = node => { if (node?.complete && node.naturalWidth > 0) setLoadedSrc(src); };
  return <div className={`rs-art ${toneClass(item.id)} ${className}`} aria-hidden="true">
    <div className="rs-art-fallback">
      <span className="rs-art-glyph">{titleInitials(displayTitle(item))}</span>
      <span className="rs-art-studio">{item.studioName || 'ReelStash'}</span>
    </div>
    <img key={src} ref={reveal} src={src} alt="" decoding="async" className={loadedSrc === src ? 'is-loaded' : ''} onLoad={event => { if (event.currentTarget.naturalWidth > 0) setLoadedSrc(src); }} onError={() => { setLoadedSrc(''); if (step < sources.length - 1) setStep(next => next + 1); }} />
  </div>;
}

// A short silent clip that plays while the pointer rests on a card (mouse devices only).
const canHover = () => typeof window !== 'undefined' && !!window.matchMedia?.('(hover: hover)').matches;

function CardPreview({ item }) {
  const [failed, setFailed] = useState(false);
  if (failed) return null;
  return <video className="rs-card-preview" ref={el => { if (el) el.muted = true; }} src={previewUrl(item.id)} muted loop autoPlay playsInline preload="auto" aria-hidden="true" tabIndex={-1} onError={() => setFailed(true)} />;
}

// A 5-star rating control. Ratings are stored server-side on a 0-10 scale (half-star precision
// isn't exposed in the UI), so callers convert with starsFromRating / ratingFromStars.
const starsFromRating = rating => rating ? Math.round(rating / 2) : 0;
const ratingFromStars = stars => stars * 2;

function StarRating({ value = 0, onChange, size = 16 }) {
  const stars = [1, 2, 3, 4, 5];
  if (!onChange) {
    if (!value) return null;
    return <span className="rs-star-rating is-readonly" aria-label={`Rated ${value} out of 5 stars`}>
      {stars.map(n => <Star key={n} size={size} fill={n <= value ? 'currentColor' : 'none'} />)}
    </span>;
  }
  return <div className="rs-star-rating" role="radiogroup" aria-label="Your rating">
    {stars.map(n => <button key={n} type="button" role="radio" aria-checked={n === value} aria-label={`${n} star${n > 1 ? 's' : ''}`} className={n <= value ? 'is-filled' : ''} onClick={() => onChange(n === value ? 0 : n)}>
      <Star size={size} fill={n <= value ? 'currentColor' : 'none'} />
    </button>)}
  </div>;
}

// Where the title page's back link returns to, and what it's called there.
const BACK_LABELS = { '/library': 'Library', '/favorites': 'Favorites', '/watch-later': 'Watch later', '/search': 'Search', '/performers': 'Performers', '/studios': 'Studios', '/series': 'Series', '/categories': 'Categories', '/playlists': 'Playlists', '/activity': 'Activity', '/movies': 'Movies', '/shows': 'TV shows', '/directors': 'Directors', '/': 'Home' };
export function backLabel(from) {
  const path = from.split('?')[0];
  if (BACK_LABELS[path]) return BACK_LABELS[path];
  const detail = path.match(/^\/(performers|studios|series|categories)\/[^/]+$/);
  return detail ? { performers: 'Performer', studios: 'Studio', series: 'Series', categories: 'Category' }[detail[1]] : 'Back';
}

function MediaCard({ item, exclude = [], kicker, showPlayCount = false, mostPlayed = false, wide = false }) {
  const location = useLocation();
  const [previewing, setPreviewing] = useState(false);
  const hoverTimer = useRef();
  useEffect(() => () => clearTimeout(hoverTimer.current), []);
  const startPreview = () => { if (item.hasPreview && canHover()) { clearTimeout(hoverTimer.current); hoverTimer.current = setTimeout(() => setPreviewing(true), 350); } };
  const stopPreview = () => { clearTimeout(hoverTimer.current); setPreviewing(false); };
  const skip = exclude.map(name => String(name).trim().toLowerCase());
  const people = (item.performers || []).filter(name => !skip.includes(String(name).trim().toLowerCase()));
  const progress = item.positionSeconds && item.durationSeconds
    ? Math.min(100, Math.max(4, (item.positionSeconds / item.durationSeconds) * 100)) : null;
  const runtime = progress != null ? `${formatRuntime(item.durationSeconds - item.positionSeconds) || '1m'} left` : formatRuntime(item.durationSeconds);
  const fresh = isNew(item) && !item.watched && !item.positionSeconds;
  return <Link className={`rs-media-card${wide ? ' is-wide' : ''}`} to={`/library/${item.id}`} state={{ from: `${location.pathname}${location.search}` }} aria-label={`${displayTitle(item)}${item.studioName ? `, ${item.studioName}` : ''}`} onMouseEnter={startPreview} onMouseLeave={stopPreview} onFocus={startPreview} onBlur={stopPreview}>
    <div className="rs-media-card-art">
      <Artwork item={item} preferBackdrop={wide} />
      {previewing && <CardPreview item={item} />}
      <span className="rs-card-play" aria-hidden="true"><Play size={18} fill="currentColor" /></span>
      {(fresh || item.watched) && <span className="rs-card-badges">{fresh && <span className="rs-badge rs-badge-new">New</span>}{item.watched && progress == null && <span className="rs-badge rs-badge-watched"><Check size={11} strokeWidth={3} /> Watched</span>}</span>}
      {(item.rating > 0 || item.favorite) && <span className="rs-card-indicators">
        {item.rating > 0 && <span className="rs-card-rating" title={`Average rating ${(item.rating / 2).toFixed(1)} out of 5`} aria-label={`Average rating ${(item.rating / 2).toFixed(1)} out of 5`}><Star size={12} fill="currentColor" />{(item.rating / 2).toFixed(1)}</span>}
        {item.favorite && <span className="rs-card-favorite" title="Favorite" aria-label="Favorite"><Heart size={13} fill="currentColor" /></span>}
      </span>}
      {runtime && <span className={`rs-card-runtime${progress != null ? ' is-remaining' : ''}`}>{runtime}</span>}
      {progress != null && <span className="rs-card-progress"><span style={{ width: `${progress}%` }} /></span>}
    </div>
    <div className="rs-card-copy">
      <span className="rs-card-kicker">{kicker || item.reason || item.studioName || item.seriesName || 'Your library'}</span>
      <h3 title={item.title}>{displayTitle(item)}</h3>
      {!!people.length && <p className="rs-card-people">{people.slice(0, 2).join(' · ')}</p>}
      {showPlayCount && <span className="rs-card-plays">{mostPlayed && <strong>Most played · </strong>}{item.watchCount || 0} {item.watchCount === 1 ? 'play' : 'plays'}</span>}
    </div>
  </Link>;
}

// A horizontal row with page-by-page arrow buttons (shown on devices that can hover; touch just swipes).
function Shelf({ title, eyebrow, href, hrefLabel = 'View all', items, tone, limit = 12, className = '', renderItem = item => <MediaCard key={item.id} item={item} /> }) {
  const track = useRef(null);
  const [edge, setEdge] = useState({ start: true, end: true });
  const syncEdges = React.useCallback(() => {
    const el = track.current;
    if (el) setEdge({ start: el.scrollLeft < 4, end: el.scrollLeft + el.clientWidth >= el.scrollWidth - 4 });
  }, []);
  useEffect(() => {
    syncEdges();
    const el = track.current;
    if (!el || typeof ResizeObserver === 'undefined') return undefined;
    const observer = new ResizeObserver(syncEdges);
    observer.observe(el);
    return () => observer.disconnect();
  }, [items.length, syncEdges]);
  if (!items.length) return null;
  const page = direction => track.current?.scrollBy({ left: direction * track.current.clientWidth * 0.85, behavior: 'smooth' });
  return <section className={`rs-shelf ${tone != null ? `rs-tone-${tone}` : ''} ${className}`}>
    <div className="rs-section-head"><div><p className="rs-eyebrow">{eyebrow}</p><h2 title={title}>{title}</h2></div>
      <div className="rs-shelf-actions">
        {!(edge.start && edge.end) && <span className="rs-shelf-arrows"><button type="button" aria-label={`Scroll ${title} back`} disabled={edge.start} onClick={() => page(-1)}><ArrowLeft size={16} /></button><button type="button" aria-label={`Scroll ${title} forward`} disabled={edge.end} onClick={() => page(1)}><ArrowRight size={16} /></button></span>}
        {href && <Link to={href}>{hrefLabel} <ArrowRight size={16} /></Link>}
      </div>
    </div>
    <div className="rs-shelf-track" ref={track} onScroll={syncEdges}>{items.slice(0, limit).map(renderItem)}</div>
  </section>;
}

// Deterministic per-day pick of `count` items from `pool`, seeded by `seedKey` (e.g. today's date)
// so every visitor sees the same slideshow order and it only changes once a day.
function dailySelection(pool, count, seedKey) {
  if (!pool.length) return [];
  let seed = hashString(seedKey);
  const rand = () => { seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0; return seed / 4294967296; };
  const shuffled = [...pool];
  for (let i = shuffled.length - 1; i > 0; i--) {
    const j = Math.floor(rand() * (i + 1));
    [shuffled[i], shuffled[j]] = [shuffled[j], shuffled[i]];
  }
  return shuffled.slice(0, count);
}

const HERO_AUTOPLAY_MS = 7000;
const HERO_SLIDE_COUNT = 10;

function HeroCarousel({ items, eyebrow = 'FROM YOUR COLLECTION', exclude = [], bleed = false }) {
  const [active, setActive] = useState(0);
  const [paused, setPaused] = useState(false);
  const touchStart = useRef(null);
  const count = items.length;
  useEffect(() => { setActive(0); }, [items]);
  function goTo(index) { setActive((index + count) % count); }

  useEffect(() => {
    if (paused || count < 2) return undefined;
    const timer = setTimeout(() => setActive(current => (current + 1) % count), HERO_AUTOPLAY_MS);
    return () => clearTimeout(timer);
  }, [count, paused, active]);

  if (!items[active]) return null;
  const skip = new Set(exclude.filter(Boolean).map(value => value.toLowerCase()));
  const metaFor = item => [item.studioName, item.seriesName, (item.performers || []).filter(person => !skip.has(String(person).toLowerCase())).slice(0, 2).join(', ')].filter(Boolean).join('  ·  ') || 'From your collection';

  function onKeyDown(event) {
    if (count < 2) return;
    if (event.key === 'ArrowLeft') { event.preventDefault(); goTo(active - 1); }
    if (event.key === 'ArrowRight') { event.preventDefault(); goTo(active + 1); }
  }
  function onTouchEnd(event) {
    if (touchStart.current == null || count < 2) return;
    const delta = event.changedTouches[0].clientX - touchStart.current;
    touchStart.current = null;
    if (Math.abs(delta) > 45) goTo(active + (delta < 0 ? 1 : -1));
  }

  const slideMeta = item => {
    const year = (item.releaseDate || '').slice(0, 4) || (item.productionYear ? String(item.productionYear) : '');
    return [year, formatRuntime(item.durationSeconds), item.rating > 0 ? `★ ${(item.rating / 2).toFixed(1)}` : '', item.studioName].filter(Boolean);
  };
  const people = item => (item.performers || []).filter(person => !skip.has(String(person).toLowerCase())).slice(0, 3);
  return <section className={`rs-feature rs-hero${bleed ? ' rs-hero-bleed' : ''}${paused ? ' is-paused' : ''}`} aria-roledescription="carousel" aria-label="Featured titles" tabIndex={count > 1 ? 0 : undefined}
    style={{ '--hero-interval': `${HERO_AUTOPLAY_MS}ms` }}
    onMouseEnter={() => setPaused(true)} onMouseLeave={() => setPaused(false)} onFocus={() => setPaused(true)} onBlur={() => setPaused(false)}
    onKeyDown={onKeyDown} onTouchStart={event => { touchStart.current = event.touches[0].clientX; }} onTouchEnd={onTouchEnd}>
    {items.map((item, index) => {
      const resumable = item.positionSeconds > 0 && item.durationSeconds > item.positionSeconds;
      const cast = people(item);
      const tabIndex = index === active ? undefined : -1;
      return <div key={item.id} className={`rs-hero-slide${index === active ? ' is-active' : ''}`} aria-hidden={index !== active} aria-roledescription="slide" aria-label={`${index + 1} of ${count}`}>
        <Artwork item={item} className="rs-feature-art" preferBackdrop />
        <div className="rs-feature-shade" />
        <div className="rs-feature-copy">
          <p className="rs-hero-kicker"><span className="rs-live-dot" /> {eyebrow}</p>
          <h2 title={item.title}>{displayTitle(item)}</h2>
          <p className="rs-hero-meta">{slideMeta(item).map((part, i) => <span key={i}>{part}</span>)}</p>
          {item.description ? <p className="rs-hero-desc">{item.description}</p> : <p className="rs-hero-desc is-faint">{metaFor(item)}</p>}
          {!!cast.length && <p className="rs-hero-people">With {cast.join(', ')}</p>}
          <div className="rs-feature-actions">
            <Link className="rs-button rs-button-primary rs-button-lg" to={`/library/${item.id}`} state={{ autoplay: true }} tabIndex={tabIndex}><Play size={18} fill="currentColor" /> {resumable ? `Resume · ${formatRuntime(item.durationSeconds - item.positionSeconds) || '1m'} left` : 'Play'}</Link>
            <Link className="rs-button rs-button-glass rs-button-lg" to={`/library/${item.id}`} tabIndex={tabIndex}>Details <ArrowRight size={17} /></Link>
            <HeroSaveButton item={item} tabIndex={tabIndex} />
          </div>
          {resumable && <span className="rs-hero-progress" aria-hidden="true"><span style={{ width: `${Math.min(100, item.positionSeconds / item.durationSeconds * 100)}%` }} /></span>}
        </div>
      </div>;
    })}
    {count > 1 && <div className="rs-hero-rail">
      <div className="rs-hero-thumbs">{items.map((item, index) => <button key={item.id} type="button" className={`rs-hero-thumb${index === active ? ' is-active' : ''}`} aria-label={`Show ${displayTitle(item)}`} aria-current={index === active} onClick={() => goTo(index)}><Artwork item={item} preferBackdrop /><span className="rs-hero-thumb-timer" /></button>)}</div>
      <div className="rs-hero-nav">
        <span className="rs-hero-count">{String(active + 1).padStart(2, '0')} / {String(count).padStart(2, '0')}</span>
        <button type="button" aria-label="Previous title" onClick={() => goTo(active - 1)}><ArrowLeft size={17} /></button>
        <button type="button" aria-label="Next title" onClick={() => goTo(active + 1)}><ArrowRight size={17} /></button>
      </div>
    </div>}
  </section>;
}

// Watch later from the featured slideshow, without leaving Home.
function HeroSaveButton({ item, tabIndex }) {
  const { toast } = useToast();
  const [saved, setSaved] = useState(!!item.watchLater);
  async function toggle() {
    try {
      if (saved) await removeWatchLater(item.id); else await addWatchLater(item.id);
      setSaved(!saved);
      toast({ title: saved ? 'Removed from Watch later' : 'Saved to Watch later', tone: 'success' });
    } catch (err) { toast({ title: 'Could not update Watch later', description: err.message, tone: 'danger' }); }
  }
  return <button type="button" className="rs-button rs-button-glass rs-button-lg rs-button-icon" aria-pressed={saved} aria-label={saved ? 'Remove from Watch later' : 'Add to Watch later'} title={saved ? 'In Watch later' : 'Watch later'} tabIndex={tabIndex} onClick={toggle}>{saved ? <BookmarkCheck size={19} /> : <BookmarkPlus size={19} />}</button>;
}

function Status({ loading, error, empty, retry, title, message, href, action, icon: Icon = Film }) {
  if (!loading && !error && !empty) return null;
  const tone = loading ? 'is-loading' : error ? 'is-error' : 'is-empty';
  const StateIcon = loading ? RefreshCw : error ? AlertTriangle : Icon;
  return <div className={`rs-status ${tone}`} role={error ? 'alert' : 'status'} aria-live={error ? 'assertive' : 'polite'}>
    <span className="rs-status-icon"><StateIcon className={loading ? 'rs-spin' : undefined} size={26} strokeWidth={1.75} /></span>
    <h2>{loading ? 'Loading your library' : error ? 'This page didn’t load' : title || 'Nothing here yet'}</h2>
    <p>{loading ? 'One moment.' : error ? 'Something went wrong on the way. Check your connection and try again.' : message || 'Titles show up here once they’re in your library.'}</p>
    {error && typeof error === 'string' && <code className="rs-status-detail">{error}</code>}
    {(error && retry) || (!loading && !error && href) ? <div className="rs-status-actions">
      {error && retry && <button type="button" className="rs-button rs-button-primary" onClick={retry}><RefreshCw size={16} /> Try again</button>}
      {!loading && !error && href && <Link className="rs-button rs-button-outline" to={href}>{action} <ArrowRight size={16} /></Link>}
    </div> : null}
  </div>;
}

function greetingFor(date = new Date()) {
  const hour = date.getHours();
  return hour < 5 ? 'Up late' : hour < 12 ? 'Good morning' : hour < 17 ? 'Good afternoon' : hour < 22 ? 'Good evening' : 'Up late';
}

const plural = (count, one, many = `${one}s`) => `${count.toLocaleString()} ${count === 1 ? one : many}`;

// One of the four big "Explore" tiles on Home, with a peek of what's inside.
function ExploreTile({ to, tone, icon: Icon, label, count, unit, units, peek, chips }) {
  return <Link to={to} className={`rs-explore-tile rs-tone-${tone}`}>
    <span className="rs-explore-main">
      <span className="rs-explore-icon"><Icon size={20} /></span>
      <span className="rs-explore-copy"><strong>{label}</strong><small>{count == null ? '…' : count === 0 ? 'None yet' : plural(count, unit, units)}</small></span>
    </span>
    {peek && <span className="rs-explore-peek" aria-hidden="true">{peek}</span>}
    {!peek && !!chips?.length && <span className="rs-explore-chips" aria-hidden="true">{chips.map(chip => <em key={chip}>{chip}</em>)}</span>}
    <span className="rs-explore-go" aria-hidden="true"><ArrowUpRight size={16} /></span>
  </Link>;
}

// "Part of <series>" on the video page: the series in order beside the player, with the current
// title marked and the next one called out.
function SeriesSideList({ item, nextEpisode }) {
  const { terms, mode } = useLibrary();
  const queue = useQueue();
  const [parts, setParts] = useState([]);
  useEffect(() => {
    let live = true;
    setParts([]);
    if (item.seriesId) getMediaItems({ seriesId: item.seriesId, limit: 200 }).then(result => { if (live) setParts(result.items || []); }).catch(() => {});
    return () => { live = false; };
  }, [item.seriesId]);
  if (!item.seriesId || parts.length < 2) return null;
  const position = parts.findIndex(part => String(part.id) === String(item.id));
  const nextId = nextEpisode ? String(nextEpisode.id) : position >= 0 && parts[position + 1] ? String(parts[position + 1].id) : null;
  const episodes = item.kind === 'episode';
  const watched = parts.filter(part => part.watched).length;
  const route = `/${episodes && mode === 'entertainment' ? 'shows' : 'series'}/${item.seriesId}`;
  const backToPlayer = event => { event.preventDefault(); window.scrollTo({ top: 0, behavior: 'smooth' }); };
  return <aside className="rs-watch-side rs-series-side" aria-label={`In ${item.seriesName || terms.series}`}>
    <div className="rs-side-head">
      <Link to={route} className="rs-side-title">{item.seriesName || (episodes ? terms.show : terms.series)}</Link>
      <span className="rs-side-meta">{watched} of {parts.length} watched</span>
    </div>
    <label className="rs-side-toggle"><span>Autoplay next {episodes ? 'episode' : 'part'}</span><input type="checkbox" checked={queue.autoplay} onChange={event => setAutoplay(event.target.checked)} /></label>
    <ol className="rs-side-list">
      {parts.map((part, index) => {
        const current = String(part.id) === String(item.id);
        const next = String(part.id) === nextId;
        const progress = part.positionSeconds && part.durationSeconds ? Math.min(100, part.positionSeconds / part.durationSeconds * 100) : part.watched ? 100 : 0;
        const state = current ? 'Now playing' : next ? 'Up next' : part.watched ? 'Watched' : 'Not watched';
        return <li key={part.id}>
          <Link to={`/library/${part.id}`} state={{ from: route }} className={`rs-side-item${current ? ' is-current' : ''}${next ? ' is-next' : ''}`} aria-current={current ? 'true' : undefined} onClick={current ? backToPlayer : undefined}>
            <span className="rs-side-art"><Artwork item={part} preferBackdrop /><span className="rs-side-progress" style={{ width: `${progress}%` }} /></span>
            <span className="rs-side-copy">
              <small>{episodes ? 'Episode' : 'Part'} {index + 1}{part.durationSeconds ? ` · ${formatRuntime(part.durationSeconds)}` : ''}</small>
              <strong>{displayTitle(part)}</strong>
              <em>{state}</em>
            </span>
          </Link>
        </li>;
      })}
    </ol>
  </aside>;
}

// Beside the player when the title isn't part of a series: a short list of similar titles.
function SimilarSideList({ items }) {
  if (!items.length) return null;
  return <aside className="rs-watch-side" aria-label="Watch next">
    <div className="rs-side-head"><span className="rs-side-title">Watch next</span><span className="rs-side-meta">Similar titles</span></div>
    <ol className="rs-side-list">
      {items.slice(0, 6).map(part => <li key={part.id}><Link to={`/library/${part.id}`} className="rs-side-item">
        <span className="rs-side-art"><Artwork item={part} preferBackdrop /></span>
        <span className="rs-side-copy"><small>{[part.studioName, formatRuntime(part.durationSeconds)].filter(Boolean).join(' · ') || 'Your library'}</small><strong>{displayTitle(part)}</strong>{part.reason && <em>{part.reason}</em>}</span>
      </Link></li>)}
    </ol>
  </aside>;
}

const bestFile = item => (item.files || []).filter(file => file.available).sort((a, b) => (b.height || 0) - (a.height || 0))[0] || (item.files || [])[0];
function qualityLabel(file) {
  if (!file?.width || !file?.height) return '';
  const short = Math.min(file.width, file.height);
  const long = Math.max(file.width, file.height);
  return long >= 3800 || short >= 2100 ? '4K' : short >= 1000 ? '1080p' : short >= 700 ? '720p' : 'SD';
}
const formatBytes = bytes => { if (!bytes) return ''; const units = ['B', 'KB', 'MB', 'GB', 'TB']; let n = bytes; let i = 0; while (n >= 1024 && i < units.length - 1) { n /= 1024; i += 1; } return `${n.toFixed(n >= 10 || i === 0 ? 0 : 1)} ${units[i]}`; };
const formatBitrate = bits => bits ? (bits >= 1e6 ? `${(bits / 1e6).toFixed(1)} Mbps` : `${Math.round(bits / 1e3)} kbps`) : '';

// The file the player opens: resolution, codecs, size and where it lives.
// Plain-language names for what the file is: picture quality, video and sound formats.
function resolutionName(file) {
  if (!file?.width || !file?.height) return '';
  const long = Math.max(file.width, file.height);
  const short = Math.min(file.width, file.height);
  if (long >= 7000) return '8K';
  if (long >= 3800 || short >= 2100) return '4K Ultra HD';
  if (long >= 2500 || short >= 1400) return '2K Quad HD';
  if (long >= 1800 || short >= 1000) return 'Full HD';
  if (long >= 1200 || short >= 700) return 'HD';
  return 'SD';
}
const VIDEO_NAMES = { h264: 'H.264', avc: 'H.264', avc1: 'H.264', hevc: 'HEVC (H.265)', h265: 'HEVC (H.265)', av1: 'AV1', vp9: 'VP9', vp8: 'VP8', mpeg4: 'MPEG-4', mpeg2video: 'MPEG-2', vc1: 'VC-1', wmv3: 'WMV' };
const AUDIO_NAMES = { aac: 'AAC', ac3: 'Dolby Digital', eac3: 'Dolby Digital Plus', truehd: 'Dolby TrueHD', dts: 'DTS', 'dts-hd': 'DTS-HD', mp3: 'MP3', opus: 'Opus', flac: 'FLAC', vorbis: 'Vorbis', pcm_s16le: 'PCM', alac: 'ALAC' };
const codecName = (table, codec) => (codec ? table[String(codec).toLowerCase()] || String(codec).toUpperCase() : '');
const isDolby = codec => /^(ac3|eac3|truehd)$/i.test(codec || '');

// The file the player opens, in words a viewer understands: quality, video and sound formats, size.
function TechnicalCard({ item }) {
  const file = bestFile(item);
  if (!file) return null;
  const quality = resolutionName(file);
  const badges = [quality, codecName(VIDEO_NAMES, file.videoCodec), isDolby(file.audioCodec) ? 'Dolby Audio' : '', file.frameRate >= 48 ? `${Math.round(file.frameRate)} fps` : ''].filter(Boolean);
  const rows = [
    ['Picture', [quality, file.width && file.height ? `${file.width} × ${file.height}` : ''].filter(Boolean).join(' · ')],
    ['Video', [codecName(VIDEO_NAMES, file.videoCodec), file.frameRate ? `${Math.round(file.frameRate * 100) / 100} fps` : '', formatBitrate(file.videoBitrate)].filter(Boolean).join(' · ')],
    ['Sound', [codecName(AUDIO_NAMES, file.audioCodec), formatBitrate(file.audioBitrate)].filter(Boolean).join(' · ')],
    ['Size', [formatBytes(file.fileSize), (file.container || file.extension || '').toUpperCase()].filter(Boolean).join(' · ')],
    ...(item.files?.length > 1 ? [['Versions', `${item.files.length} files`]] : []),
  ].filter(([, value]) => value);
  return <section className="rs-tech" aria-labelledby="rs-tech-title">
    <p className="rs-eyebrow">File</p>
    <h2 id="rs-tech-title">Technical</h2>
    {!!badges.length && <div className="rs-tech-badges">{badges.map(badge => <span key={badge} className={/Dolby/.test(badge) ? 'is-dolby' : /Ultra|8K|2K/.test(badge) ? 'is-top' : ''}>{badge}</span>)}</div>}
    <dl className="rs-tech-list">{rows.map(([label, value]) => <div key={label}><dt>{label}</dt><dd title={value}>{value}</dd></div>)}</dl>
    {!file.available && <p className="rs-tech-offline">This file is offline right now.</p>}
  </section>;
}

function FaceCard({ person }) {
  return <Link to={`/performers/${person.id}`} className={`rs-face ${toneClass(person.id)}`} aria-label={`${person.name}, ${countLabel(person.mediaCount || 0)}`}>
    <span className="rs-face-ring"><DirectoryMark item={person} index={0} type="performers" portrait />{person.favorite && <Heart className="rs-face-heart" size={12} fill="currentColor" />}</span>
    <strong>{person.name}</strong>
    <small>{countLabel(person.mediaCount || 0)}</small>
  </Link>;
}

export function Home({ session }) {
  const { terms: homeTerms, mode: homeMode } = useLibrary();
  const { items, total, loading, error, refresh } = useMediaList({ limit: 48 });
  const { items: heroPool } = useMediaList({ limit: 100, hasArtwork: 'true' });
  const { items: favorites } = useMediaList({ limit: 24, favorite: 'true' });
  const [continueItems, setContinueItems] = useState([]);
  const [nextUpItems, setNextUpItems] = useState([]);
  const [studios, setStudios] = useState([]);
  const [seriesNames, setSeriesNames] = useState([]);
  const [recs, setRecs] = useState([]);
  const [people, setPeople] = useState([]);
  const [moods, setMoods] = useState([]);
  const [counts, setCounts] = useState({});
  const [summary, setSummary] = useState(null);
  const [seriesProgress, setSeriesProgress] = useState([]);
  const { items: quickItems } = useMediaList({ limit: 16, maxDuration: 900, watched: 'unwatched', sort: 'random' });
  useEffect(() => { getHomeRecommendations().then(result => setRecs(result.sections || [])).catch(() => {}); }, []);
  useEffect(() => { getHomeSummary().then(setSummary).catch(() => {}); }, []);
  useEffect(() => { getHomeSeries(6).then(result => setSeriesProgress(result.items || [])).catch(() => {}); }, []);
  useEffect(() => { getContinueWatching({ limit: 12 }).then(result => setContinueItems(result.items || [])).catch(() => {}); }, []);
  useEffect(() => { getNextUp({ limit: 12 }).then(result => setNextUpItems(result.items || [])).catch(() => {}); }, []);
  useEffect(() => {
    getStudios({ limit: 60 }).then(result => {
      setStudios((result.items || []).filter(s => s.mediaCount > 0).sort((a, b) => b.mediaCount - a.mediaCount).slice(0, 8));
      setCounts(current => ({ ...current, studios: result.total ?? (result.items || []).length }));
    }).catch(() => {});
    // Faces: favourites first, then people with a photo, then whoever has the most titles.
    getPerformers({ limit: 40 }).then(result => {
      const ranked = (result.items || []).filter(p => p.mediaCount > 0)
        .sort((a, b) => Number(b.favorite) - Number(a.favorite) || Number(!!b.profileImageId) - Number(!!a.profileImageId) || b.mediaCount - a.mediaCount);
      setPeople(ranked);
      setCounts(current => ({ ...current, performers: result.total ?? ranked.length }));
    }).catch(() => {});
    getSeries({ limit: 4 }).then(result => {
      setCounts(current => ({ ...current, series: result.total ?? (result.items || []).length }));
      setSeriesNames((result.items || []).map(item => item.name || item.title).filter(Boolean).slice(0, 3));
    }).catch(() => {});
    getCategories().then(result => {
      const used = (result.items || []).filter(c => c.mediaCount > 0).sort((a, b) => b.mediaCount - a.mediaCount);
      setMoods(used.slice(0, 18));
      setCounts(current => ({ ...current, categories: used.length }));
    }).catch(() => {});
  }, []);

  // Prefer titles with real artwork, but few titles may have any: top up from the rest of the
  // library so the slideshow always has HERO_SLIDE_COUNT slides (artwork titles first).
  const today = new Date().toDateString();
  const heroItems = useMemo(() => {
    const withBackdrop = heroPool.filter(item => item.backdropImageId);
    const withArt = [...dailySelection(withBackdrop, HERO_SLIDE_COUNT, today), ...dailySelection(heroPool.filter(item => !item.backdropImageId), HERO_SLIDE_COUNT, today)].slice(0, HERO_SLIDE_COUNT);
    if (withArt.length >= HERO_SLIDE_COUNT) return withArt;
    const seen = new Set(withArt.map(item => item.id));
    const rest = dailySelection(items.filter(item => !seen.has(item.id)), HERO_SLIDE_COUNT - withArt.length, today);
    return [...withArt, ...rest];
  }, [heroPool, items, today]);
  const fresh = items.slice(0, 12);
  const newThisWeek = items.filter(isNew).length;
  const faces = people.filter(p => p.profileImageId).slice(0, 4);
  const peekFaces = (faces.length ? faces : people.slice(0, 4)).map(person => <span key={person.id} className="rs-peek-face"><DirectoryMark item={person} index={0} type="performers" portrait /></span>);
  const peekStudios = studios.slice(0, 4).map(studio => <span key={studio.id} className="rs-peek-studio"><DirectoryMark item={studio} index={0} type="studios" /></span>);

  return <div className="rs-home">
    {heroItems.length ? <HeroCarousel items={heroItems} eyebrow="Featured from your library" bleed /> : !loading && !error && <div className="rs-home-empty"><span>Start here</span><h2>Your library is empty.</h2><p>The setup wizard in Admin walks you through adding your videos. Your titles will land on this page.</p><a className="rs-button rs-button-primary" href={adminHref('/admin/setup')}>Set up your library <ArrowRight size={17} /></a></div>}
    <Status loading={loading && !items.length} error={error} retry={refresh} />
    <div className="rs-home-body">
      {summary && summary.titles > 0 && <LibraryPulse summary={summary} newThisWeek={newThisWeek} />}
      <Shelf title="Continue watching" eyebrow="Pick up where you left off" href="/activity" hrefLabel="History" className="rs-shelf--wide" items={continueItems} renderItem={item => <MediaCard key={item.id} item={item} kicker={resumeKicker(item)} wide />} />
      <Shelf title="Next up" eyebrow="Your next episodes" items={nextUpItems} renderItem={item => <MediaCard key={item.id} item={item} kicker={nextUpKicker(item)} />} />
      {!!seriesProgress.length && <SeriesProgress items={seriesProgress} />}
      {recs.slice(0, 1).map(section => <Shelf key={section.key} title={section.title} eyebrow={section.subtitle || 'Picked for you'} items={section.items} />)}
      <Shelf title="Recently added" eyebrow="Fresh arrivals" href="/library" items={fresh} />
      {total > 0 && <section className="rs-explore" aria-labelledby="rs-explore-title">
        <div className="rs-section-head"><div><p className="rs-eyebrow">Explore</p><h2 id="rs-explore-title">Find something new</h2></div></div>
        <div className="rs-explore-grid">
          <ExploreTile to="/performers" icon={Users} label={homeTerms.people} count={counts.performers} unit="person" units="people" peek={peekFaces.length ? peekFaces : null} />
          <ExploreTile to="/studios" icon={Building2} label={homeTerms.studios} count={counts.studios} unit="studio" peek={peekStudios.length ? peekStudios : null} />
          <ExploreTile to={homeMode === 'entertainment' ? '/shows' : '/series'} icon={ListVideo} label={homeMode === 'entertainment' ? homeTerms.shows : homeTerms.seriesPlural} count={counts.series} unit="series" units="series" chips={seriesNames} />
          <ExploreTile to="/categories" icon={Layers3} label={homeTerms.categories} count={counts.categories} unit={homeTerms.category.toLowerCase()} units={homeTerms.categories.toLowerCase()} chips={moods.slice(0, 4).map(m => m.name)} />
        </div>
      </section>}
      <Shelf title="Faces in your library" eyebrow={homeTerms.people} href="/performers" hrefLabel={`All ${homeTerms.people.toLowerCase()}`} className="rs-faces" limit={20} items={people} renderItem={person => <FaceCard key={person.id} person={person} />} />
      <Shelf title="Your favorites" eyebrow="Saved with love" href="/favorites" items={favorites.slice(0, 12)} />
      <Shelf title="Under 15 minutes" eyebrow="Short on time" href="/library" items={quickItems} renderItem={item => <MediaCard key={item.id} item={item} kicker={item.studioName || 'Not watched yet'} />} />
      {recs.slice(1).map(section => <Shelf key={section.key} title={section.title} eyebrow={section.subtitle || 'Picked for you'} items={section.items} />)}
      {!!moods.length && <section className="rs-moods" aria-labelledby="rs-moods-title">
        <div className="rs-section-head"><div><p className="rs-eyebrow">{homeTerms.categories}</p><h2 id="rs-moods-title">Browse by mood</h2></div><Link to="/categories" className="rs-head-link">All {homeTerms.categories.toLowerCase()} <ArrowRight size={16} /></Link></div>
        <div className="rs-mood-grid">{moods.slice(0, 12).map(mood => <Link key={mood.id} to={`/categories/${mood.id}`} className={`rs-mood-tile ${toneClass(mood.name)}`}><strong>{mood.name}</strong><small>{plural(mood.mediaCount, 'title')}</small></Link>)}</div>
      </section>}
      {!!studios.length && <section className="rs-studios"><div className="rs-section-head"><div><p className="rs-eyebrow">Most titles</p><h2>{homeTerms.studios} in your library</h2></div><Link to="/studios" className="rs-head-link">All {homeTerms.studios.toLowerCase()} <ArrowRight size={16} /></Link></div><div className="rs-studio-grid">{studios.slice(0, 8).map(studio => <Link key={studio.id} to={`/studios/${studio.id}`}><span className="rs-home-studio-mark"><DirectoryMark item={studio} index={studio.id} type="studios" /></span><span className="rs-home-studio-copy"><strong>{studio.name}</strong><small>{plural(studio.mediaCount, 'title')}</small></span><ArrowUpRight size={18} /></Link>)}</div></section>}
    </div>
  </div>;
}

const hoursLabel = seconds => {
  const hours = seconds / 3600;
  if (hours >= 10) return `${Math.round(hours).toLocaleString()} hours`;
  if (hours >= 1) return `${hours.toFixed(1)} hours`;
  return `${Math.max(1, Math.round(seconds / 60))} min`;
};
const watchTimeLabel = seconds => {
  const minutes = Math.round(seconds / 60);
  return minutes >= 60 ? `${Math.floor(minutes / 60)}h ${String(minutes % 60).padStart(2, '0')}m` : `${minutes}m`;
};

// "Library at a glance" on Home: four quick numbers, each a door to the page that explains it.
function LibraryPulse({ summary }) {
  const { isAdmin } = useLibrary();
  const finished = summary.playsThisMonth ? Math.round(summary.completedThisMonth / summary.playsThisMonth * 100) : 0;
  const cards = [
    { label: 'In your library', value: plural(summary.titles, 'title'), sub: summary.totalSeconds ? `${hoursLabel(summary.totalSeconds)} to watch` : 'Ready to watch', to: '/library' },
    { label: 'Added this week', value: `${summary.addedThisWeek.toLocaleString()} new`, sub: summary.addedThisWeek ? `${summary.addedThisWeekUnseen.toLocaleString()} not opened yet` : 'Nothing new this week', to: '/library?added=7' },
    { label: 'Watched this month', value: watchTimeLabel(summary.watchSecondsThisMonth), sub: summary.playsThisMonth ? `${plural(summary.playsThisMonth, 'play')} · ${finished}% finished` : 'No plays yet this month', to: '/activity' },
    ...(isAdmin && summary.needsDetails > 0 ? [{ label: 'Waiting for details', value: plural(summary.needsDetails, 'title'), sub: 'Match them to add people & tags', href: adminHref('/admin/review') }] : []),
  ];
  return <section className="rs-pulse" aria-label="Library at a glance">
    {cards.map(card => {
      const body = <><span className="rs-pulse-label">{card.label}</span><strong className="rs-pulse-value">{card.value}</strong><span className="rs-pulse-sub">{card.sub}</span></>;
      return card.href ? <a key={card.label} href={card.href} className="rs-pulse-card">{body}</a> : <Link key={card.label} to={card.to} className="rs-pulse-card">{body}</Link>;
    })}
  </section>;
}

// "Up next in your series": series you're part-way through, with the part to watch next.
function SeriesProgress({ items }) {
  const { terms, mode } = useLibrary();
  return <section className="rs-keepgoing" aria-labelledby="rs-keepgoing-title">
    <div className="rs-section-head"><div><p className="rs-eyebrow">Keep going</p><h2 id="rs-keepgoing-title">Up next in your {mode === 'entertainment' ? 'shows' : terms.seriesPlural.toLowerCase()}</h2></div></div>
    <div className="rs-keepgoing-grid">
      {items.map(series => {
        const next = series.next;
        const route = `/${series.kind === 'show' && mode === 'entertainment' ? 'shows' : 'series'}/${series.id}`;
        return <article key={series.id} className="rs-keepgoing-card">
          <Link to={`/library/${next.id}`} state={{ autoplay: true, from: '/' }} className="rs-keepgoing-art" aria-label={`Play ${displayTitle(next)}`}><Artwork item={next} preferBackdrop /><span className="rs-card-play" aria-hidden="true"><Play size={16} fill="currentColor" /></span></Link>
          <div className="rs-keepgoing-copy">
            <span className="rs-keepgoing-next">Next: Part {series.nextPosition} · {formatRuntime(next.durationSeconds) || displayTitle(next)}</span>
            <Link to={route} className="rs-keepgoing-name">{series.name}</Link>
            <span className="rs-keepgoing-meta">{series.watched} of {series.total} watched</span>
            <span className="rs-keepgoing-bar" aria-hidden="true">{Array.from({ length: Math.min(series.total, 24) }, (_, i) => <i key={i} className={i < series.watched ? 'is-done' : ''} />)}</span>
          </div>
        </article>;
      })}
    </div>
  </section>;
}

// Studio names for the "Filter by studio" dropdown (paged in 200 at a time).
function useStudioNames(enabled = true) {
  const [studios, setStudios] = useState([]);
  useEffect(() => {
    if (!enabled) return undefined;
    let active = true;
    (async () => {
      const names = [];
      let offset = 0;
      while (active) {
        const result = await getStudios({ limit: 200, offset });
        const page = result.items || [];
        names.push(...page.map(item => item.name).filter(Boolean));
        if (page.length < 200) break;
        offset += page.length;
      }
      if (active) setStudios([...new Set(names)].sort((a, b) => a.localeCompare(b)));
    })().catch(() => {});
    return () => { active = false; };
  }, [enabled]);
  return studios;
}

// Studio / rating / sort filters shared by the Library and the performer / studio / category / series pages.
function TitleFilters({ studios, studio, onStudio, sort, onSort, rating, onRating, watched, onWatched, kind, onKind, seriesOrder = false, performerOrder = false }) {
  const studioOptions = useMemo(() => [{ value: 'all', label: 'All studios' }, ...(studios || []).map(name => ({ value: name, label: name }))], [studios]);
  const ratingOptions = [{ value: '0', label: 'Any rating' }, ...[5, 4, 3, 2, 1].map(n => ({ value: String(n), label: `${n}+ stars` }))];
  const sortOptions = [
    ...(seriesOrder ? [{ value: 'series', label: 'Series order' }] : []),
    ...(performerOrder ? [{ value: 'watched', label: 'Most played' }, { value: 'rated', label: 'Highest rated' }] : []),
    { value: 'recent', label: 'Newest first' }, { value: 'oldest', label: 'Oldest first' }, { value: 'title', label: 'Title A–Z' },
  ];
  return <div className="rs-filters">
 {onKind && <FilterSelect label="Type" value={kind} options={[{ value: 'all', label: 'All videos' }, { value: 'videos', label: 'Videos' }, { value: 'reels', label: 'Reels' }]} onChange={onKind} />}
    {studios && <FilterSelect label="Studio" value={studio} options={studioOptions} onChange={onStudio} searchable searchPlaceholder="Find a studio…" />}
 {onWatched && <FilterSelect label="Watched" value={watched} options={WATCHED_FILTERS} onChange={onWatched} />}
    {onRating && <FilterSelect label="Rating" value={String(rating)} options={ratingOptions} onChange={onRating} />}
    <FilterSelect label="Sort" value={sort} options={sortOptions} onChange={onSort} />
  </div>;
}

// Play / Shuffle for whatever list is on screen: fills the queue from the same filters and opens the first title.
function PlayButtons({ filters, ordered = false, shuffleLabel = 'Shuffle' }) {
  const { toast } = useToast();
  const navigate = useNavigate();
  const [busy, setBusy] = useState('');
  async function run(shuffle) {
    setBusy(shuffle ? 'shuffle' : 'play');
    try {
      const items = shuffle ? await fetchRandomTitles(filters, 100) : ((await getMediaItems({ ...filters, limit: 200 })).items || []);
      if (!playAll(items, { navigate })) toast({ title: 'Nothing to play here yet' });
    } catch (err) { toast({ title: 'Could not start playback', description: err.message, tone: 'danger' }); }
    finally { setBusy(''); }
  }
  return <div className="rs-play-buttons">
    {ordered && <button type="button" className="rs-button rs-button-primary" disabled={!!busy} onClick={() => run(false)}><Play size={16} fill="currentColor" /> Play all</button>}
    <button type="button" className="rs-button rs-button-outline" disabled={!!busy} onClick={() => run(true)}><Shuffle size={16} /> {shuffleLabel}</button>
  </div>;
}

function mediaQuery(filters) {
  return ({ limit, offset }) => getMediaItems({ ...filters, limit, offset });
}

function SearchPerformers({ query, favoritesOnly = false, full = false, onMore }) {
  const [items, setItems] = useState([]);
  const [total, setTotal] = useState(0);
  const [expanded, setExpanded] = useState(false);
  useEffect(() => {
    setExpanded(false);
    if (!query && !favoritesOnly) { setItems([]); setTotal(0); return undefined; }
    let active = true;
    getPerformers({ q: query, limit: 40, ...(favoritesOnly ? { favorite: 'true' } : {}) }).then(result => { if (active) { setItems(result.items || []); setTotal(result.total ?? (result.items || []).length); } }).catch(() => { if (active) setItems([]); });
    return () => { active = false; };
  }, [query, favoritesOnly]);
  if (!items.length) return null;
  return <section className="rs-search-performers">
    <div className="rs-section-head"><div><p className="rs-eyebrow">Performers</p><h2>{total} {favoritesOnly && !query ? (total === 1 ? 'favorite' : 'favorites') : (total === 1 ? 'match' : 'matches')}</h2></div>{!full && items.length > 6 && <button type="button" className="rs-search-more" onClick={onMore || (() => setExpanded(v => !v))}>{onMore ? 'View all' : expanded ? 'Less' : 'More'} <ArrowRight size={16} /></button>}</div>
    <div className={`rs-search-performer-grid${expanded || full ? ' is-expanded' : ''}`}>{items.map((item, index) => <PerformerCard key={item.id} item={item} index={index} />)}</div>
  </section>;
}

const DURATION_FILTERS = [
  { value: 'any', label: 'Any length' },
  { value: 'u15', label: 'Under 15 min', params: { maxDuration: 900 } },
  { value: 'u30', label: 'Under 30 min', params: { maxDuration: 1800 } },
  { value: '30-60', label: '30–60 min', params: { minDuration: 1800, maxDuration: 3600 } },
  { value: 'o60', label: 'Over 1 hour', params: { minDuration: 3600 } },
];
const QUALITY_FILTERS = [{ value: 'any', label: 'Any quality' }, { value: '4k', label: '4K' }, { value: '1080p', label: '1080p' }, { value: '720p', label: '720p' }, { value: 'sd', label: 'SD' }];
const WATCHED_FILTERS = [{ value: 'any', label: 'Any' }, { value: 'unwatched', label: 'Not watched' }, { value: 'inProgress', label: 'In progress' }, { value: 'watched', label: 'Watched' }];
const ADDED_FILTERS = [{ value: 'any', label: 'Any time' }, { value: '1', label: 'Today' }, { value: '7', label: 'This week' }, { value: '30', label: 'This month' }, { value: '90', label: 'Last 3 months' }];
const SORT_OPTIONS = [
  { value: 'recent', label: 'Recently added' }, { value: 'oldest', label: 'Oldest first' }, { value: 'title', label: 'Title A–Z' },
  { value: 'watched', label: 'Most played' }, { value: 'rated', label: 'Highest rated' },
];

// People and categories for the Library's filter pills (top people by titles; type to search the rest).
function usePeopleOptions(enabled) {
  const [query, setQuery] = useState('');
  const debounced = useDebounce(query, 200);
  const [items, setItems] = useState([]);
  useEffect(() => {
    if (!enabled) return undefined;
    let live = true;
    getPerformers({ limit: 60, ...(debounced.trim() ? { q: debounced.trim() } : {}) }).then(result => { if (live) setItems(result.items || []); }).catch(() => {});
    return () => { live = false; };
  }, [enabled, debounced]);
  return [items, setQuery];
}

function useCategoryOptions(enabled) {
  const [items, setItems] = useState([]);
  useEffect(() => {
    if (!enabled) return undefined;
    let live = true;
    getCategories().then(result => { if (live) setItems((result.items || []).filter(c => c.mediaCount > 0).sort((a, b) => b.mediaCount - a.mediaCount)); }).catch(() => {});
    return () => { live = false; };
  }, [enabled]);
  return items;
}

const SCOPES = [['all', 'Everything'], ['videos', 'Videos'], ['reels', 'Reels'], ['unwatched', 'Not watched'], ['inProgress', 'In progress']];

export function Catalog({ type = 'library' }) {
  const { terms } = useLibrary();
  const [params, setParams] = useSearchParams();
  const query = params.get('q') || '';
  const debouncedQuery = useDebounce(query, 250);
  const [studios, setStudios] = usePersistedState(`catalog:${type}:studios`, []);
  const [sort, setSort] = usePersistedState(`catalog:${type}:sort`, 'recent');
  const [rating, setRating] = usePersistedState(`catalog:${type}:rating`, '0');
  const [mediaKind, setMediaKind] = usePersistedState('catalog:library:media-kind', 'all');
  const [person, setPerson] = usePersistedState(`catalog:${type}:person`, '');
  const [personName, setPersonName] = usePersistedState(`catalog:${type}:person-name`, '');
  const [category, setCategory] = usePersistedState(`catalog:${type}:category`, '');
  const [duration, setDuration] = usePersistedState(`catalog:${type}:duration`, 'any');
  const [quality, setQuality] = usePersistedState(`catalog:${type}:quality`, 'any');
  const [watched, setWatched] = usePersistedState(`catalog:${type}:watched`, 'any');
  const [added, setAdded] = usePersistedState(`catalog:${type}:added`, 'any');
  // Links such as Home's "Added this week" arrive with ?added=7.
  useEffect(() => {
    const fromLink = params.get('added');
    if (!fromLink) return;
    setAdded(ADDED_FILTERS.some(option => option.value === fromLink) ? fromLink : 'any');
    const next = new URLSearchParams(params); next.delete('added'); setParams(next, { replace: true });
  }, [params]); // eslint-disable-line react-hooks/exhaustive-deps
  const studioNames = useStudioNames();
  const browsing = type !== 'search';
  const [people, searchPeople] = usePeopleOptions(browsing);
  const categories = useCategoryOptions(browsing);
  const tab = type === 'favorites' || type === 'search' ? (['performers', 'videos'].includes(params.get('tab')) ? params.get('tab') : 'overview') : 'videos';
  const setTab = value => { const next = new URLSearchParams(params); if (value === 'overview') next.delete('tab'); else next.set('tab', value); setParams(next); };
  const showPerformers = (type === 'search' || type === 'favorites') && tab !== 'videos';
  const showVideos = tab !== 'performers';
  const [cardStyle] = useCardStyle();
  const cards = cardMetrics(cardStyle);
  const minColumn = useColumnCount(cards.wideColumn, cards.narrowColumn, '(max-width: 640px)');
  const kindFilter = type === 'library' && mediaKind !== 'all' ? { reels: String(mediaKind === 'reels') } : {};
  const filters = useMemo(() => ({
    ...(type === 'favorites' ? { favorite: 'true' } : {}),
    ...(type === 'watchlater' ? { watchLater: 'true' } : {}),
    ...(type === 'movies' ? { kind: 'movie' } : {}),
    ...kindFilter,
    ...(debouncedQuery.trim() ? { q: debouncedQuery.trim() } : {}),
    ...(studios.length === 1 ? { studio: studios[0] } : studios.length > 1 ? { studios: studios.join(',') } : {}),
    ...(rating !== '0' ? { minRating: String(ratingFromStars(Number(rating))) } : {}),
    ...(person ? { performerId: person } : {}),
    ...(category ? { categoryId: category } : {}),
    ...(DURATION_FILTERS.find(option => option.value === duration)?.params || {}),
    ...(quality !== 'any' ? { quality } : {}),
    ...(watched !== 'any' ? { watched } : {}),
    ...(added !== 'any' ? { addedWithin: added } : {}),
    ...(sort !== 'recent' ? { sort } : {}),
  }), [type, debouncedQuery, studios, rating, sort, mediaKind, person, category, duration, quality, watched, added]); // eslint-disable-line react-hooks/exhaustive-deps
  const { items, total, loading, loadingMore, error, hasMore, loadMore, refresh } = useInfiniteList(mediaQuery(filters), JSON.stringify(filters));
  const label = type === 'search' ? 'Search' : type === 'favorites' ? 'Favorites' : type === 'watchlater' ? 'Watch later' : type === 'movies' ? terms.movies : terms.library;
  useScrollRestoration(showVideos && !!items.length, [items.length]);

  const scope = mediaKind !== 'all' ? mediaKind : (watched === 'unwatched' || watched === 'inProgress' ? watched : 'all');
  const pickScope = value => {
    if (value === 'videos' || value === 'reels') { setMediaKind(value); if (watched === 'unwatched' || watched === 'inProgress') setWatched('any'); }
    else if (value === 'all') { setMediaKind('all'); setWatched('any'); }
    else { setMediaKind('all'); setWatched(value); }
  };
  const chips = [
    ...studios.map(name => ({ key: `s:${name}`, label: `${terms.studio}: ${name}`, clear: () => setStudios(studios.filter(s => s !== name)) })),
    person && { key: 'p', label: `${terms.person}: ${personName || 'chosen'}`, clear: () => { setPerson(''); setPersonName(''); } },
    category && { key: 'c', label: `${terms.category}: ${categories.find(c => String(c.id) === String(category))?.name || 'chosen'}`, clear: () => setCategory('') },
    rating !== '0' && { key: 'r', label: `Rating: ${rating}+ ★`, clear: () => setRating('0') },
    duration !== 'any' && { key: 'd', label: DURATION_FILTERS.find(o => o.value === duration)?.label, clear: () => setDuration('any') },
    quality !== 'any' && { key: 'q', label: `Quality: ${QUALITY_FILTERS.find(o => o.value === quality)?.label}`, clear: () => setQuality('any') },
    watched !== 'any' && scope === 'all' && { key: 'w', label: WATCHED_FILTERS.find(o => o.value === watched)?.label, clear: () => setWatched('any') },
    added !== 'any' && { key: 'a', label: `Added ${ADDED_FILTERS.find(o => o.value === added)?.label.toLowerCase()}`, clear: () => setAdded('any') },
  ].filter(Boolean);
  const narrowed = chips.length > 0 || !!query || (type === 'library' && mediaKind !== 'all') || watched !== 'any';
  const clearAll = () => { setStudios([]); setPerson(''); setPersonName(''); setCategory(''); setRating('0'); setDuration('any'); setQuality('any'); setWatched('any'); setAdded('any'); if (type === 'library') setMediaKind('all'); };
  const blurb = type === 'favorites' ? 'Titles you saved to come back to.' : type === 'watchlater' ? 'Titles you set aside to watch when you have time. Finished ones drop off on their own.' : type === 'search' ? 'Search titles, people and studios.' : type === 'movies' ? 'Every movie in this library.' : 'Every title in your library. Your filters stay put when you come back.';
  const countText = loading && !items.length ? '…' : `${total.toLocaleString()} ${total === 1 ? 'title' : 'titles'}`;

  const inScopes = type === 'library' || type === 'movies';
  const filterControls = <>
      <FilterPanel
        activeCount={chips.length}
        onClear={clearAll}
        sections={[
          { id: 'studio', label: terms.studio, kind: 'multi', values: studios, onChange: setStudios, hidden: !browsing, options: studioNames.map(name => ({ value: name, label: name })) },
          { id: 'person', label: terms.person, kind: 'single', value: person, hidden: !browsing, onSearch: searchPeople,
            onChange: value => { setPerson(value); setPersonName(people.find(p => String(p.id) === value)?.name || ''); },
            options: [{ value: '', label: 'Anyone' }, ...(person && !people.some(p => String(p.id) === String(person)) ? [{ value: person, label: personName || 'Chosen' }] : []), ...people.map(p => ({ value: String(p.id), label: p.name, count: p.mediaCount }))] },
          { id: 'category', label: terms.category, kind: 'single', value: category, onChange: setCategory, hidden: !browsing, options: [{ value: '', label: 'Any' }, ...categories.map(c => ({ value: String(c.id), label: c.name, count: c.mediaCount }))] },
          { id: 'rating', label: 'Rating', kind: 'chips', value: String(rating), onChange: setRating, options: [{ value: '0', label: 'Any' }, ...[5, 4, 3, 2, 1].map(n => ({ value: String(n), label: `${n}+ ★` }))] },
          { id: 'length', label: 'Length', kind: 'chips', value: duration, onChange: setDuration, options: DURATION_FILTERS },
          { id: 'quality', label: 'Quality', kind: 'chips', value: quality, onChange: setQuality, options: QUALITY_FILTERS },
          { id: 'watched', label: 'Watched', kind: 'chips', value: watched, onChange: setWatched, hidden: scope !== 'all', options: WATCHED_FILTERS },
          { id: 'added', label: 'Added', kind: 'chips', value: added, onChange: setAdded, options: ADDED_FILTERS },
        ]}
      />
      <FilterSelect label="Sort" value={sort} options={SORT_OPTIONS} onChange={setSort} />
  </>;

  return <div className={`rs-page rs-catalog rs-catalog-${type}`}>
    <header className="rs-page-head">
      <div>
        <p className="rs-eyebrow">{type === 'favorites' || type === 'watchlater' ? 'Saved' : type === 'search' ? 'Search' : 'Library'}</p>
        <h1 className="rs-page-title">{label}<span className="rs-count">{countText}</span></h1>
        <p className="rs-page-sub">{blurb}</p>
      </div>
      {showVideos && total > 0 && <PlayButtons filters={filters} ordered={type === 'watchlater'} />}
    </header>
    {type === 'favorites' && <Tabs className="rs-favorites-tabs" value={tab} onValueChange={setTab}><TabsList><TabsTrigger value="overview">Overview</TabsTrigger><TabsTrigger value="performers">{terms.people}</TabsTrigger><TabsTrigger value="videos">{terms.titles}</TabsTrigger></TabsList></Tabs>}
    {(type === 'library' || type === 'movies') && <div className="rs-scopes" role="group" aria-label="Show">
      {SCOPES.filter(([id]) => type === 'library' || (id !== 'videos' && id !== 'reels')).map(([id, text]) => <button key={id} type="button" className={`rs-scope${scope === id ? ' is-active' : ''}`} aria-pressed={scope === id} onClick={() => pickScope(id)}>{text}</button>)}
      <span className="rs-scopes-sep" aria-hidden="true" />
      <Link className="rs-scope" to="/favorites">Favorites</Link>
      <Link className="rs-scope" to="/watch-later">Watch later</Link>
      {showVideos && <><span className="rs-filterbar-gap" />{filterControls}</>}
    </div>}
    {showVideos && !inScopes && <div className="rs-filterbar">
      <FilterPanel
        activeCount={chips.length}
        onClear={clearAll}
        sections={[
          { id: 'studio', label: terms.studio, kind: 'multi', values: studios, onChange: setStudios, hidden: !browsing, options: studioNames.map(name => ({ value: name, label: name })) },
          { id: 'person', label: terms.person, kind: 'single', value: person, hidden: !browsing, onSearch: searchPeople,
            onChange: value => { setPerson(value); setPersonName(people.find(p => String(p.id) === value)?.name || ''); },
            options: [{ value: '', label: 'Anyone' }, ...(person && !people.some(p => String(p.id) === String(person)) ? [{ value: person, label: personName || 'Chosen' }] : []), ...people.map(p => ({ value: String(p.id), label: p.name, count: p.mediaCount }))] },
          { id: 'category', label: terms.category, kind: 'single', value: category, onChange: setCategory, hidden: !browsing, options: [{ value: '', label: 'Any' }, ...categories.map(c => ({ value: String(c.id), label: c.name, count: c.mediaCount }))] },
          { id: 'rating', label: 'Rating', kind: 'chips', value: String(rating), onChange: setRating, options: [{ value: '0', label: 'Any' }, ...[5, 4, 3, 2, 1].map(n => ({ value: String(n), label: `${n}+ ★` }))] },
          { id: 'length', label: 'Length', kind: 'chips', value: duration, onChange: setDuration, options: DURATION_FILTERS },
          { id: 'quality', label: 'Quality', kind: 'chips', value: quality, onChange: setQuality, options: QUALITY_FILTERS },
          { id: 'watched', label: 'Watched', kind: 'chips', value: watched, onChange: setWatched, hidden: scope !== 'all', options: WATCHED_FILTERS },
          { id: 'added', label: 'Added', kind: 'chips', value: added, onChange: setAdded, options: ADDED_FILTERS },
        ]}
      />
      <span className="rs-filterbar-gap" />
      <FilterSelect label="Sort" value={sort} options={SORT_OPTIONS} onChange={setSort} />
    </div>}
    {showVideos && !!chips.length && <div className="rs-chips-bar">
      {chips.map(chip => <FilterChip key={chip.key} onRemove={chip.clear} label={`Remove ${chip.label}`}>{chip.label}</FilterChip>)}
      <button type="button" className="rs-clear-all" onClick={clearAll}>Clear all</button>
    </div>}
    {showPerformers && <SearchPerformers query={debouncedQuery.trim()} favoritesOnly={type === 'favorites'} full={tab === 'performers'} onMore={type === 'favorites' && tab === 'overview' ? () => setTab('performers') : undefined} />}
    {showVideos && <>
    <Status loading={loading && !items.length} error={error} empty={!loading && !error && !items.length && !narrowed} retry={refresh} title={type === 'favorites' ? 'No favorites yet' : type === 'watchlater' ? 'Nothing saved for later' : 'Your library is waiting'} message={type === 'favorites' ? 'Save titles from their detail pages and they will appear here.' : type === 'watchlater' ? 'Use Watch later on a title and it will wait for you here.' : 'Add media from Admin to begin your collection.'} />
    {!loading && !error && !items.length && narrowed && <div className="rs-status is-empty" role="status"><span className="rs-status-icon"><SearchX size={26} strokeWidth={1.75} /></span><h2>No matches</h2><p>Nothing fits these filters. Remove one or clear them all.</p>{chips.length > 0 && <div className="rs-status-actions"><button type="button" className="rs-button rs-button-outline" onClick={clearAll}>Clear all filters</button></div>}</div>}
    {!!items.length && <VirtualWindow items={items} minColumn={minColumn} gapX={18} gapY={26} aspect={cards.aspect} extra={72} hasMore={hasMore} onNearEnd={loadMore} renderItem={item => <MediaCard item={item} />} />}
    {loadingMore && <p className="rs-virtual-status">Loading more titles…</p>}
    {!loadingMore && !hasMore && items.length > 24 && <p className="rs-virtual-status">That’s all {total.toLocaleString()}.</p>}
    </>}
  </div>;
}

// The page names in the words of the library's mode (Performers vs Cast & crew, ...).
function dimensionTerms(type, terms) {
  return {
    performers: { title: terms.people, singular: terms.person },
    studios: { title: terms.studios, singular: terms.studio },
    series: { title: terms.seriesPlural, singular: terms.series },
    categories: { title: terms.categories, singular: terms.category },
  }[type];
}

function cleanParams(params) {
  return Object.fromEntries(Object.entries(params).filter(([, value]) => value !== '' && value != null));
}

const dimensions = {
  performers: { title: 'Performers', singular: 'Performer', fetchPage: params => getPerformers(cleanParams(params)), filterKey: 'performerId' },
  studios: { title: 'Studios', singular: 'Studio', fetchPage: params => getStudios(cleanParams(params)), filterKey: 'studioId' },
  series: { title: 'Series', singular: 'Series', fetchPage: params => getSeries(cleanParams(params)), filterKey: 'seriesId' },
  categories: {
    title: 'Categories',
    singular: 'Category',
    fetchPage: async ({ q, showEmpty }) => {
      const result = await getCategories();
      const query = (q || '').trim().toLowerCase();
      const items = (result.items || [])
        .filter(item => (showEmpty || item.mediaCount > 0) && (!query || item.name.toLowerCase().includes(query)))
        .sort((a, b) => showEmpty ? a.name.localeCompare(b.name) : b.mediaCount - a.mediaCount || a.name.localeCompare(b.name));
      return { items, total: items.length };
    },
    filterKey: 'categoryId',
  },
};

function DirectoryMark({ item, index, type, portrait = false }) {
  const [failed, setFailed] = useState(false);
  const imageId = type === 'studios' ? item.logoImageId : item.profileImageId;
  const src = type === 'studios' ? studioImageUrl(item.id, item.logoImageId) : performerImageUrl(item.id, item.profileImageId);
  if (imageId && !failed) {
    return <img className={portrait ? 'rs-portrait-photo' : 'rs-directory-photo'} src={src} alt="" loading="lazy" onError={() => setFailed(true)} />;
  }
  const mono = monogram(item.name);
  if (portrait) return <span className="rs-portrait-fallback rs-mono" style={mono.style} aria-hidden="true">{mono.initials}</span>;
  return <span className="rs-directory-mark rs-mono" style={mono.style} aria-hidden="true">{mono.initials}</span>;
}

function countLabel(count) {
  return `${count} ${count === 1 ? 'title' : 'titles'}`;
}

function PerformerCard({ item, index, note, below = false }) {
  const count = item.mediaCount || 0;
  if (below) {
    // The directory's card as designed: the portrait, then name and counts underneath.
    return <Link to={`/performers/${item.id}`} className={`rs-portrait-card is-below ${toneClass(item.id)}`} aria-label={`${item.name}, ${countLabel(count)}`}>
      <span className="rs-portrait-frame">
        <DirectoryMark item={item} index={index} type="performers" portrait />
        {item.favorite && <span className="rs-portrait-fav" aria-label="Favorite"><Heart size={14} fill="currentColor" aria-hidden="true" /></span>}
      </span>
      <span className="rs-portrait-caption"><strong>{item.name}</strong><small>{countLabel(count)}{item.unwatchedCount != null ? ` · ${item.unwatchedCount} not watched` : ''}</small></span>
    </Link>;
  }
  return <Link to={`/performers/${item.id}`} className={`rs-portrait-card ${toneClass(item.id)}`} aria-label={`${item.name}, ${countLabel(count)}`}>
    <span className="rs-portrait-frame">
      <DirectoryMark item={item} index={index} type="performers" portrait />
      <span className="rs-portrait-shade" />
      {item.favorite && <Heart className="rs-portrait-heart" size={16} fill="currentColor" aria-label="Favorite" />}
      <span className="rs-portrait-copy"><strong>{item.name}</strong><small>{note || countLabel(count)}</small></span>
    </span>
  </Link>;
}

function StudioCard({ item, index }) {
  const count = item.mediaCount || 0;
  return <Link to={`/studios/${item.id}`} className={`rs-studio-card ${toneClass(item.id)}`} aria-label={`${item.name}, ${countLabel(count)}`}>
    <span className="rs-studio-card-logo"><DirectoryMark item={item} index={index} type="studios" /></span>
    <span className="rs-studio-card-copy"><strong>{item.name}</strong><small>{countLabel(count)}</small></span>
  </Link>;
}

function SeriesCard({ item, base = 'series', view = 'grid' }) {
  const count = item.mediaCount || 0;
  const seasons = item.seasonCount > 0 ? `${item.seasonCount} ${item.seasonCount === 1 ? 'season' : 'seasons'}` : null;
  const meta = [seasons || countLabel(count), item.releaseDate?.slice(0, 4), item.studioName].filter(Boolean).join(' · ');
  const label = `${item.name}, ${countLabel(count)}`;
  if (view === 'list') {
    return <Link className={`rs-series-item rs-series-item-list ${toneClass(item.id)}`} to={`/${base}/${item.id}`} aria-label={label}>
      <span className="rs-series-item-art"><SeriesCover series={item} /></span>
      <span className="rs-series-item-copy">
        <strong>{item.name}</strong>
        <small>{meta}</small>
        {item.description ? <p>{item.description}</p> : <p className="is-empty">No description yet.</p>}
      </span>
      <span className="rs-series-item-count"><b>{count}</b>{count === 1 ? 'title' : 'titles'}</span>
      <ArrowUpRight className="rs-series-item-go" size={18} />
    </Link>;
  }
  return <Link className={`rs-series-item ${toneClass(item.id)}`} to={`/${base}/${item.id}`} aria-label={label}>
    <span className="rs-series-item-art">
      <SeriesCover series={item} />
      <span className="rs-series-item-badge"><ListVideo size={12} /> {count}</span>
      {item.description && <span className="rs-series-item-peek">{item.description}</span>}
    </span>
    <span className="rs-series-item-copy"><strong>{item.name}</strong><small>{meta}</small></span>
  </Link>;
}

function NameTile({ item, type }) {
  const count = item.mediaCount || 0;
  return <Link to={`/${type}/${item.id}`} className={`rs-name-tile ${toneClass(`${type}:${item.id}`)}`} aria-label={`${item.name}, ${countLabel(count)}`}>
    <strong>{item.name}</strong>
    <small>{countLabel(count)}</small>
    <ArrowUpRight size={18} />
  </Link>;
}

// Entertainment libraries split two directories: people into cast and directors, and series into
// TV shows and collections of movies.
const BROWSE_VARIANTS = {
  directors: { params: { role: 'director' }, title: terms => terms.directors, singular: terms => terms.director, blurb: 'Who directed the titles in this library.' },
  shows: { params: { kind: 'show' }, title: terms => terms.shows, singular: terms => terms.show, blurb: 'Every show, with its seasons and episodes.' },
  collections: { params: { kind: 'collection' }, title: terms => terms.seriesPlural, singular: terms => terms.series, blurb: 'Movies that belong together.' },
};

// "You watch most this month": the people behind the titles played most in the last 30 days.
function MostWatchedStrip() {
  const [rows, setRows] = useState([]);
  useEffect(() => {
    let live = true;
    getWatchActivity(30).then(data => { if (live) setRows((data.performers || []).slice(0, 5)); }).catch(() => {});
    return () => { live = false; };
  }, []);
  if (!rows.length) return null;
  const length = seconds => { const m = Math.round((seconds || 0) / 60); return m >= 60 ? `${Math.floor(m / 60)}h ${String(m % 60).padStart(2, '0')}m` : `${m}m`; };
  return <section className="rs-most-watched" aria-label="You watch most this month">
    <div className="rs-most-watched-head"><h2>You watch most this month</h2><Link to="/activity">Activity</Link></div>
    <ol>{rows.map((row, i) => {
      const mono = monogram(row.name);
      return <li key={row.id}><Link to={`/performers/${row.id}`}>
        <span className="rs-most-rank">{i + 1}</span>
        <span className={`rs-most-avatar${row.profileImageId ? '' : ' rs-mono'}`} style={row.profileImageId ? undefined : mono.style}>{row.profileImageId ? <img src={performerImageUrl(row.id, row.profileImageId)} alt="" loading="lazy" /> : mono.initials}</span>
        <span className="rs-most-copy"><strong>{row.name}</strong><small>{row.plays} {row.plays === 1 ? 'play' : 'plays'} · {length(row.watchSeconds)}</small></span>
      </Link></li>;
    })}</ol>
  </section>;
}

export function Browse({ type, variant }) {
  const { terms, isAdmin, mode } = useLibrary();
  const extra = mode === 'entertainment' ? BROWSE_VARIANTS[variant] : null;
  const config = { ...dimensions[type], ...dimensionTerms(type, terms), ...(extra ? { title: extra.title(terms), singular: extra.singular(terms) } : {}) };
  const [query, setQuery] = useState('');
  const [peopleTab, setPeopleTab] = usePersistedState(`browse:${type}:tab`, 'all'); // all | favorites | recent | photo
  const onlyFavorites = peopleTab === 'favorites';
  const [tabCounts, setTabCounts] = useState({});
  useEffect(() => {
    if (type !== 'performers' || variant === 'directors') return undefined;
    let live = true;
    Promise.all([getPerformers({ favorite: 'true', limit: 1 }), getPerformers({ needsPhoto: 'true', limit: 1 })])
      .then(([fav, photo]) => { if (live) setTabCounts({ favorites: fav.total, photo: photo.total }); }).catch(() => {});
    return () => { live = false; };
  }, [type, variant]);
  const [letter, setLetter] = usePersistedState(`browse:${type}:letter`, '');
  const [showEmpty, setShowEmpty] = usePersistedState(`browse:${type}:showEmpty`, false);
  const [sort, setSort] = usePersistedState(`browse:${type}:sort`, 'count');
  const [view, setView] = usePersistedState(`browse:${type}:view`, 'grid');
  const [creating, setCreating] = useState(false);
  const navigate = useNavigate();
  const debounced = useDebounce(query, 250);
  const favoriteFilter = type === 'performers' && onlyFavorites;
  const { items, total, loading, loadingMore, error, hasMore, loadMore, refresh } = useInfiniteList(
    ({ limit, offset }) => config.fetchPage({ limit, offset, q: debounced.trim(), favorite: favoriteFilter ? 'true' : '', ...(type === 'performers' && peopleTab === 'recent' ? { recent: 'true' } : {}), ...(type === 'performers' && peopleTab === 'photo' ? { needsPhoto: 'true' } : {}), letter: type === 'performers' ? letter : '', showEmpty, ...(type === 'performers' && sort !== 'count' ? { sort } : {}), ...(extra?.params || {}) }),
    `${type}:${variant || ''}:${debounced.trim()}:${favoriteFilter}:${peopleTab}:${letter}:${showEmpty}:${sort}`,
  );
  useScrollRestoration(!!items.length, [items.length]);

  const blurbs = {
    performers: 'The people in your library.',
    studios: 'Who made each title.',
    series: 'Titles that belong together.',
    categories: 'Topics used in your collection.',
  };
  return <div className={`rs-page rs-catalog-${type}`}>
    <header className="rs-page-header rs-directory-header"><div className="rs-directory-headrow"><div className="rs-directory-title"><p className="rs-eyebrow">Discover</p><h1>{config.title}<span className="rs-count">{loading && !items.length ? '…' : total.toLocaleString()}</span></h1>{type !== 'performers' && <p>{extra?.blurb || blurbs[type]}</p>}</div><div className="rs-directory-toolbar"><label className="rs-search"><Search size={18} /><input aria-label={`Search ${config.title.toLowerCase()}`} value={query} onChange={e => setQuery(e.target.value)} placeholder={`Search ${config.title.toLowerCase()}`} /></label>{type === 'series' && <div className="rs-view-toggle" role="group" aria-label="View"><button type="button" aria-pressed={view !== 'list'} title="Grid view" onClick={() => setView('grid')}><LayoutGrid size={16} /></button><button type="button" aria-pressed={view === 'list'} title="List view" onClick={() => setView('list')}><Rows3 size={16} /></button></div>}{type === 'categories' ? <button type="button" className="rs-button rs-button-outline" aria-pressed={showEmpty} onClick={() => setShowEmpty(value => !value)}>{showEmpty ? 'Show used only' : 'Show all categories'}</button> : type !== 'performers' && <span>A — Z</span>}{type === 'performers' && <FilterSelect label="Sort" value={sort} onChange={setSort} options={[{ value: 'count', label: 'Most titles' }, { value: 'name', label: 'Name A–Z' }, { value: 'name_desc', label: 'Name Z–A' }]} />}{isAdmin && type === 'performers' && variant !== 'directors' && <button type="button" className="rs-button rs-button-primary rs-toolbar-new" onClick={() => setCreating(true)}><Plus size={16} /> New {terms.person.toLowerCase()}</button>}{isAdmin && type === 'series' && variant !== 'shows' && <button type="button" className="rs-button rs-button-outline rs-toolbar-new" onClick={() => setCreating(true)}><Plus size={16} /> New {terms.series.toLowerCase()}</button>}</div></div></header>
    {type === 'performers' && variant !== 'directors' && !query && peopleTab === 'all' && !letter && <MostWatchedStrip />}
    {type === 'performers' && <div className="rs-performer-subbar">
      <Tabs value={peopleTab} onValueChange={setPeopleTab}><TabsList>
        <TabsTrigger value="all">All</TabsTrigger>
        <TabsTrigger value="favorites"><Heart size={14} /> Favorites{tabCounts.favorites != null ? ` · ${tabCounts.favorites.toLocaleString()}` : ''}</TabsTrigger>
        {variant !== 'directors' && <TabsTrigger value="recent">Recently watched</TabsTrigger>}
        {variant !== 'directors' && <TabsTrigger value="photo">Needs a photo{tabCounts.photo != null ? ` · ${tabCounts.photo.toLocaleString()}` : ''}</TabsTrigger>}
      </TabsList></Tabs>
      <nav className="rs-az" aria-label="Filter by first letter">{['#', ...'ABCDEFGHIJKLMNOPQRSTUVWXYZ'].map(l => <button key={l} type="button" className={letter === l ? 'is-active' : ''} aria-pressed={letter === l} onClick={() => setLetter(current => current === l ? '' : l)}>{l}</button>)}</nav>
    </div>}
    {type === 'performers' && <NewPerformerDialog open={creating} onOpenChange={setCreating} onCreated={id => navigate(`/performers/${id}`)} />}
    {type === 'series' && <NewSeriesDialog open={creating} onOpenChange={setCreating} onCreated={id => navigate(`/series/${id}`)} />}
    <Status loading={loading && !items.length} error={error} empty={!loading && !error && !items.length && !query && !favoriteFilter && !letter} retry={refresh} title={`No ${config.title.toLowerCase()} yet`} message={type === 'series' ? 'Group the scenes of a movie or the episodes of a show: choose New series, or open a video’s Settings and use its Series tab.' : type === 'categories' ? 'No titles have categories yet.' : 'They appear here as your library is scanned.'} href={type === 'series' || type === 'categories' ? '/library' : undefined} action="Browse titles" />
    {!loading && !error && (!!query || favoriteFilter || !!letter) && !items.length && <Status empty title={favoriteFilter && !query ? 'No favorite performers yet' : 'No matches'} message={favoriteFilter && !query ? 'Open a performer and tap Favorite.' : 'Try a different name.'} />}
    {!!items.length && type === 'performers' && <VirtualWindow className="rs-portrait-grid" items={items} minColumn={168} gapX={16} gapY={24} aspect={3 / 4} extra={56} hasMore={hasMore} onNearEnd={loadMore} renderItem={(item, index) => <PerformerCard item={item} index={index} below />} />}
    {!!items.length && type === 'studios' && <VirtualWindow className="rs-studio-cards" items={items} minColumn={220} gapX={16} gapY={16} rowHeight={138} hasMore={hasMore} onNearEnd={loadMore} renderItem={(item, index) => <StudioCard item={item} index={index} />} />}
    {!!items.length && type === 'series' && (view === 'list'
      ? <VirtualWindow className="rs-series-list-view" items={items} columns={1} gapX={0} gapY={10} rowHeight={140} hasMore={hasMore} onNearEnd={loadMore} renderItem={item => <SeriesCard item={item} view="list" base={variant === 'shows' && extra ? 'shows' : 'series'} />} />
      : <VirtualWindow className="rs-series-grid-view" items={items} minColumn={190} gapX={18} gapY={22} aspect={2 / 3} extra={70} hasMore={hasMore} onNearEnd={loadMore} renderItem={item => <SeriesCard item={item} base={variant === 'shows' && extra ? 'shows' : 'series'} />} />)}
    {!!items.length && type !== 'performers' && type !== 'studios' && type !== 'series' && <VirtualWindow className="rs-name-tiles" items={items} minColumn={240} gapX={14} gapY={14} rowHeight={88} hasMore={hasMore} onNearEnd={loadMore} renderItem={item => <NameTile item={item} type={type} />} />}
    {loadingMore && <p className="rs-virtual-status">Loading more…</p>}
  </div>;
}

function hashString(value) {
  let h = 2166136261;
  for (const ch of String(value)) { h ^= ch.codePointAt(0); h = Math.imul(h, 16777619); }
  return h >>> 0;
}

// Deterministic per-performer palette: same performer always gets the same backdrop.
function performerTheme(entity) {
  const h = hashString(`${entity.id}:${entity.name}`);
  const hue = h % 360;
  const spread = 40 + ((h >>> 9) % 80);
  const pos = n => `${(h >>> n) % 100}%`;
  const angle = (h >>> 5) % 360;
  return {
    '--pf-a': `hsl(${hue} 70% 42%)`,
    '--pf-b': `hsl(${(hue + spread) % 360} 65% 38%)`,
    '--pf-c': `hsl(${(hue + 180 + spread / 2) % 360} 60% 30%)`,
    '--pf-x1': pos(2), '--pf-y1': pos(7), '--pf-x2': pos(13), '--pf-y2': pos(17), '--pf-angle': `${angle}deg`,
    '--pf-pattern': ['pf-dots', 'pf-lines', 'pf-grid', 'pf-rings'][(h >>> 21) % 4],
  };
}

// Long bios are clamped to a few lines with a Read more / Show less toggle so the hero stays compact.
// Bio fact labels already shown as hero stats, so the grid doesn't repeat them.
function statLabels(meta = {}) {
  return ['height', 'birthplace', 'nationality'].filter(key => meta?.[key])
    .concat(meta?.birthdate ? ['born', 'birthdate', 'date of birth', 'birthday', 'age'] : []);
}

// "Label: value" lines in a bio become a compact fact grid; the rest stays prose.
function ExpandableBio({ text, skip }) {
  const [open, setOpen] = useState(false);
  const { facts, prose } = splitBio(text, skip);
  const long = prose.length > 260;
  useEffect(() => { setOpen(false); }, [text]);
  return <div className="rs-bio-wrap">
    {!!facts.length && <dl className="rs-bio-facts">
      {facts.map(fact => <div key={fact.label}><dt>{fact.label}</dt><dd>{fact.value}</dd></div>)}
    </dl>}
    {prose && <p className={`rs-performer-bio rs-entity-description${long && !open ? ' is-clamped' : ''}`}>{prose}</p>}
    {long && <button type="button" className="rs-bio-toggle" aria-expanded={open} onClick={() => setOpen(value => !value)}>{open ? 'Show less' : 'Read more'}</button>}
  </div>;
}

function ageFrom(value) {
  const born = new Date(value);
  if (Number.isNaN(born.getTime())) return null;
  const now = new Date();
  let age = now.getFullYear() - born.getFullYear();
  if (now < new Date(now.getFullYear(), born.getMonth(), born.getDate())) age -= 1;
  return age >= 0 && age < 120 ? age : null;
}

function SeriesHero({ entity, name, countLabel, onSettings }) {
  const facts = [entity?.releaseDate?.slice(0, 4), entity?.studioName].filter(Boolean);
  return <header className="rs-page-header rs-entity-header rs-studio-hero rs-series-hero">
    {entity && <span className="rs-series-hero-poster"><SeriesCover series={entity} /></span>}
    <div><p className="rs-eyebrow">SERIES / YOUR ARCHIVE</p><h1>{name || 'Loading…'}</h1><p>{[countLabel, ...facts].join('  ·  ')}</p>
      {entity?.description && <p className="rs-entity-description">{entity.description}</p>}
      {entity && !entity.mediaCount && <p>Open Settings to add videos, or match the series with a provider and pair its scenes.</p>}
    </div>
    {entity && onSettings && <div className="rs-studio-hero-actions"><button type="button" className="rs-button rs-button-outline" onClick={onSettings}><Settings2 size={17} /> Settings</button></div>}
  </header>;
}

function NewSeriesDialog({ open, onOpenChange, onCreated }) {
  const { toast } = useToast();
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  useEffect(() => { if (open) setName(''); }, [open]);
  async function submit(event) {
    event.preventDefault();
    setBusy(true);
    try {
      const created = await createSeries({ name: name.trim() });
      onOpenChange(false);
      onCreated?.(created.id);
      toast({ title: 'Series created', description: 'Open Settings to add videos and match it with a provider.', tone: 'success' });
    } catch (err) {
      toast({ title: 'Could not create series', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }
  return <Dialog open={open} onOpenChange={onOpenChange}><DialogContent><DialogHeader><DialogTitle>New series</DialogTitle><DialogDescription>For example a movie whose scenes are separate videos, or a show.</DialogDescription></DialogHeader>
    <form className="rs-add-playlist-form" onSubmit={submit}><Input aria-label="Series name" placeholder="Series name" value={name} onChange={e => setName(e.target.value)} autoFocus /><Button type="submit" disabled={busy || !name.trim()}>Create</Button></form>
  </DialogContent></Dialog>;
}

// "Start here" on a person's page: the title you play most and the newest one you have not watched.
function StartHere({ titles, name }) {
  const location = useLocation();
  const picks = useMemo(() => {
    const played = [...titles].filter(t => t.watchCount > 0).sort((a, b) => b.watchCount - a.watchCount)[0];
    const fresh = [...titles].filter(t => !t.watchCount && !t.positionSeconds && (!played || t.id !== played.id)).sort((a, b) => String(b.dateAdded || '').localeCompare(String(a.dateAdded || '')))[0];
    return [
      played && { item: played, why: 'Your most played', meta: [played.studioName, formatRuntime(played.durationSeconds), `watched ${played.watchCount}×`].filter(Boolean).join(' · ') },
      fresh && { item: fresh, why: 'Newest · not watched', meta: [fresh.studioName, formatRuntime(fresh.durationSeconds), fresh.dateAdded ? `added ${new Date(fresh.dateAdded).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}` : ''].filter(Boolean).join(' · ') },
    ].filter(Boolean);
  }, [titles]);
  if (!picks.length) return null;
  return <section className="rs-start-here" aria-label="Start here">
    <div className="rs-section-head"><div><p className="rs-eyebrow">Featuring {name}</p><h2>Start here</h2></div></div>
    <div className="rs-start-cards">
      {picks.map(({ item, why, meta }) => <article key={item.id} className={`rs-start-card ${toneClass(item.id)}`}>
        <Artwork item={item} preferBackdrop />
        <div className="rs-start-copy">
          <span className="rs-start-why">{why}</span>
          <strong>{displayTitle(item)}</strong>
          {meta && <small>{meta}</small>}
          <div className="rs-start-actions">
            <Link className="rs-button rs-button-primary" to={`/library/${item.id}`} state={{ autoplay: true, from: location.pathname }}>Play</Link>
            <Link className="rs-button rs-button-secondary" to={`/library/${item.id}`}>Details</Link>
          </div>
        </div>
      </article>)}
    </div>
  </section>;
}

export function BrowseDetail({ type }) {
  const { toast } = useToast();
  const { name: id } = useParams();
  const [cardStyle] = useCardStyle();
  const cards = cardMetrics(cardStyle);
  const minColumn = useColumnCount(cards.wideColumn, cards.narrowColumn, '(max-width: 640px)');
  const [similar, setSimilar] = useState([]);
  useEffect(() => {
    setSimilar([]);
    if (type !== 'performers') return undefined;
    let live = true;
    getRelatedPerformers(id, 12).then(result => { if (live) setSimilar(result.items || []); }).catch(() => {});
    return () => { live = false; };
  }, [type, id]);
  const { terms, isAdmin } = useLibrary();
  const config = { ...dimensions[type], ...dimensionTerms(type, terms) };
  const [name, setName] = useState('');
  const [entity, setEntity] = useState(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const navigate = useNavigate();
  const [studio, setStudio] = useState('all');
  const defaultSort = type === 'series' ? 'series' : type === 'performers' ? 'watched' : 'recent';
  const [sort, setSort] = useState(defaultSort);
  const [watchedFilter, setWatchedFilter] = useState('any');
  const [kindFilter, setKindFilter] = useState('all');
  useEffect(() => { setStudio('all'); setSort(defaultSort); setWatchedFilter('any'); setKindFilter('all'); }, [type, id, defaultSort]);
  const studios = useStudioNames(type !== 'studios');
  const filters = useMemo(() => ({ [config.filterKey]: id }), [config.filterKey, id]);
  const listFilters = useMemo(() => ({ ...filters, ...(studio !== 'all' ? { studio } : {}), ...(sort !== 'recent' || type === 'series' ? { sort } : {}), ...(type === 'performers' && watchedFilter !== 'any' ? { watched: watchedFilter } : {}), ...(type === 'performers' && kindFilter === 'reels' ? { reels: 'true' } : {}), ...(type === 'performers' && kindFilter === 'videos' ? { reels: 'false' } : {}) }), [filters, studio, sort, type, watchedFilter, kindFilter]);
  const { items, total, loading, loadingMore, error, hasMore, loadMore, refresh } = useInfiniteList(mediaQuery(listFilters), `${type}:${id}:${studio}:${sort}:${watchedFilter}:${kindFilter}`);
  useScrollRestoration(!!items.length, [items.length]);
  const filtered = studio !== 'all' || sort !== defaultSort || watchedFilter !== 'any' || kindFilter !== 'all';
  const isStudio = type === 'studios' || type === 'performers';
  const studioFilters = useMemo(() => ({ ...filters, limit: 5 }), [filters]);
  const { items: featuredArt } = useMediaList(isStudio ? { ...filters, limit: HERO_SLIDE_COUNT, sort: 'random', hasArtwork: 'true' } : null);
  const { items: featuredRest } = useMediaList(isStudio ? { ...filters, limit: HERO_SLIDE_COUNT, sort: 'random' } : null);
  // Titles with artwork first, topped up with others so the slideshow has enough slides.
  const featuredItems = useMemo(() => {
    const seen = new Set(featuredArt.map(item => item.id));
    return [...featuredArt, ...featuredRest.filter(item => !seen.has(item.id))].slice(0, Math.max(featuredArt.length, Math.min(6, HERO_SLIDE_COUNT)));
  }, [featuredArt, featuredRest]);
  const { items: watchedItems } = useMediaList(type === 'studios' ? { ...studioFilters, sort: 'watched' } : null);
  const personPage = type === 'performers';
  const { items: personTitles } = useMediaList(personPage ? { ...filters, limit: 500 } : null);
  const { total: watchedTotal } = useMediaList(personPage ? { ...filters, watched: 'watched', limit: 1 } : null);
  const { total: favoriteTotal } = useMediaList(personPage ? { ...filters, favorite: 'true', limit: 1 } : null);
  const insights = useMemo(() => {
    const years = new Map();
    const studiosSeen = new Map();
    let rated = 0; let ratingSum = 0; let plays = 0;
    personTitles.forEach(title => {
      const year = Number((title.releaseDate || '').slice(0, 4)) || title.productionYear || 0;
      if (year > 1900) years.set(year, (years.get(year) || 0) + 1);
      if (title.studioName) studiosSeen.set(title.studioName, { id: title.studioId, name: title.studioName, count: (studiosSeen.get(title.studioName)?.count || 0) + 1 });
      if (title.myRating > 0) { rated += 1; ratingSum += title.myRating; }
      plays += title.watchCount || 0;
    });
    const span = [...years.keys()].sort((a, b) => a - b);
    const byYear = span.length ? Array.from({ length: Math.min(span[span.length - 1] - span[0] + 1, 30) }, (_, i) => { const year = span[span.length - 1] - Math.min(span[span.length - 1] - span[0], 29) + i; return { year, count: years.get(year) || 0 }; }) : [];
    return { byYear, studios: [...studiosSeen.values()].sort((a, b) => b.count - a.count).slice(0, 6), avgRating: rated ? ratingSum / rated / 2 : 0, plays };
  }, [personTitles]);
  const { items: favoriteItems } = useMediaList(type === 'studios' ? { ...studioFilters, favorite: 'true' } : null);
  const loadEntity = React.useCallback(() => {
    const request = type === 'performers' ? getPerformer(id) : type === 'studios' ? getStudio(id) : type === 'categories' ? getCategories().then(result => (result.items || []).find(item => String(item.id) === String(id))) : getSeriesItem(id);
    return request.then(result => { setName(result?.name || ''); setEntity(result || null); return result; });
  }, [type, id]);
  useEffect(() => { let active = true; loadEntity().catch(() => { if (active) setEntity(null); }); return () => { active = false; }; }, [loadEntity]);
  const collectionLabel = `${loading && !items.length ? '…' : total} ${total === 1 ? 'title' : 'titles'} in your collection`;
  const portrait = type === 'performers';
  async function toggleFavorite() {
    try {
      const result = await setPerformerFavorite(entity.id, !entity.favorite);
      setEntity(current => ({ ...current, favorite: result.favorite }));
      toast({ title: result.favorite ? 'Added to favorites' : 'Removed from favorites', tone: 'success' });
    } catch (err) {
      toast({ title: 'Could not update favorite', description: err.message, tone: 'danger' });
    }
  }
  async function toggleNotify() {
    try {
      const result = await setPerformerNotify(entity.id, !entity.notify);
      setEntity(current => ({ ...current, notify: result.notify }));
      toast({ title: result.notify ? 'We’ll tell you about new titles' : 'Notifications off', description: result.notify ? `New titles with ${entity.name} appear under the bell.` : undefined, tone: 'success' });
    } catch (err) {
      toast({ title: 'Could not update notifications', description: err.message, tone: 'danger' });
    }
  }
  return <div className={`rs-page rs-entity-page${type === 'studios' ? ' rs-studio-page' : ''} ${toneClass(`${type}:${id}`)}`}>
    {type === 'performers' ? <header className={`rs-performer-hero ${entity ? performerTheme(entity)['--pf-pattern'] : ''}`} style={entity ? performerTheme(entity) : undefined}>
      {entity?.profileImageId && <span className="rs-performer-hero-bg" style={{ backgroundImage: `url(${performerImageUrl(entity.id, entity.profileImageId)})` }} aria-hidden="true" />}
      <span className="rs-entity-portrait">{entity ? <DirectoryMark item={entity} index={0} type="performers" portrait /> : null}</span>
      <div className="rs-performer-hero-copy">
        <p className="rs-eyebrow">{terms.person} · In your library</p>
        <h1>{name || 'Loading…'}</h1>
        {!!entity?.aliases?.length && <p className="rs-performer-aka">aka {entity.aliases.slice(0, 4).join(', ')}</p>}
        <ul className="rs-hero-stats">
          <li><strong>{loading && !items.length ? '…' : total}</strong><span>{total === 1 ? 'title' : 'titles'}</span></li>
          {entity?.metadata?.birthdate && <li><strong>{ageFrom(entity.metadata.birthdate) ?? entity.metadata.birthdate}</strong><span>{ageFrom(entity.metadata.birthdate) != null ? 'years old' : 'born'}</span></li>}
          {entity?.metadata?.nationality && <li><strong>{entity.metadata.nationality}</strong><span>nationality</span></li>}
          {entity?.metadata?.height && <li><strong>{entity.metadata.height}</strong><span>height</span></li>}
          {entity?.metadata?.birthplace && <li title={entity.metadata.birthplace}><strong>{entity.metadata.birthplace}</strong><span>birthplace</span></li>}
        </ul>
        {entity && (entity.notes || entity.metadata?.bio) && <ExpandableBio text={entity.notes || entity.metadata.bio} skip={statLabels(entity.metadata)} />}
      </div>
      {entity && <div className="rs-performer-hero-actions">
        {total > 0 && <PlayButtons filters={listFilters} shuffleLabel={`Shuffle ${total.toLocaleString()} ${total === 1 ? 'title' : 'titles'}`} />}
        <button type="button" className={`rs-button rs-button-outline ${entity.favorite ? 'is-favorite' : ''}`} aria-pressed={!!entity.favorite} onClick={toggleFavorite}><Heart size={17} fill={entity.favorite ? 'currentColor' : 'none'} /> {entity.favorite ? 'Favorited' : 'Favorite'}</button>
        <button type="button" className={`rs-button rs-button-outline ${entity.notify ? 'is-favorite' : ''}`} aria-pressed={!!entity.notify} onClick={toggleNotify}>{entity.notify ? <BellRing size={17} /> : <Bell size={17} />} {entity.notify ? 'Notifying you about new titles' : 'Notify me about new titles'}</button>
        {isAdmin && <button type="button" className="rs-button rs-button-outline" onClick={() => setSettingsOpen(true)}><Settings2 size={17} /> Edit</button>}
      </div>}
      {entity && total > 0 && <dl className="rs-person-stats" aria-label="Your watching">
        <div><dt>Watched</dt><dd>{watchedTotal.toLocaleString()} of {total.toLocaleString()}</dd></div>
        <div><dt>Your plays</dt><dd>{insights.plays.toLocaleString()}</dd></div>
        <div><dt>Favorites</dt><dd>{favoriteTotal.toLocaleString()}</dd></div>
        <div><dt>Your avg rating</dt><dd>{insights.avgRating ? `★ ${insights.avgRating.toFixed(1)}` : '—'}</dd></div>
      </dl>}
    </header> : type === 'series' ? <SeriesHero entity={entity} name={name} countLabel={collectionLabel} onSettings={isAdmin ? () => setSettingsOpen(true) : null} /> : <header className={`rs-page-header rs-entity-header${portrait ? ' rs-entity-hero' : ''}${type === 'studios' ? ' rs-studio-hero' : ''}`}>
      {type === 'studios' && entity && <span className={`rs-studio-hero-logo${entity.logoImageId ? '' : ' is-mono'}`}><DirectoryMark item={entity} index={0} type="studios" /></span>}
      {portrait && entity && <span className="rs-entity-portrait"><DirectoryMark item={entity} index={0} type="performers" portrait /></span>}
      <div><p className="rs-eyebrow">{config.singular.toUpperCase()} / YOUR ARCHIVE</p><h1>{name || 'Loading…'}</h1><p>{collectionLabel}</p>
        {type === 'performers' && entity && (entity.notes || entity.metadata?.bio) && <p className="rs-performer-bio rs-entity-description">{entity.notes || entity.metadata.bio}</p>}
        {type === 'performers' && entity && <div className="rs-detail-actions">
          <button type="button" className={`rs-button rs-button-outline ${entity.favorite ? 'is-favorite' : ''}`} aria-pressed={!!entity.favorite} onClick={toggleFavorite}><Heart size={17} fill={entity.favorite ? 'currentColor' : 'none'} /> {entity.favorite ? 'Favorited' : 'Favorite'}</button>
          {isAdmin && <button type="button" className="rs-button rs-button-outline" onClick={() => setSettingsOpen(true)}><Settings2 size={17} /> Settings</button>}
        </div>}
        {type === 'studios' && entity?.website && <a className="rs-studio-website" href={/^https?:\/\//i.test(entity.website) ? entity.website : `https://${entity.website}`} target="_blank" rel="noopener noreferrer"><Globe size={15} /> {entity.website.replace(/^https?:\/\//i, '').replace(/\/$/, '')}</a>}
        {type === 'studios' && entity?.description && <p className="rs-entity-description">{entity.description}</p>}
      </div>
      {isAdmin && type === 'studios' && entity && <div className="rs-studio-hero-actions"><button type="button" className="rs-button rs-button-outline" onClick={() => setSettingsOpen(true)}><Settings2 size={17} /> Settings</button></div>}
    </header>}
    {type === 'performers' && entity && <PerformerSettings performerId={entity.id} open={settingsOpen} onOpenChange={setSettingsOpen} onChanged={loadEntity} onSwitch={id => { setSettingsOpen(false); navigate(`/performers/${id}`); }} onDeleted={() => navigate('/performers')} />}
    {type === 'studios' && entity && <StudioSettings studioId={entity.id} open={settingsOpen} onOpenChange={setSettingsOpen} onChanged={() => { loadEntity(); refresh(); }} onSwitch={id => { setSettingsOpen(false); navigate(`/studios/${id}`); }} onDeleted={() => navigate('/studios')} />}
    {type === 'series' && entity && <SeriesSettings seriesId={entity.id} open={settingsOpen} onOpenChange={setSettingsOpen} onChanged={() => { loadEntity(); refresh(); }} onDeleted={() => navigate('/series')} />}
    <Status loading={loading && !items.length} error={error} empty={!loading && !error && !items.length} retry={refresh} />
    {type === 'performers' && <StartHere titles={personTitles} name={name || terms.person.toLowerCase()} />}
    {type === 'studios' && !!featuredItems.length && <HeroCarousel items={featuredItems} eyebrow={type === 'performers' ? `Featuring ${name || terms.person.toLowerCase()}` : `From ${name || terms.studio.toLowerCase()}`} exclude={type === 'performers' ? [name, ...(entity?.aliases || [])] : []} />}
    {type === 'studios' && <Shelf title="Most watched" eyebrow="Popular" items={watchedItems.filter(item => item.watchCount > 0)} />}
    {type === 'studios' && <Shelf title="Favorites" eyebrow="Saved" items={favoriteItems} />}
    {type === 'performers' && insights.byYear.length > 1 && <div className="rs-person-insights rs-person-years">
      {insights.byYear.length > 1 && <section className="rs-panel rs-years" aria-labelledby="rs-years-title">
        <h2 id="rs-years-title">Titles by year</h2>
        <div className="rs-years-chart" role="img" aria-label={`Titles by year: ${insights.byYear.filter(y => y.count).map(y => `${y.year}: ${y.count}`).join(', ')}`}>
          {insights.byYear.map(({ year, count }) => { const max = Math.max(...insights.byYear.map(y => y.count)); return <span key={year} className="rs-years-col"><em>{count || ''}</em><i style={{ height: `${count ? Math.max(6, count / max * 100) : 2}%` }} className={count === max ? 'is-top' : ''} /><small>{String(year).slice(-2) === '00' || insights.byYear.length <= 12 ? year : `’${String(year).slice(-2)}`}</small></span>; })}
        </div>
      </section>}
    </div>}
    {(!!items.length || filtered) && <div className="rs-section-head rs-videos-head"><div><p className="rs-eyebrow">Library</p><h2>All {total.toLocaleString()} {total === 1 ? terms.title.toLowerCase() : terms.titles.toLowerCase()}</h2></div><div className="rs-title-controls">{total > 0 && <PlayButtons filters={listFilters} ordered={type === 'series'} />}<TitleFilters studios={type === 'studios' ? null : studios} studio={studio} onStudio={setStudio} sort={sort} onSort={setSort} seriesOrder={type === 'series'} performerOrder={type === 'performers'} {...(type === 'performers' ? { watched: watchedFilter, onWatched: setWatchedFilter, kind: kindFilter, onKind: setKindFilter } : {})} /></div></div>}
    {!loading && !error && !items.length && filtered && <Status empty icon={SearchX} title="No matches found" message="Try another studio or sort order." />}
    {!!items.length && <VirtualWindow items={items} minColumn={minColumn} gapX={16} gapY={24} aspect={cards.aspect} extra={96} hasMore={hasMore} onNearEnd={loadMore} renderItem={(item, index) => <MediaCard item={item} index={index} exclude={type === 'performers' ? [name, ...(entity?.aliases || [])] : []} kicker={type === 'series' && item.seriesPosition != null ? [`No. ${item.seriesPosition}`, item.studioName].filter(Boolean).join(' · ') : undefined} showPlayCount={type === 'performers'} mostPlayed={type === 'performers' && sort === 'watched' && index === 0 && item.watchCount > 0} />} />}
    {loadingMore && <p className="rs-virtual-status">Loading more titles…</p>}
    {type === 'performers' && (!!similar.length || insights.studios.length > 0) && <div className="rs-person-insights rs-person-side">
      {!!similar.length && <section className="rs-panel rs-costars" aria-labelledby="rs-costars-title">
        <h2 id="rs-costars-title">Often appears with</h2>
        {similar.slice(0, 4).map(person => <Link key={person.id} to={`/performers/${person.id}`} className="rs-costar">
          <span className="rs-costar-mark"><DirectoryMark item={person} index={0} type="performers" portrait /></span>
          <strong>{person.name}</strong>
          <small>{person.sharedTitles ? `${person.sharedTitles} ${person.sharedTitles === 1 ? 'title' : 'titles'} together` : countLabel(person.mediaCount || 0)}</small>
        </Link>)}
      </section>}
      {!!insights.studios.length && <section className="rs-panel rs-person-studios" aria-labelledby="rs-person-studios-title">
        <h2 id="rs-person-studios-title">{terms.studios}</h2>
        {insights.studios.map(studio => <Link key={studio.name} to={studio.id ? `/studios/${studio.id}` : `/search?q=${encodeURIComponent(studio.name)}`} className="rs-bar-row"><span><strong>{studio.name}</strong><small>{plural(studio.count, 'title')}</small></span><i><b style={{ width: `${studio.count / insights.studios[0].count * 100}%` }} /></i></Link>)}
      </section>}
    </div>}
  </div>;
}

function CreditImage({ src, studio }) {
  const [failed, setFailed] = useState(false);
  if (!src || failed) return null;
  return <img className={studio ? 'is-studio' : ''} src={src} alt="" onError={() => setFailed(true)} />;
}

function DetailFacts({ item }) {
  const { terms, mode } = useLibrary();
  const cast = item.cast?.length ? item.cast : (item.performers || []).map(name => ({ name }));
  const crew = item.crew || [];
  const personImage = person => person.profileImageId ? <CreditImage src={performerImageUrl(person.id, person.profileImageId)} /> : <span className="rs-cast-fallback rs-mono" style={monogram(person.name).style} aria-hidden="true">{monogram(person.name).initials}</span>;
  const isEpisode = item.kind === 'episode';
  const search = value => `/search?q=${encodeURIComponent(value)}`;
  const chips = [
    ...(item.categories || []).map(name => ({ key: `c:${name}`, label: name, to: search(name), kind: 'Category' })),
    ...(item.tags || []).map(name => ({ key: `t:${name}`, label: name, to: search(name), kind: 'Tag' })),
  ];
  if (!cast.length && !chips.length && !item.studioName && !item.seriesName && !item.releaseDate) {
    return <aside className="rs-detail-facts rs-detail-facts-empty"><p className="rs-eyebrow">THE DETAILS</p><p>No details yet. Match this title to add its performers, studio and tags.</p></aside>;
  }
  return <aside className="rs-detail-facts">
    <p className="rs-eyebrow">THE DETAILS</p>
    <h2>About this title</h2>
    {!!cast.length && <section className="rs-cast-block" aria-label={terms.cast}>
      <h3 className="rs-facts-label">{terms.cast}</h3>
      <div className="rs-cast rs-cast-grid">{cast.map(person => <Link key={person.id || person.name} className="rs-credit rs-cast-person" to={person.id ? `/performers/${person.id}` : search(person.name)}>{personImage(person)}<span>{person.name}{!person.character && person.mediaCount > 0 && <small className="rs-cast-character">{countLabel(person.mediaCount)} in library</small>}{person.character && <small className="rs-cast-character">{person.character}</small>}</span></Link>)}</div>
    </section>}
    {['director', 'writer'].map(job => { const people = crew.filter(c => c.job === job); return people.length > 0 && <section className="rs-cast-block" key={job} aria-label={job === 'director' ? terms.directors : 'Writers'}>
      <h3 className="rs-facts-label">{job === 'director' ? (people.length === 1 ? terms.director : terms.directors) : (people.length === 1 ? 'Writer' : 'Writers')}</h3>
      <div className="rs-cast rs-cast-grid">{people.map(person => <Link key={`${job}:${person.id}`} className="rs-credit rs-cast-person" to={`/performers/${person.id}`}>{personImage(person)}<span>{person.name}</span></Link>)}</div>
    </section>; })}
    {item.studioName && <section className="rs-cast-block" aria-label={terms.studio}>
      <h3 className="rs-facts-label">{terms.studio}</h3>
      {(() => {
        const mark = item.studioLogoImageId ? <CreditImage studio src={studioImageUrl(item.studioId, item.studioLogoImageId)} /> : null;
        const body = <>{mark || <span className="rs-studio-tile rs-mono" style={monogram(item.studioName).style} aria-hidden="true">{monogram(item.studioName).initials}</span>}<span><strong>{item.studioName}</strong><small>More from this {terms.studio.toLowerCase()}</small></span></>;
        return item.studioId ? <Link className="rs-studio-credit" to={`/studios/${item.studioId}`}>{body}</Link> : <div className="rs-studio-credit">{body}</div>;
      })()}
    </section>}
    <dl className="rs-facts-grid">
      {item.seriesName && <div><dt>{isEpisode ? terms.show : terms.series}</dt><dd>{item.seriesId ? <Link className="rs-credit" to={`/${isEpisode && mode === 'entertainment' ? 'shows' : 'series'}/${item.seriesId}`}><span>{item.seriesName}</span></Link> : item.seriesName}</dd></div>}
      {isEpisode && item.seasonNumber != null && <div><dt>{terms.episode}</dt><dd>{`${terms.season} ${item.seasonNumber}${item.episodeNumber != null ? ` · ${terms.episode} ${item.episodeNumber}` : ''}`}</dd></div>}
      {item.releaseDate && <div><dt>Released</dt><dd>{item.releaseDate.slice(0, 10)}</dd></div>}
    </dl>
    {!!chips.length && <section aria-label="Categories and tags">
      <h3 className="rs-facts-label">Categories &amp; tags</h3>
      <div className="rs-chip-row">{chips.map(chip => <Link key={chip.key} className={`rs-chip ${toneClass(chip.label)}`} to={chip.to} title={`Find more: ${chip.label}`}>{chip.label}</Link>)}</div>
    </section>}
  </aside>;
}

export function MediaDetail({ isAdmin = false }) {
  const { toast } = useToast();
  const { id } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const queue = useQueue();
  const [markers, setMarkers] = useState([]);
  const [playlistOpen, setPlaylistOpen] = useState(false);
  // Set when the person came here from the queue or a "play" button: start playing once the video is ready.
  const wantsAutoplay = useRef(false);
  // 'backdrop' shows the key art with the title over it; 'player' swaps in the video once playback starts.
  const [stage, setStage] = useState('backdrop');
  // The video's shape as the browser decodes it (rotation applied); until it loads, the server's guess.
  const [videoShape, setVideoShape] = useState(null);
  useEffect(() => { setVideoShape(null); }, [id]);
  // Prefer provider backdrops; fall back to a landscape poster when needed.
  const [backdropArt, setBackdropArt] = useState('loading'); // 'loading' | 'wide' | 'tall' | 'none'
  const [failedBackdropId, setFailedBackdropId] = useState(null);
  const [item, setItem] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [playerError, setPlayerError] = useState(false);
  const [playbackRate, setPlaybackRate] = useState(1);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const playerRef = useRef(null);
  const frameRef = useRef(null);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const playerAvailable = Boolean(item?.files?.some(file => file.available));
  useEffect(() => {
    const video = playerRef.current;
    // iPhone fullscreens only the bare <video>; give it Safari's own controls (with a touch scrubber) there.
    const sync = () => { setIsFullscreen(isPlayerFullscreen(frameRef.current, video)); if (video) video.controls = Boolean(video.webkitDisplayingFullscreen); };
    return onFullscreenChange(video, sync);
  }, [item?.id, playerAvailable]);
  // Audio and subtitle tracks, started from the person's language defaults; the player waits for them
  // (briefly) so it opens the right audio track the first time.
  const trackSel = useMediaTracks(id, playerAvailable);
  const burnTrack = trackSel.subtitles.find(track => track.id === trackSel.subtitleId && track.burn);
  const { mode: playbackMode, audioForcesTranscode, skips, lastSkip, quality: transcodeQuality, setQuality: setTranscodeQuality, prepareSourceChange, resumeAt, resumePlayback, retry: retryPlayback, setTranscode, skipAhead } = useAdaptiveSource(playerRef, id, playerAvailable && trackSel.ready, () => setPlayerError(true), trackSel.audioSelection, burnTrack ? burnTrack.burnIndex : -1);
  // Shows subtitle text tracks only for the chosen one; the browser keeps the others off.
  useEffect(() => {
    const video = playerRef.current;
    if (!video?.textTracks) return undefined;
    const wanted = trackSel.subtitleId ? `rs-sub-${trackSel.subtitleId}` : null;
    const apply = () => Array.from(video.textTracks).forEach(track => { if (track.kind === 'subtitles') track.mode = wanted && track.id === wanted ? 'showing' : 'disabled'; });
    apply();
    video.textTracks.addEventListener?.('addtrack', apply);
    video.addEventListener('loadedmetadata', apply);
    return () => { video.textTracks.removeEventListener?.('addtrack', apply); video.removeEventListener('loadedmetadata', apply); };
  }, [trackSel.subtitleId, trackSel.subtitles, playerAvailable]);
  function chooseAudio(index) { prepareSourceChange(); trackSel.setAudioIndex(index); }
  // The episode that follows this one, for the Next button and the Up next countdown.
  const [nextEpisode, setNextEpisode] = useState(null);
  const [upNext, setUpNext] = useState(false);
  const seriesId = item?.seriesId;
  useEffect(() => {
    let live = true;
    setNextEpisode(null);
    setUpNext(false);
    if (seriesId) getNextInSeries(id).then(result => { if (live) setNextEpisode(result.item || null); }).catch(() => {});
    return () => { live = false; };
  }, [id, seriesId]);
  const load = React.useCallback(() => { setLoading(true); setError(''); setPlayerError(false); getMediaItem(id).then(setItem).catch(err => setError(err.message)).finally(() => setLoading(false)); }, [id]);
  useEffect(load, [load]);
  const [related, setRelated] = useState([]);
  const [studioMore, setStudioMore] = useState([]);
  const [personMore, setPersonMore] = useState([]);
  const studioId = item?.studioId;
  const leadPerson = (item?.cast || []).find(person => person.id);
  useEffect(() => { wantsAutoplay.current = Boolean(location.state?.autoplay); setStage(wantsAutoplay.current ? 'player' : 'backdrop'); }, [id, location.key]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { setBackdropArt('loading'); }, [id]);
  // Rendered synchronously so play() and fullscreen still run inside the click that asked for them.
  function showPlayer() { if (stage !== 'player') flushSync(() => setStage('player')); }
  useEffect(() => { syncPlaying(id); }, [id]);
  useEffect(() => {
    let live = true;
    setMarkers([]);
    getMarkers(id).then(r => live && setMarkers(r.items || [])).catch(() => {});
    return () => { live = false; };
  }, [id]);
  useEffect(() => {
    let live = true;
    setRelated([]);
    getRelatedMedia(id).then(r => live && setRelated(r.items || [])).catch(() => {});
    return () => { live = false; };
  }, [id]);
  useEffect(() => {
    let live = true;
    setStudioMore([]);
    if (studioId) getMediaItems({ studioId, limit: 13, sort: 'random' }).then(r => live && setStudioMore((r.items || []).filter(m => String(m.id) !== String(id)).slice(0, 12))).catch(() => {});
    return () => { live = false; };
  }, [id, studioId]);
  useEffect(() => {
    let live = true;
    setPersonMore([]);
    if (leadPerson?.id) getMediaItems({ performerId: leadPerson.id, limit: 13, sort: 'random' }).then(r => live && setPersonMore((r.items || []).filter(m => String(m.id) !== String(id)).slice(0, 12))).catch(() => {});
    return () => { live = false; };
  }, [id, leadPerson?.id]);
  async function toggleFavorite() { try { const updated = await updateMediaItem(id, { favorite: !item.favorite }); setItem(updated); toast({ title: updated.favorite ? 'Added to favorites' : 'Removed from favorites', tone: 'success' }); } catch (err) { toast({ title: 'Could not update favorite', description: err.message, tone: 'danger' }); } }
  async function setRating(stars) { try { const updated = await updateMediaItem(id, { rating: ratingFromStars(stars) }); setItem(updated); } catch (err) { toast({ title: 'Could not update rating', description: err.message, tone: 'danger' }); } }
  async function toggleWatchLater() {
    const next = !item.watchLater;
    try {
      if (next) await addWatchLater(id); else await removeWatchLater(id);
      setItem(current => ({ ...current, watchLater: next }));
      toast({ title: next ? 'Saved to Watch later' : 'Removed from Watch later', tone: 'success' });
    } catch (err) { toast({ title: 'Could not update Watch later', description: err.message, tone: 'danger' }); }
  }
  function addToQueue() {
    const entry = { id: item.id, title: item.title, studioName: item.studioName };
    if (!queue.items.length) { startQueue([entry]); toast({ title: 'Queue started', description: 'Open other titles and add them to line up what plays next.', tone: 'success' }); return; }
    toast(enqueue(entry) ? { title: 'Added to queue', tone: 'success' } : { title: 'Already in your queue' });
  }
  function jumpTo(seconds) {
    const video = playerRef.current;
    if (!video) return;
    showPlayer();
    video.currentTime = seconds;
    video.scrollIntoView({ behavior: 'smooth', block: 'center' });
    video.play().catch(() => {});
  }
  async function markCurrentMoment() {
    const seconds = Math.round((playerRef.current?.currentTime || 0) * 10) / 10;
    try {
      const marker = await createMarker(item.id, seconds, '');
      setMarkers(current => [...current, marker]);
      toast({ title: `Marker added at ${formatClock(seconds)}`, tone: 'success' });
    } catch (err) { toast({ title: 'Could not add marker', description: err.message, tone: 'danger' }); }
  }
  function fullscreen() { toggleFullscreen(frameRef.current, playerRef.current); }
  function togglePlayPause() {
    const video = playerRef.current;
    if (!video) return;
    if (video.paused) video.play().catch(() => {}); else video.pause();
  }
  function changeRate(rate) {
    const clamped = Math.max(0.25, Math.min(3, rate));
    setPlaybackRate(clamped);
    if (playerRef.current) playerRef.current.playbackRate = clamped;
  }
  const backTo = location.state?.from || '/library';
  function goTo(entry) { if (entry) navigate(`/library/${entry.id}`, { state: { autoplay: true, from: backTo } }); }
  async function markWatched() {
    try {
      await updatePlayback(id, 0, true);
      setItem(current => ({ ...current, positionSeconds: 0, watchCount: (current.watchCount || 0) + 1 }));
      toast({ title: 'Marked as watched', tone: 'success' });
    } catch (err) { toast({ title: 'Could not mark as watched', description: err.message, tone: 'danger' }); }
  }
  function jumpMarker(direction) {
    const video = playerRef.current;
    if (!video || !markers.length) return;
    const times = markers.map(marker => marker.seconds).sort((a, b) => a - b);
    const target = direction > 0 ? times.find(t => t > video.currentTime + 0.5) : [...times].reverse().find(t => t < video.currentTime - 2);
    if (target != null) video.currentTime = target;
  }
  function onFrameKeyDown(event) {
    if (event.target !== event.currentTarget) return; // buttons and the seek bar handle their own keys
    if (event.ctrlKey || event.metaKey || event.altKey) return;
    const video = playerRef.current;
    if (!video) return;
    if (/^[0-9]$/.test(event.key) && Number.isFinite(video.duration)) { video.currentTime = video.duration * Number(event.key) / 10; event.preventDefault(); return; }
    if (event.key === 'j') { skip(-10); event.preventDefault(); return; }
    if (event.key === 'l') { skip(10); event.preventDefault(); return; }
    if (event.key === '>' ) { changeRate(playbackRate + 0.25); event.preventDefault(); return; }
    if (event.key === '<') { changeRate(playbackRate - 0.25); event.preventDefault(); return; }
    if (event.key === 'N') { goTo(nextInQueue() || nextEpisode); event.preventDefault(); return; }
    if (event.key === 'P') { goTo(previousInQueue()); event.preventDefault(); return; }
    if (event.key === ']') { jumpMarker(1); event.preventDefault(); return; }
    if (event.key === '[') { jumpMarker(-1); event.preventDefault(); return; }
    if (event.key === 'i') {
      const intro = markers.find(marker => /intro|opening|recap/i.test(marker.title || ''));
      if (intro) { video.currentTime = intro.seconds; event.preventDefault(); return; }
    }
    if (event.key === 'c' && trackSel.subtitles.length) { trackSel.toggleSubtitles(); event.preventDefault(); return; }
    if (event.key === 'c') { const t = Array.from(video.textTracks || []).filter(x => x.kind === 'subtitles' || x.kind === 'captions'); if (t.length) { const on = t.some(x => x.mode === 'showing'); t.forEach((x, i) => { x.mode = !on && i === 0 ? 'showing' : 'disabled'; }); event.preventDefault(); } return; }
    if (event.key === ',' && video.paused) { video.currentTime = Math.max(0, video.currentTime - 1 / 30); event.preventDefault(); return; }
    if (event.key === '.' && video.paused) { video.currentTime += 1 / 30; event.preventDefault(); return; }
    if (event.key === ' ' || event.key === 'k') { togglePlayPause(); event.preventDefault(); }
    else if (event.key === 'f') { fullscreen(); event.preventDefault(); }
    else if (event.key === 'm') { video.muted = !video.muted; event.preventDefault(); }
    else if (event.key === 'ArrowLeft') { skip(-5); event.preventDefault(); }
    else if (event.key === 'ArrowRight') { skip(5); event.preventDefault(); }
  }
  function play() {
    const video = playerRef.current;
    if (!video) return;
    showPlayer();
    video.scrollIntoView({ behavior: 'smooth', block: 'center' });
    video.play().catch(() => {});
    if (window.matchMedia('(max-width: 720px)').matches && !isPlayerFullscreen(frameRef.current, video)) fullscreen();
  }
  function playFromStart() {
    const video = playerRef.current;
    if (!video) return;
    resumeAt.current = 0;
    showPlayer();
    video.currentTime = 0;
    lastSaved.current = 0;
    video.scrollIntoView({ behavior: 'smooth', block: 'center' });
    video.play().catch(() => {});
    if (window.matchMedia('(max-width: 720px)').matches && !isPlayerFullscreen(frameRef.current, video)) fullscreen();
  }
  function skip(seconds) {
    const video = playerRef.current;
    if (!video) return;
    video.currentTime = Math.max(0, Math.min(Number.isFinite(video.duration) ? video.duration : Infinity, video.currentTime + seconds));
  }

  const lastSaved = useRef(0);
  const session = useRef({ id: null, starting: null, watched: 0, lastTime: null });
  function ensureSession() {
    const cur = session.current;
    if (cur.id || cur.starting) return;
    cur.starting = startWatchSession(id).then(result => { cur.id = result.sessionId; }).catch(() => {}).finally(() => { cur.starting = null; });
  }
  function flushSession(completed = false, keepalive = false) {
    const video = playerRef.current;
    const cur = session.current;
    if (video) { cur.position = video.currentTime; cur.duration = Number.isFinite(video.duration) ? video.duration : 0; }
    if (!cur.id || cur.watched <= 0 && !completed) return;
    updateWatchSession(cur.id, {
      positionSeconds: cur.position || 0, watchedSeconds: cur.watched, durationSeconds: cur.duration || 0, completed,
    }, keepalive).catch(() => {});
    if (completed) session.current = { id: null, starting: null, watched: 0, lastTime: null };
  }
  function saveProgress(completed = false) {
    const video = playerRef.current;
    if (!video) return;
    const position = completed ? 0 : video.currentTime;
    lastSaved.current = video.currentTime;
    updatePlayback(id, position, completed).catch(() => {});
    flushSession(completed);
  }
  function onTimeUpdate() {
    const video = playerRef.current;
    if (!video) return;
    const cur = session.current;
    // Count only continuous playback: seeks and stalls produce large deltas.
    if (cur.lastTime != null) {
      const delta = video.currentTime - cur.lastTime;
      if (delta > 0 && delta < 2.5) cur.watched += delta;
    }
    cur.lastTime = video.currentTime;
    cur.position = video.currentTime;
    cur.duration = Number.isFinite(video.duration) ? video.duration : 0;
    if (Math.abs(video.currentTime - lastSaved.current) < 15) return;
    saveProgress(false);
  }
  useEffect(() => {
    const onHide = () => flushSession(false, true);
    window.addEventListener('pagehide', onHide);
    return () => { window.removeEventListener('pagehide', onHide); flushSession(false, true); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id]);
  function onLoadedMetadata() {
    const video = playerRef.current;
    if (video) video.playbackRate = playbackRate;
    if (video?.videoWidth && video.videoHeight) setVideoShape(video.videoHeight > video.videoWidth ? 'portrait' : 'landscape');
    const target = resumeAt.current ?? item.positionSeconds;
    if (video && target > 0 && target < video.duration - 5) {
      video.currentTime = target;
      lastSaved.current = target;
    }
  }
  // Started on canplay, not loadedmetadata: calling play() while the resume-seek is still pending gets aborted.
  function onCanPlay() {
    const video = playerRef.current;
    if (video && video.readyState >= HTMLMediaElement.HAVE_FUTURE_DATA && !video.seeking && (wantsAutoplay.current || resumePlayback.current)) video.play().catch(() => {});
  }
  function onEnded() {
    saveProgress(true);
    const next = nextInQueue();
    if (next && queue.autoplay) { navigate(`/library/${next.id}`, { state: { autoplay: true, from: backTo } }); return; }
    if (!next && nextEpisode) setUpNext(true);
  }

  if (loading || error || !item) return <div className="rs-page"><Status loading={loading} error={error} empty={!item} retry={load} /></div>;

  const available = item.files?.some(file => file.available);
  const resumable = available && item.positionSeconds > 0 && item.durationSeconds > item.positionSeconds;
  const title = displayTitle(item);
  const titleLengthClass = title.length > 90 ? 'is-extra-long-title' : title.length > 48 ? 'is-long-title' : '';
  const useProviderBackdrop = item.backdropImageId && failedBackdropId !== item.backdropImageId;
  const showBackdrop = stage === 'backdrop' && (backdropArt === 'loading' || backdropArt === 'wide');
  // Portrait videos (reels) get a tall player with the details beside it.
  const portrait = videoShape ? videoShape === 'portrait' : !!item.reelDetected;
  const file = bestFile(item);
  const quality = qualityLabel(file);
  const year = item.releaseDate?.slice(0, 4) || (item.productionYear ? String(item.productionYear) : '');
  const seriesLine = item.seriesName ? `${item.seriesName}${item.kind === 'episode' && item.seasonNumber != null ? ` · S${item.seasonNumber}${item.episodeNumber != null ? ` E${item.episodeNumber}` : ''}` : item.seriesPosition != null ? ` · Part ${item.seriesPosition}` : ''}` : '';
  const queueBeside = !item.seriesId && queue.items.length > 0;
  return <div className={`rs-detail rs-watch-page ${showBackdrop ? 'is-backdrop' : 'is-playing'}${portrait ? ' is-portrait' : ''}`}>
    <div className="rs-tv-backdrop" aria-hidden="true"><img key={`${item.id}-${useProviderBackdrop ? 'backdrop' : 'poster'}`} src={artworkUrl(item.id, useProviderBackdrop ? 'backdrop' : 'poster', useProviderBackdrop ? item.backdropImageId : item.posterImageId)} alt="" className={backdropArt === 'wide' || backdropArt === 'tall' ? 'is-loaded' : ''} onLoad={event => { const { naturalWidth: w, naturalHeight: h } = event.currentTarget; if (useProviderBackdrop && w / h < 1.3) { setFailedBackdropId(item.backdropImageId); setBackdropArt('loading'); } else setBackdropArt(w / h >= 1.3 ? 'wide' : 'tall'); }} onError={() => { if (useProviderBackdrop) { setFailedBackdropId(item.backdropImageId); setBackdropArt('loading'); } else setBackdropArt('none'); }} /></div>
    <div className="rs-watch-grid">
      <div className="rs-watch-main">
        <section className="rs-player-section" aria-label="Video player">
          {available ? <>
            <div className="rs-player-frame" ref={frameRef} tabIndex={0} onKeyDown={onFrameKeyDown} aria-label="Video player. Space plays or pauses, F toggles fullscreen, M mutes, arrow keys seek, J and L skip ten seconds, C toggles subtitles, N and P change title, less-than and greater-than change speed.">
            <video ref={playerRef} preload="metadata" playsInline x-webkit-airplay="allow" poster={artworkUrl(item.id, useProviderBackdrop ? 'backdrop' : 'poster', useProviderBackdrop ? item.backdropImageId : item.posterImageId)} onLoadedMetadata={onLoadedMetadata} onCanPlay={onCanPlay} onPlay={() => { wantsAutoplay.current = false; resumePlayback.current = false; setStage('player'); setPlayerError(false); ensureSession(); session.current.lastTime = playerRef.current?.currentTime ?? null; }} onSeeked={() => { session.current.lastTime = playerRef.current?.currentTime ?? null; onCanPlay(); }} onTimeUpdate={onTimeUpdate} onPause={() => saveProgress(false)} onEnded={onEnded}>{trackSel.subtitles.filter(track => !track.burn).map(track => <track key={`${id}-${track.id}`} id={`rs-sub-${track.id}`} kind="subtitles" src={subtitleUrl(id, track.id)} srcLang={track.language || undefined} label={track.label} />)}</video>
            {stage === 'backdrop' && <button type="button" className="rs-player-start" onClick={play} aria-label={resumable ? 'Resume' : 'Play'}><Play size={34} fill="currentColor" /></button>}
            {stage === 'backdrop' && resumable && <div className="rs-player-resume"><div><span>Resume from {formatClock(item.positionSeconds)}</span><span>{formatClock(item.durationSeconds)}</span></div><span className="rs-player-resume-bar"><i style={{ width: `${Math.min(100, item.positionSeconds / item.durationSeconds * 100)}%` }} />{markers.map(marker => <b key={marker.id} style={{ left: `${Math.min(100, marker.seconds / item.durationSeconds * 100)}%` }} />)}</span></div>}
            {upNext && nextEpisode && <UpNext item={nextEpisode} onPlay={() => goTo(nextEpisode)} onCancel={() => setUpNext(false)} />}
            {playerError && <div className="rs-player-error" role="alert">Playback stopped. <button type="button" onClick={() => { setPlayerError(false); retryPlayback(); }}>Retry from here</button>{playbackMode === 'direct' && <><button type="button" onClick={() => { setPlayerError(false); skipAhead(30); }}>Skip 30s ahead</button><button type="button" onClick={() => { setPlayerError(false); setTranscode(true); }}>Play transcoded</button></>}</div>}
            {skips > 0 && <span className="rs-player-notice" role="status">Jumped {lastSkip || 2}s past a spot that would not play ({skips} {skips === 1 ? 'time' : 'times'})</span>}
            <PlayerControls idle={stage === 'backdrop'} videoRef={playerRef} frameRef={frameRef} mediaId={item.id} markers={markers} enabled={available} onAddMarker={markCurrentMoment} onToggleFullscreen={fullscreen} isFullscreen={isFullscreen} onPrev={previousInQueue() ? () => goTo(previousInQueue()) : null} onNext={nextInQueue() ? () => goTo(nextInQueue()) : nextEpisode ? () => goTo(nextEpisode) : null} nextLabel={nextInQueue() ? 'Next in queue' : 'Next episode'} trackInfo={trackSel.subtitles.length || trackSel.audio.length > 1 ? { audio: trackSel.audio, audioIndex: trackSel.audioIndex, onAudio: chooseAudio, subtitles: trackSel.subtitles, subtitleId: trackSel.subtitleId, onSubtitle: trackSel.setSubtitleId } : undefined} playbackRate={playbackRate} onRateChange={changeRate} onMarkWatched={markWatched} title={title} kicker={seriesLine} badge={[quality, playbackMode === 'hls' ? 'Transcoding' : 'Direct play'].filter(Boolean).join(' · ')} queueItems={queue.items} onQueueGo={goTo} transcode={{ on: playbackMode === 'hls', disabled: !playbackMode || audioForcesTranscode, forced: audioForcesTranscode, quality: transcodeQuality, qualities: TRANSCODE_QUALITIES, onToggle: () => { setPlayerError(false); setTranscode(playbackMode !== 'hls'); }, onQuality: value => { setPlayerError(false); setTranscodeQuality(value); } }} />
            </div>
          </> : <div className="rs-player-offline"><Artwork item={item} preferBackdrop /><span>This file isn’t available right now.</span></div>}
        </section>
        <div className={`rs-detail-intro ${titleLengthClass}`}>
          <p className="rs-detail-kicker">{seriesLine || item.studioName || 'Your library'}</p>
          <h1 title={item.title}>{title}</h1>
          {item.title !== displayTitle(item) && <p className="rs-detail-original">{item.title}</p>}
          <div className="rs-detail-meta">
            {year && <span>{year}</span>}
            {item.durationSeconds > 0 && <span>{formatRuntime(item.durationSeconds)}</span>}
            {quality && <span className="rs-spec">{quality}</span>}
            {item.files?.length > 1 && <span className="rs-spec">{item.files.length} versions</span>}
            {item.studioName && item.studioId && <Link to={`/studios/${item.studioId}`}>{item.studioName}</Link>}
          </div>
                    <div className="rs-detail-actions">
            <button type="button" className="rs-button rs-button-primary rs-button-lg" onClick={play} disabled={!available}><Play size={18} fill="currentColor" /> {!available ? 'File offline' : resumable ? `Resume · ${Math.max(1, Math.round((item.durationSeconds - item.positionSeconds) / 60))} min left` : 'Play'}</button>
            {resumable && <button type="button" className="rs-button rs-button-outline rs-button-lg" onClick={playFromStart}>Play from start</button>}
            <button type="button" className={`rs-button rs-button-outline rs-button-lg ${item.favorite ? 'is-favorite' : ''}`} aria-pressed={!!item.favorite} onClick={toggleFavorite}><Heart size={18} fill={item.favorite ? 'currentColor' : 'none'} /> {item.favorite ? 'Favorited' : 'Favorite'}</button>
            <button type="button" className={`rs-button rs-button-outline rs-button-lg ${item.watchLater ? 'is-favorite' : ''}`} aria-pressed={!!item.watchLater} onClick={toggleWatchLater}>{item.watchLater ? <BookmarkCheck size={18} /> : <Bookmark size={18} />} {item.watchLater ? 'In Watch later' : 'Watch later'}</button>
            <button type="button" className="rs-button rs-button-outline rs-button-lg" onClick={() => setPlaylistOpen(true)}><ListPlus size={18} /> Add to playlist</button>
            <DropdownMenu><DropdownMenuTrigger asChild><button type="button" className="rs-button rs-button-outline rs-button-lg rs-button-icon rs-detail-more" aria-label="More"><Ellipsis size={18} /></button></DropdownMenuTrigger><DropdownMenuContent align="end"><DropdownMenuItem onSelect={addToQueue}><ListPlus size={16} /> Add to queue</DropdownMenuItem><DropdownMenuItem onSelect={() => setPlaylistOpen(true)}><ListVideo size={16} /> Add to playlist</DropdownMenuItem><DropdownMenuItem onSelect={markWatched}><Check size={16} /> Mark as watched</DropdownMenuItem>{isAdmin && <DropdownMenuItem onSelect={() => setSettingsOpen(true)}><Settings2 size={16} /> Settings</DropdownMenuItem>}</DropdownMenuContent></DropdownMenu>
            <AddToPlaylist mediaItemId={item.id} open={playlistOpen} onOpenChange={setPlaylistOpen} />
          </div>
          {item.description && <p className="rs-detail-description">{item.description}</p>}
          <div className="rs-rating-card">
            <div className="rs-detail-rating"><span>Your rating</span><StarRating value={starsFromRating(item.myRating)} onChange={setRating} size={22} /></div>
            {item.ratingCount > 0 && <div className="rs-rating-stat"><span>Average</span><strong className="rs-rating-avg" title="Average of all users' ratings">{(item.rating / 2).toFixed(1)} · {item.ratingCount} {item.ratingCount === 1 ? 'rating' : 'ratings'}</strong></div>}
            {item.watchCount > 0 && <div className="rs-rating-stat"><span>You watched</span><strong>{item.watchCount} {item.watchCount === 1 ? 'time' : 'times'}{item.lastWatchedAt ? ` · last ${new Date(item.lastWatchedAt).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}` : ''}</strong></div>}
            {item.dateAdded && <div className="rs-rating-stat"><span>Added</span><strong>{new Date(item.dateAdded).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })}</strong></div>}
          </div>
        </div>
      </div>
      {item.seriesId ? <SeriesSideList item={item} nextEpisode={nextEpisode} /> : queueBeside ? <aside className="rs-watch-side"><QueuePanel currentId={item.id} /></aside> : <SimilarSideList items={related} />}
    </div>
    {available && <Markers mediaId={item.id} videoRef={playerRef} markers={markers} onChange={setMarkers} onJump={jumpTo} />}
    {!queueBeside && queue.items.length > 0 && <QueuePanel currentId={item.id} />}
    <div className="rs-detail-lower">
      <DetailFacts item={item} />
      <TechnicalCard item={item} />
    </div>
    <div className="rs-detail-shelves">
      {leadPerson && <Shelf eyebrow="More with" title={leadPerson.name} href={`/performers/${leadPerson.id}`} items={personMore} />}
      <Shelf eyebrow={`More from ${item.studioName || 'this studio'}`} title={item.studioName || 'More from this studio'} href={item.studioId ? `/studios/${item.studioId}` : undefined} items={studioMore} />
      <Shelf eyebrow="Suggested" title="Similar titles" items={related} />
    </div>
    <MediaSettings mediaItemId={item.id} canDelete={isAdmin} onDeleted={() => { setSettingsOpen(false); navigate('/library'); }} open={settingsOpen} onOpenChange={setSettingsOpen} onChanged={() => getMediaItem(id).then(setItem).catch(() => {})} />
  </div>;
}
