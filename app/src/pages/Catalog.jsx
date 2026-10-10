import React, { useEffect, useMemo, useRef, useState } from 'react';
import { flushSync } from 'react-dom';
import { Link, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { LayoutGrid, Rows3, AlertTriangle, ArrowLeft, ArrowRight, ArrowUpRight, Bookmark, BookmarkCheck, Building2, Check, Dices, Film, Layers3, Sparkles, Users, Globe, Heart, ListPlus, ListVideo, Maximize2, Play, Plus, RefreshCw, Search, SearchX, Settings2, Shuffle, SkipBack, SkipForward, SlidersHorizontal, Star, X, Zap } from 'lucide-react';
import { monogram } from '../components/Monogram';
import { splitBio } from '../bioFacts';
import { Button, Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, Input, Tabs, TabsList, TabsTrigger, useToast } from '@spatika/react';
import {
  addCollectionItem, addWatchLater, artworkUrl, createCollection, getMarkers, previewUrl, removeWatchLater, getCategories, getCollections, getContinueWatching, getNextInSeries, getNextUp, subtitleUrl, getHomeRecommendations, startWatchSession, updateWatchSession, getMediaItem, getRelatedMedia, getMediaItems,
  createSeries, getPerformer, getPerformers, getSeries, getSeriesItem, getStudio, getStudios, performerImageUrl, setPerformerFavorite, studioImageUrl, updateMediaItem, updatePlayback, getRelatedPerformers } from '../api';
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
import { FilterSelect } from '../components/FilterControls';
import { usePersistedState } from '../hooks/usePersistedState';
import { useScrollRestoration } from '../hooks/useScrollRestoration';
import { useColumnCount, useInfiniteList, VirtualWindow } from '../virtual';
import { cardMetrics, useCardStyle } from '../cardStyle';
import PlayerControls from '../components/PlayerControls';
import Markers from '../components/Markers';
import QueuePanel from '../components/QueuePanel';
import { isFullscreen as isPlayerFullscreen, onFullscreenChange, toggleFullscreen } from '../fullscreen';
import { enqueue, nextInQueue, previousInQueue, startQueue, syncPlaying, useQueue } from '../queue';
import { fetchRandomTitles, playAll } from '../queueActions';
import { adminHref } from '../appLinks';

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

function MediaCard({ item, exclude = [], kicker, showPlayCount = false, mostPlayed = false }) {
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
  return <Link className={`rs-media-card ${toneClass(item.id)}`} to={`/library/${item.id}`} state={{ from: `${location.pathname}${location.search}` }} aria-label={`${displayTitle(item)}${item.studioName ? `, ${item.studioName}` : ''}`} onMouseEnter={startPreview} onMouseLeave={stopPreview} onFocus={startPreview} onBlur={stopPreview}>
    <div className="rs-media-card-art">
      <Artwork item={item} />
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

  return <section className={`rs-feature rs-hero${bleed ? ' rs-hero-bleed' : ''}${paused ? ' is-paused' : ''}`} aria-roledescription="carousel" aria-label="Featured titles" tabIndex={count > 1 ? 0 : undefined}
    style={{ '--hero-interval': `${HERO_AUTOPLAY_MS}ms` }}
    onMouseEnter={() => setPaused(true)} onMouseLeave={() => setPaused(false)} onFocus={() => setPaused(true)} onBlur={() => setPaused(false)}
    onKeyDown={onKeyDown} onTouchStart={event => { touchStart.current = event.touches[0].clientX; }} onTouchEnd={onTouchEnd}>
    {items.map((item, index) => <div key={item.id} className={`rs-hero-slide ${toneClass(item.id)}${index === active ? ' is-active' : ''}`} aria-hidden={index !== active} aria-roledescription="slide" aria-label={`${index + 1} of ${count}`}>
      <Artwork item={item} className="rs-feature-art" preferBackdrop />
      <div className="rs-feature-shade" />
      <div className="rs-feature-copy">
        <p className="rs-eyebrow"><span className="rs-live-dot" /> {eyebrow}</p>
        <h2 title={item.title}>{displayTitle(item)}</h2>
        <p className="rs-feature-meta">{metaFor(item)}</p>
        <div className="rs-feature-actions">
          <Link className="rs-button rs-button-primary" to={`/library/${item.id}`} state={{ autoplay: true }} tabIndex={index === active ? undefined : -1}><Play size={16} fill="currentColor" /> Play</Link>
          <Link className="rs-button rs-button-outline" to={`/library/${item.id}`} tabIndex={index === active ? undefined : -1}>Details <ArrowRight size={16} /></Link>
        </div>
      </div>
    </div>)}
    {count > 1 && <>
      <div className="rs-hero-nav">
        <span className="rs-hero-count">{String(active + 1).padStart(2, '0')} <i>/</i> {String(count).padStart(2, '0')}</span>
        <button type="button" aria-label="Previous title" onClick={() => goTo(active - 1)}><ArrowLeft size={17} /></button>
        <button type="button" aria-label="Next title" onClick={() => goTo(active + 1)}><ArrowRight size={17} /></button>
      </div>
      <div className="rs-hero-dots">{items.map((item, index) => <button key={`${item.id}:${index === active ? active : 'x'}`} type="button" className={index === active ? 'is-active' : ''} aria-label={`Show ${displayTitle(item)}`} aria-current={index === active} onClick={() => goTo(index)}><span /></button>)}</div>
    </>}
  </section>;
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

// "Part of <series>" on the video page: the series in order, with the current title marked and the next one called out.
function SeriesShelf({ item, nextEpisode }) {
  const { terms, mode } = useLibrary();
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
  const start = Math.max(0, position - 2);
  const visible = position > 3 ? parts.slice(start) : parts;
  const kickerFor = part => {
    const index = parts.indexOf(part) + 1;
    const label = `${episodes ? 'Episode' : 'Part'} ${index}`;
    if (String(part.id) === String(item.id)) return `Now playing · ${label}`;
    if (String(part.id) === nextId) return `Up next · ${label}`;
    return label;
  };
  const route = `/${episodes && mode === 'entertainment' ? 'shows' : 'series'}/${item.seriesId}`;
  // The current title links to this very page, so bring the player back into view instead of doing nothing.
  const backToPlayer = event => { event.preventDefault(); event.stopPropagation(); (document.querySelector('.spk-app-content') || window).scrollTo({ top: 0, behavior: 'smooth' }); };
  return <section className="rs-series-shelf">
    <Shelf eyebrow={position >= 0 ? `${episodes ? 'Episode' : 'Part'} ${position + 1} of ${parts.length}` : plural(parts.length, 'part')} title={`More in ${item.seriesName || (episodes ? terms.show : terms.series)}`} href={route} hrefLabel="Open series" limit={200} items={visible}
      renderItem={part => <div key={part.id} className={`rs-series-part${String(part.id) === String(item.id) ? ' is-current' : ''}${String(part.id) === nextId ? ' is-next' : ''}`} onClickCapture={String(part.id) === String(item.id) ? backToPlayer : undefined}><MediaCard item={part} kicker={kickerFor(part)} /></div>} />
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
  useEffect(() => { getHomeRecommendations().then(result => setRecs(result.sections || [])).catch(() => {}); }, []);
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
  const firstName = String(session?.displayName || '').trim().split(/\s+/)[0] || String(session?.email || '').split('@')[0];
  const newThisWeek = items.filter(isNew).length;
  const summary = [
    total ? plural(total, 'title') : null,
    counts.performers ? plural(counts.performers, 'performer') : null,
    counts.studios ? plural(counts.studios, 'studio') : null,
  ].filter(Boolean).join(' · ');
  const faces = people.filter(p => p.profileImageId).slice(0, 4);
  const peekFaces = (faces.length ? faces : people.slice(0, 4)).map(person => <span key={person.id} className="rs-peek-face"><DirectoryMark item={person} index={0} type="performers" portrait /></span>);
  const peekStudios = studios.slice(0, 4).map(studio => <span key={studio.id} className="rs-peek-studio"><DirectoryMark item={studio} index={0} type="studios" /></span>);

  return <div className="rs-home">
    {heroItems.length ? <HeroCarousel items={heroItems} bleed /> : !loading && !error && <div className="rs-home-empty"><span>Start here</span><h2>Your library is empty.</h2><p>The setup wizard in Admin walks you through adding your videos. Your titles will land on this page.</p><a className="rs-button rs-button-primary" href={adminHref('/admin/setup')}>Set up your library <ArrowRight size={17} /></a></div>}
    <Status loading={loading && !items.length} error={error} retry={refresh} />
    <div className="rs-home-body">
      <Shelf title="Continue watching" eyebrow="Pick up where you left off" tone={0} items={continueItems} renderItem={item => <MediaCard key={item.id} item={item} kicker={resumeKicker(item)} />} />
      <Shelf title="Next up" eyebrow="Your next episodes" tone={3} items={nextUpItems} renderItem={item => <MediaCard key={item.id} item={item} kicker={nextUpKicker(item)} />} />
      {total > 0 && <section className="rs-explore" aria-labelledby="rs-explore-title">
        <div className="rs-section-head"><div><p className="rs-eyebrow">Explore</p><h2 id="rs-explore-title">Find something new</h2></div></div>
        <div className="rs-explore-grid">
          <ExploreTile to="/performers" tone={7} icon={Users} label={homeTerms.people} count={counts.performers} unit="person" units="people" peek={peekFaces.length ? peekFaces : null} />
          <ExploreTile to="/studios" tone={1} icon={Building2} label={homeTerms.studios} count={counts.studios} unit="studio" peek={peekStudios.length ? peekStudios : null} />
          <ExploreTile to={homeMode === 'entertainment' ? '/shows' : '/series'} tone={3} icon={ListVideo} label={homeMode === 'entertainment' ? homeTerms.shows : homeTerms.seriesPlural} count={counts.series} unit="series" units="series" chips={seriesNames} />
          <ExploreTile to="/categories" tone={5} icon={Layers3} label={homeTerms.categories} count={counts.categories} unit={homeTerms.category.toLowerCase()} units={homeTerms.categories.toLowerCase()} chips={moods.slice(0, 4).map(m => m.name)} />
        </div>
      </section>}
      <Shelf title="Recently added" eyebrow="Fresh arrivals" tone={4} href="/library" items={fresh} />
      <Shelf title="Faces in your library" eyebrow={homeTerms.people} tone={7} href="/performers" hrefLabel={`All ${homeTerms.people.toLowerCase()}`} className="rs-faces" limit={20} items={people} renderItem={person => <FaceCard key={person.id} person={person} />} />
      <Shelf title="Your favorites" eyebrow="Saved with love" tone={6} href="/favorites" items={favorites.slice(0, 12)} />
      {recs.map((section, index) => <Shelf key={section.key} title={section.title} eyebrow={section.subtitle || 'Picked for you'} tone={[5, 2, 3, 1][index % 4]} items={section.items} />)}
      {!!moods.length && <section className="rs-moods" aria-labelledby="rs-moods-title">
        <div className="rs-section-head"><div><p className="rs-eyebrow">Categories</p><h2 id="rs-moods-title">Browse by mood</h2></div><Link to="/categories">All categories <ArrowRight size={16} /></Link></div>
        <div className="rs-mood-cloud">{moods.map(mood => <Link key={mood.id} to={`/categories/${mood.id}`} className={`rs-mood ${toneClass(mood.name)}`}>{mood.name}<small>{mood.mediaCount}</small></Link>)}</div>
      </section>}
      {!!studios.length && <section className="rs-studios"><div className="rs-section-head"><div><p className="rs-eyebrow">Studios</p><h2>Most titles</h2></div><Link to="/studios">All studios <ArrowRight size={16} /></Link></div><div className="rs-studio-grid">{studios.slice(0, 4).map(studio => <Link key={studio.id} to={`/studios/${studio.id}`} className={toneClass(studio.id)}><span className="rs-home-studio-mark"><DirectoryMark item={studio} index={studio.id} type="studios" /></span><strong>{studio.name}</strong><small>{plural(studio.mediaCount, 'title')}</small><ArrowUpRight size={18} /></Link>)}</div></section>}
    </div>
  </div>;
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
function TitleFilters({ studios, studio, onStudio, sort, onSort, rating, onRating, seriesOrder = false, performerOrder = false }) {
  const studioOptions = useMemo(() => [{ value: 'all', label: 'All studios' }, ...(studios || []).map(name => ({ value: name, label: name }))], [studios]);
  const ratingOptions = [{ value: '0', label: 'Any rating' }, ...[5, 4, 3, 2, 1].map(n => ({ value: String(n), label: `${n}+ stars` }))];
  const sortOptions = [
    ...(seriesOrder ? [{ value: 'series', label: 'Series order' }] : []),
    ...(performerOrder ? [{ value: 'watched', label: 'Most played' }, { value: 'rated', label: 'Highest rated' }] : []),
    { value: 'recent', label: 'Newest first' }, { value: 'oldest', label: 'Oldest first' }, { value: 'title', label: 'Title A–Z' },
  ];
  return <div className="rs-filters">
    {studios && <FilterSelect label="Studio" value={studio} options={studioOptions} onChange={onStudio} searchable searchPlaceholder="Find a studio…" />}
    {onRating && <FilterSelect label="Rating" value={String(rating)} options={ratingOptions} onChange={onRating} />}
    <FilterSelect label="Sort" value={sort} options={sortOptions} onChange={onSort} />
  </div>;
}

// Play / Shuffle for whatever list is on screen: fills the queue from the same filters and opens the first title.
function PlayButtons({ filters, ordered = false }) {
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
    <button type="button" className="rs-button rs-button-outline" disabled={!!busy} onClick={() => run(true)}><Shuffle size={16} /> Shuffle</button>
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

export function Catalog({ type = 'library' }) {
  const { terms } = useLibrary();
  const [params, setParams] = useSearchParams();
  const query = params.get('q') || '';
  const debouncedQuery = useDebounce(query, 250);
  const [studio, setStudio] = usePersistedState(`catalog:${type}:studio`, 'all');
  const [sort, setSort] = usePersistedState(`catalog:${type}:sort`, 'recent');
  const [rating, setRating] = usePersistedState(`catalog:${type}:rating`, '0');
  const studios = useStudioNames();
  const tab = type === 'favorites' || type === 'search' ? (['performers', 'videos'].includes(params.get('tab')) ? params.get('tab') : 'overview') : 'videos';
  const setTab = value => { const next = new URLSearchParams(params); if (value === 'overview') next.delete('tab'); else next.set('tab', value); setParams(next); };
  const showPerformers = (type === 'search' || type === 'favorites') && tab !== 'videos';
  const showVideos = tab !== 'performers';
  const [cardStyle] = useCardStyle();
  const cards = cardMetrics(cardStyle);
  const minColumn = useColumnCount(cards.wideColumn, cards.narrowColumn, '(max-width: 640px)');
  const filters = useMemo(() => ({
    ...(type === 'favorites' ? { favorite: 'true' } : {}),
    ...(type === 'watchlater' ? { watchLater: 'true' } : {}),
    ...(type === 'movies' ? { kind: 'movie' } : {}),
    ...(debouncedQuery.trim() ? { q: debouncedQuery.trim() } : {}),
    ...(studio !== 'all' ? { studio } : {}),
    ...(rating !== '0' ? { minRating: String(ratingFromStars(Number(rating))) } : {}),
    ...(sort !== 'recent' ? { sort } : {}),
  }), [type, debouncedQuery, studio, rating, sort]);
  const { items, total, loading, loadingMore, error, hasMore, loadMore, refresh } = useInfiniteList(mediaQuery(filters), JSON.stringify(filters));
  const label = type === 'search' ? 'Search' : type === 'favorites' ? 'Favorites' : type === 'watchlater' ? 'Watch later' : type === 'movies' ? terms.movies : terms.library;
  useScrollRestoration(showVideos && !!items.length, [items.length]);

  return <div className={`rs-page rs-catalog-${type}`}>
    <header className="rs-page-header"><p className="rs-eyebrow">{type === 'favorites' || type === 'watchlater' ? 'Saved' : type === 'search' ? 'Search' : 'Library'}</p><div className="rs-page-title-row"><h1>{label}<span className="rs-count">{loading && !items.length ? '…' : `${total.toLocaleString()} ${total === 1 ? 'title' : 'titles'}`}</span></h1><div className="rs-title-controls">{showVideos && total > 0 && <PlayButtons filters={filters} ordered={type === 'watchlater'} />}{showVideos && <TitleFilters studios={studios} studio={studio} onStudio={setStudio} rating={rating} onRating={setRating} sort={sort} onSort={setSort} />}</div></div>{type === 'favorites' && <Tabs className="rs-favorites-tabs" value={tab} onValueChange={setTab}><TabsList><TabsTrigger value="overview">Overview</TabsTrigger><TabsTrigger value="performers">Performers</TabsTrigger><TabsTrigger value="videos">Videos</TabsTrigger></TabsList></Tabs>}<p>{type === 'favorites' ? 'Titles you saved to come back to.' : type === 'watchlater' ? 'Titles you set aside to watch when you have time. Finished ones drop off on their own.' : type === 'search' ? 'Search titles, performers, and studios.' : type === 'movies' ? 'Every movie in this library.' : 'Every title in your library.'}</p></header>
    {showPerformers && <SearchPerformers query={debouncedQuery.trim()} favoritesOnly={type === 'favorites'} full={tab === 'performers'} onMore={type === 'favorites' && tab === 'overview' ? () => setTab('performers') : undefined} />}
    {showVideos && <>
    <Status loading={loading && !items.length} error={error} empty={!loading && !error && !items.length && !query && studio === 'all'} retry={refresh} title={type === 'favorites' ? 'No favorites yet' : type === 'watchlater' ? 'Nothing saved for later' : 'Your library is waiting'} message={type === 'favorites' ? 'Save titles from their detail pages and they will appear here.' : type === 'watchlater' ? 'Use Watch later on a title and it will wait for you here.' : 'Add media from Admin to begin your collection.'} />
    {!loading && !error && !items.length && (query || studio !== 'all') && <Status empty icon={SearchX} title="No matches found" message="Try another search or studio." />}
    {!!items.length && <VirtualWindow items={items} minColumn={minColumn} gapX={16} gapY={24} aspect={cards.aspect} extra={96} hasMore={hasMore} onNearEnd={loadMore} renderItem={item => <MediaCard item={item} />} />}
    {loadingMore && <p className="rs-virtual-status">Loading more titles…</p>}
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

function PerformerCard({ item, index, note }) {
  const count = item.mediaCount || 0;
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

export function Browse({ type, variant }) {
  const { terms, isAdmin, mode } = useLibrary();
  const extra = mode === 'entertainment' ? BROWSE_VARIANTS[variant] : null;
  const config = { ...dimensions[type], ...dimensionTerms(type, terms), ...(extra ? { title: extra.title(terms), singular: extra.singular(terms) } : {}) };
  const [query, setQuery] = useState('');
  const [onlyFavorites, setOnlyFavorites] = usePersistedState(`browse:${type}:onlyFavorites`, false);
  const [letter, setLetter] = usePersistedState(`browse:${type}:letter`, '');
  const [showEmpty, setShowEmpty] = usePersistedState(`browse:${type}:showEmpty`, false);
  const [sort, setSort] = usePersistedState(`browse:${type}:sort`, 'count');
  const [view, setView] = usePersistedState(`browse:${type}:view`, 'grid');
  const [creating, setCreating] = useState(false);
  const navigate = useNavigate();
  const debounced = useDebounce(query, 250);
  const favoriteFilter = type === 'performers' && onlyFavorites;
  const { items, total, loading, loadingMore, error, hasMore, loadMore, refresh } = useInfiniteList(
    ({ limit, offset }) => config.fetchPage({ limit, offset, q: debounced.trim(), favorite: favoriteFilter ? 'true' : '', letter: type === 'performers' ? letter : '', showEmpty, ...(type === 'performers' && sort !== 'count' ? { sort } : {}), ...(extra?.params || {}) }),
    `${type}:${variant || ''}:${debounced.trim()}:${favoriteFilter}:${letter}:${showEmpty}:${sort}`,
  );
  useScrollRestoration(!!items.length, [items.length]);

  const blurbs = {
    performers: 'The people in your library.',
    studios: 'Who made each title.',
    series: 'Titles that belong together.',
    categories: 'Topics used in your collection.',
  };
  return <div className={`rs-page rs-catalog-${type}`}>
    <header className="rs-page-header rs-directory-header"><div className="rs-directory-headrow"><div className="rs-directory-title"><p className="rs-eyebrow">Discover</p><h1>{config.title}<span className="rs-count">{loading && !items.length ? '…' : total}</span></h1><p>{extra?.blurb || blurbs[type]}</p></div><div className="rs-directory-toolbar"><label className="rs-search"><Search size={18} /><input aria-label={`Search ${config.title.toLowerCase()}`} value={query} onChange={e => setQuery(e.target.value)} placeholder={`Search ${config.title.toLowerCase()}`} /></label>{type === 'series' && <div className="rs-view-toggle" role="group" aria-label="View"><button type="button" aria-pressed={view !== 'list'} title="Grid view" onClick={() => setView('grid')}><LayoutGrid size={16} /></button><button type="button" aria-pressed={view === 'list'} title="List view" onClick={() => setView('list')}><Rows3 size={16} /></button></div>}{type === 'categories' ? <button type="button" className="rs-button rs-button-outline" aria-pressed={showEmpty} onClick={() => setShowEmpty(value => !value)}>{showEmpty ? 'Show used only' : 'Show all categories'}</button> : type !== 'performers' && <span>A — Z</span>}{type === 'performers' && <FilterSelect label="Sort" value={sort} onChange={setSort} options={[{ value: 'count', label: 'Most titles' }, { value: 'name', label: 'Name A–Z' }, { value: 'name_desc', label: 'Name Z–A' }]} />}{isAdmin && type === 'performers' && variant !== 'directors' && <button type="button" className="rs-button rs-button-outline rs-toolbar-new" onClick={() => setCreating(true)}><Plus size={16} /> New {terms.person.toLowerCase()}</button>}{isAdmin && type === 'series' && variant !== 'shows' && <button type="button" className="rs-button rs-button-outline rs-toolbar-new" onClick={() => setCreating(true)}><Plus size={16} /> New {terms.series.toLowerCase()}</button>}</div></div></header>
    {type === 'performers' && <div className="rs-performer-subbar">
      <Tabs value={onlyFavorites ? 'favorites' : 'all'} onValueChange={value => setOnlyFavorites(value === 'favorites')}><TabsList><TabsTrigger value="all">All</TabsTrigger><TabsTrigger value="favorites"><Heart size={14} /> Favorites</TabsTrigger></TabsList></Tabs>
      <nav className="rs-az" aria-label="Filter by first letter">{['#', ...'ABCDEFGHIJKLMNOPQRSTUVWXYZ'].map(l => <button key={l} type="button" className={letter === l ? 'is-active' : ''} aria-pressed={letter === l} onClick={() => setLetter(current => current === l ? '' : l)}>{l}</button>)}</nav>
    </div>}
    {type === 'performers' && <NewPerformerDialog open={creating} onOpenChange={setCreating} onCreated={id => navigate(`/performers/${id}`)} />}
    {type === 'series' && <NewSeriesDialog open={creating} onOpenChange={setCreating} onCreated={id => navigate(`/series/${id}`)} />}
    <Status loading={loading && !items.length} error={error} empty={!loading && !error && !items.length && !query && !favoriteFilter && !letter} retry={refresh} title={`No ${config.title.toLowerCase()} yet`} message={type === 'series' ? 'Group the scenes of a movie or the episodes of a show: choose New series, or open a video’s Settings and use its Series tab.' : type === 'categories' ? 'No titles have categories yet.' : 'They appear here as your library is scanned.'} href={type === 'series' || type === 'categories' ? '/library' : undefined} action="Browse titles" />
    {!loading && !error && (!!query || favoriteFilter || !!letter) && !items.length && <Status empty title={favoriteFilter && !query ? 'No favorite performers yet' : 'No matches'} message={favoriteFilter && !query ? 'Open a performer and tap Favorite.' : 'Try a different name.'} />}
    {!!items.length && type === 'performers' && <VirtualWindow className="rs-portrait-grid" items={items} minColumn={180} gapX={16} gapY={16} aspect={3 / 4} hasMore={hasMore} onNearEnd={loadMore} renderItem={(item, index) => <PerformerCard item={item} index={index} />} />}
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
    getRelatedPerformers(id, 12).then(result => { if (live) setSimilar((result.items || []).filter(person => person.profileImageId)); }).catch(() => {});
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
  useEffect(() => { setStudio('all'); setSort(defaultSort); }, [type, id, defaultSort]);
  const studios = useStudioNames(type !== 'studios');
  const filters = useMemo(() => ({ [config.filterKey]: id }), [config.filterKey, id]);
  const listFilters = useMemo(() => ({ ...filters, ...(studio !== 'all' ? { studio } : {}), ...(sort !== 'recent' || type === 'series' ? { sort } : {}) }), [filters, studio, sort, type]);
  const { items, total, loading, loadingMore, error, hasMore, loadMore, refresh } = useInfiniteList(mediaQuery(listFilters), `${type}:${id}:${studio}:${sort}`);
  useScrollRestoration(!!items.length, [items.length]);
  const filtered = studio !== 'all' || sort !== defaultSort;
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
  const { items: favoriteItems } = useMediaList(type === 'studios' ? { ...studioFilters, favorite: 'true' } : null);
  const loadEntity = React.useCallback(() => {
    const request = type === 'performers' ? getPerformer(id) : type === 'studios' ? getStudio(id) : type === 'categories' ? getCategories().then(result => (result.items || []).find(item => String(item.id) === String(id))) : getSeriesItem(id);
    return request.then(result => { setName(result?.name || ''); setEntity(result || null); return result; });
  }, [type, id]);
  useEffect(() => { let active = true; loadEntity().catch(() => { if (active) setEntity(null); }); return () => { active = false; }; }, [loadEntity]);
  const countLabel = `${loading && !items.length ? '…' : total} ${total === 1 ? 'title' : 'titles'} in your collection`;
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
  return <div className={`rs-page rs-entity-page ${toneClass(`${type}:${id}`)}`}>
    {type === 'performers' ? <header className={`rs-performer-hero ${entity ? performerTheme(entity)['--pf-pattern'] : ''}`} style={entity ? performerTheme(entity) : undefined}>
      {entity?.profileImageId && <span className="rs-performer-hero-bg" style={{ backgroundImage: `url(${performerImageUrl(entity.id, entity.profileImageId)})` }} aria-hidden="true" />}
      <span className="rs-entity-portrait">{entity ? <DirectoryMark item={entity} index={0} type="performers" portrait /> : null}</span>
      <div className="rs-performer-hero-copy">
        <p className="rs-eyebrow">PERFORMER / YOUR ARCHIVE</p>
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
      {entity && <div className="rs-studio-hero-actions rs-performer-hero-actions">
        <button type="button" className={`rs-button rs-button-outline ${entity.favorite ? 'is-favorite' : ''}`} aria-pressed={!!entity.favorite} onClick={toggleFavorite}><Heart size={17} fill={entity.favorite ? 'currentColor' : 'none'} /> {entity.favorite ? 'Favorited' : 'Favorite'}</button>
        {isAdmin && <button type="button" className="rs-button rs-button-outline" onClick={() => setSettingsOpen(true)}><Settings2 size={17} /> Settings</button>}
      </div>}
    </header> : type === 'series' ? <SeriesHero entity={entity} name={name} countLabel={countLabel} onSettings={isAdmin ? () => setSettingsOpen(true) : null} /> : <header className={`rs-page-header rs-entity-header${portrait ? ' rs-entity-hero' : ''}${type === 'studios' ? ' rs-studio-hero' : ''}`}>
      {type === 'studios' && entity && entity.logoImageId && <span className="rs-studio-hero-logo"><DirectoryMark item={entity} index={0} type="studios" /></span>}
      {portrait && entity && <span className="rs-entity-portrait"><DirectoryMark item={entity} index={0} type="performers" portrait /></span>}
      <div><p className="rs-eyebrow">{config.singular.toUpperCase()} / YOUR ARCHIVE</p><h1>{name || 'Loading…'}</h1><p>{countLabel}</p>
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
    {isStudio && !!featuredItems.length && <HeroCarousel items={featuredItems} eyebrow={type === 'performers' ? `FEATURING ${(name || 'this performer').toUpperCase()}` : `FROM ${(name || 'this studio').toUpperCase()}`} exclude={type === 'performers' ? [name, ...(entity?.aliases || [])] : []} />}
    {type === 'studios' && <Shelf title="Most watched" eyebrow="Popular" items={watchedItems.filter(item => item.watchCount > 0)} />}
    {type === 'studios' && <Shelf title="Favorites" eyebrow="Saved" items={favoriteItems} />}
    {(!!items.length || filtered) && <div className="rs-section-head rs-videos-head"><div><p className="rs-eyebrow">Library</p><h2>All videos</h2></div><div className="rs-title-controls">{total > 0 && <PlayButtons filters={listFilters} ordered={type === 'series'} />}<TitleFilters studios={type === 'studios' ? null : studios} studio={studio} onStudio={setStudio} sort={sort} onSort={setSort} seriesOrder={type === 'series'} performerOrder={type === 'performers'} /></div></div>}
    {!loading && !error && !items.length && filtered && <Status empty icon={SearchX} title="No matches found" message="Try another studio or sort order." />}
    {!!items.length && <VirtualWindow items={items} minColumn={minColumn} gapX={16} gapY={24} aspect={cards.aspect} extra={96} hasMore={hasMore} onNearEnd={loadMore} renderItem={(item, index) => <MediaCard item={item} index={index} exclude={type === 'performers' ? [name, ...(entity?.aliases || [])] : []} kicker={type === 'series' && item.seriesPosition != null ? [`No. ${item.seriesPosition}`, item.studioName].filter(Boolean).join(' · ') : undefined} showPlayCount={type === 'performers'} mostPlayed={type === 'performers' && sort === 'watched' && index === 0 && item.watchCount > 0} />} />}
    {loadingMore && <p className="rs-virtual-status">Loading more titles…</p>}
    {type === 'performers' && !!similar.length && <section className="rs-shelf rs-similar-performers" aria-label="You might also like">
      <div className="rs-section-head"><div><p className="rs-eyebrow">Suggested</p><h2>You might also like</h2></div><Link to="/performers">All performers <ArrowRight size={16} /></Link></div>
      <div className="rs-shelf-track">{similar.map((person, index) => <PerformerCard key={person.id} item={person} index={index} note={person.sharedTitles ? `${person.sharedTitles} ${person.sharedTitles === 1 ? 'title' : 'titles'} together` : undefined} />)}</div>
    </section>}
  </div>;
}

function AddToPlaylist({ mediaItemId }) {
  const { toast } = useToast();
  const [open, setOpen] = useState(false);
  const [playlists, setPlaylists] = useState([]);
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  useEffect(() => { if (open) getCollections().then(result => setPlaylists((result.items || []).filter(item => item.kind === 'MANUAL'))).catch(err => toast({ title: 'Could not load playlists', description: err.message, tone: 'danger' })); }, [open, toast]);
  async function add(id, label) {
    setBusy(true);
    try { await addCollectionItem(id, mediaItemId); toast({ title: `Added to ${label}`, tone: 'success' }); setOpen(false); }
    catch (err) { toast({ title: 'Could not add to playlist', description: err.message, tone: 'danger' }); }
    finally { setBusy(false); }
  }
  async function create(event) {
    event.preventDefault();
    if (!name.trim()) return;
    setBusy(true);
    try {
      const result = await createCollection({ name: name.trim(), kind: 'MANUAL' });
      await addCollectionItem(result.id, mediaItemId);
      toast({ title: `Added to ${name.trim()}`, tone: 'success' });
      setName('');
      setOpen(false);
    } catch (err) { toast({ title: 'Could not create playlist', description: err.message, tone: 'danger' }); }
    finally { setBusy(false); }
  }
  return <><button className="rs-button rs-button-outline" onClick={() => setOpen(true)}><ListVideo size={17} /> Add to playlist</button>
    <Dialog open={open} onOpenChange={setOpen}><DialogContent><DialogHeader><DialogTitle>Add to playlist</DialogTitle><DialogDescription>Choose a playlist or make a new one.</DialogDescription></DialogHeader><div className="rs-playlist-picker">{playlists.map(list => <button key={list.id} type="button" disabled={busy} onClick={() => add(list.id, list.name)}><ListVideo size={18} /><span><strong>{list.name}</strong><small>{list.mediaCount} titles</small></span><ArrowRight size={16} /></button>)}{!playlists.length && <p>No playlists yet. Create one below.</p>}</div><form className="rs-add-playlist-form" onSubmit={create}><Input aria-label="New playlist name" placeholder="New playlist name" value={name} onChange={event => setName(event.target.value)} /><Button type="submit" disabled={busy || !name.trim()}>Create & add</Button></form></DialogContent></Dialog>
  </>;
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
  const personImage = person => person.profileImageId ? <CreditImage src={performerImageUrl(person.id)} /> : <span className="rs-cast-fallback rs-mono" style={monogram(person.name).style} aria-hidden="true">{monogram(person.name).initials}</span>;
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
      <div className="rs-cast">{cast.map(person => <Link key={person.id || person.name} className="rs-credit rs-cast-person" to={person.id ? `/performers/${person.id}` : search(person.name)}>{personImage(person)}<span>{person.name}{person.character && <small className="rs-cast-character">{person.character}</small>}</span></Link>)}</div>
    </section>}
    {['director', 'writer'].map(job => { const people = crew.filter(c => c.job === job); return people.length > 0 && <section className="rs-cast-block" key={job} aria-label={job === 'director' ? terms.directors : 'Writers'}>
      <h3 className="rs-facts-label">{job === 'director' ? (people.length === 1 ? terms.director : terms.directors) : (people.length === 1 ? 'Writer' : 'Writers')}</h3>
      <div className="rs-cast">{people.map(person => <Link key={`${job}:${person.id}`} className="rs-credit rs-cast-person" to={`/performers/${person.id}`}>{personImage(person)}<span>{person.name}</span></Link>)}</div>
    </section>; })}
    <dl className="rs-facts-grid">
      {item.studioName && <div><dt>Studio</dt><dd>{item.studioId ? <Link className="rs-credit" to={`/studios/${item.studioId}`}>{item.studioLogoImageId && <CreditImage studio src={studioImageUrl(item.studioId)} />}<span>{item.studioName}</span></Link> : item.studioName}</dd></div>}
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
  const studioId = item?.studioId;
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
  return <div className={`rs-detail rs-watch-page ${toneClass(item.id)} ${showBackdrop ? 'is-backdrop' : 'is-playing'}${portrait ? ' is-portrait' : ''}`}>
    <div className="rs-tv-backdrop" aria-hidden="true"><img key={`${item.id}-${useProviderBackdrop ? 'backdrop' : 'poster'}`} src={artworkUrl(item.id, useProviderBackdrop ? 'backdrop' : 'poster', useProviderBackdrop ? item.backdropImageId : item.posterImageId)} alt="" className={backdropArt === 'wide' || backdropArt === 'tall' ? 'is-loaded' : ''} onLoad={event => { const { naturalWidth: w, naturalHeight: h } = event.currentTarget; if (useProviderBackdrop && w / h < 1.3) { setFailedBackdropId(item.backdropImageId); setBackdropArt('loading'); } else setBackdropArt(w / h >= 1.3 ? 'wide' : 'tall'); }} onError={() => { if (useProviderBackdrop) { setFailedBackdropId(item.backdropImageId); setBackdropArt('loading'); } else setBackdropArt('none'); }} /></div>
    <section className="rs-detail-hero">
      <section className="rs-player-section" aria-label="Video player">
        {available ? <>
          <div className="rs-player-frame" ref={frameRef} tabIndex={0} onKeyDown={onFrameKeyDown} aria-label="Video player. Space plays or pauses, F toggles fullscreen, M mutes, arrow keys seek, J and L skip ten seconds, C toggles subtitles, N and P change title, less-than and greater-than change speed.">
          <video ref={playerRef} preload="metadata" playsInline x-webkit-airplay="allow" poster={artworkUrl(item.id, 'poster', item.posterImageId)} onLoadedMetadata={onLoadedMetadata} onCanPlay={onCanPlay} onPlay={() => { wantsAutoplay.current = false; resumePlayback.current = false; setStage('player'); setPlayerError(false); ensureSession(); session.current.lastTime = playerRef.current?.currentTime ?? null; }} onSeeked={() => { session.current.lastTime = playerRef.current?.currentTime ?? null; onCanPlay(); }} onTimeUpdate={onTimeUpdate} onPause={() => saveProgress(false)} onEnded={onEnded}>{trackSel.subtitles.filter(track => !track.burn).map(track => <track key={`${id}-${track.id}`} id={`rs-sub-${track.id}`} kind="subtitles" src={subtitleUrl(id, track.id)} srcLang={track.language || undefined} label={track.label} />)}</video>
          {upNext && nextEpisode && <UpNext item={nextEpisode} onPlay={() => goTo(nextEpisode)} onCancel={() => setUpNext(false)} />}
          {playerError && <div className="rs-player-error" role="alert">Playback stopped. <button type="button" onClick={() => { setPlayerError(false); retryPlayback(); }}>Retry from here</button>{playbackMode === 'direct' && <><button type="button" onClick={() => { setPlayerError(false); skipAhead(30); }}>Skip 30s ahead</button><button type="button" onClick={() => { setPlayerError(false); setTranscode(true); }}>Play transcoded</button></>}</div>}
          {skips > 0 && <span className="rs-player-notice" role="status">Jumped {lastSkip || 2}s past a spot that would not play ({skips} {skips === 1 ? 'time' : 'times'})</span>}
          <PlayerControls videoRef={playerRef} frameRef={frameRef} mediaId={item.id} markers={markers} enabled={available} onToggleFullscreen={fullscreen} isFullscreen={isFullscreen} onPrev={previousInQueue() ? () => goTo(previousInQueue()) : null} onNext={nextInQueue() ? () => goTo(nextInQueue()) : nextEpisode ? () => goTo(nextEpisode) : null} nextLabel={nextInQueue() ? 'Next in queue' : 'Next episode'} trackInfo={trackSel.subtitles.length || trackSel.audio.length > 1 ? { audio: trackSel.audio, audioIndex: trackSel.audioIndex, onAudio: chooseAudio, subtitles: trackSel.subtitles, subtitleId: trackSel.subtitleId, onSubtitle: trackSel.setSubtitleId } : undefined} playbackRate={playbackRate} onRateChange={changeRate} onMarkWatched={markWatched} transcode={{ on: playbackMode === 'hls', disabled: !playbackMode || audioForcesTranscode, forced: audioForcesTranscode, quality: transcodeQuality, qualities: TRANSCODE_QUALITIES, onToggle: () => { setPlayerError(false); setTranscode(playbackMode !== 'hls'); }, onQuality: value => { setPlayerError(false); setTranscodeQuality(value); } }} />
          </div>
        </> : <div className="rs-player-offline"><Artwork item={item} /><span>This file isn’t available right now.</span></div>}
      </section>
      <div className={`rs-detail-intro ${titleLengthClass}`}><p className="rs-eyebrow">{item.studioName || 'Your library'}</p><h1 title={item.title}>{title}</h1><p className="rs-detail-original">{item.title !== displayTitle(item) ? item.title : ''}</p><div className="rs-detail-meta">{[item.releaseDate?.slice(0, 4), item.seriesName, item.durationSeconds ? `${Math.round(item.durationSeconds / 60)} min` : null].filter(Boolean).map(value => <span key={value}>{value}</span>)}</div><div className="rs-detail-rating"><span>Your rating</span><StarRating value={starsFromRating(item.myRating)} onChange={setRating} size={20} />{item.ratingCount > 0 && <span className="rs-rating-avg" title="Average of all users' ratings">Average {(item.rating / 2).toFixed(1)} · {item.ratingCount} {item.ratingCount === 1 ? 'rating' : 'ratings'}</span>}</div>{item.description && <p className="rs-detail-description">{item.description}</p>}{resumable && showBackdrop && <div className="rs-tv-progress"><span><i style={{ width: `${Math.min(100, item.positionSeconds / item.durationSeconds * 100)}%` }} /></span>{Math.max(1, Math.round((item.durationSeconds - item.positionSeconds) / 60))} min left</div>}<div className="rs-detail-actions"><button className="rs-button rs-button-primary" onClick={play} disabled={!available}><Play size={17} fill="currentColor" /> {!available ? 'File offline' : resumable ? 'Resume' : 'Play'}</button><button className={`rs-button rs-button-outline ${item.favorite ? 'is-favorite' : ''}`} onClick={toggleFavorite}><Heart size={17} fill={item.favorite ? 'currentColor' : 'none'} /> {item.favorite ? 'Favorited' : 'Favorite'}</button><button type="button" className={`rs-button rs-button-outline ${item.watchLater ? 'is-favorite' : ''}`} aria-pressed={!!item.watchLater} onClick={toggleWatchLater}>{item.watchLater ? <BookmarkCheck size={17} /> : <Bookmark size={17} />} {item.watchLater ? 'In Watch later' : 'Watch later'}</button><button type="button" className="rs-button rs-button-outline" onClick={addToQueue}><ListPlus size={17} /> Add to queue</button><AddToPlaylist mediaItemId={item.id} />{isAdmin && <button type="button" className="rs-button rs-button-outline" onClick={() => setSettingsOpen(true)}><Settings2 size={17} /> Settings</button>}</div></div>
    </section>
    <SeriesShelf item={item} nextEpisode={nextEpisode} />
    <div className="rs-detail-lower">
      <QueuePanel currentId={item.id} />
      {available && <Markers mediaId={item.id} videoRef={playerRef} markers={markers} onChange={setMarkers} onJump={jumpTo} />}
      <DetailFacts item={item} />
    </div>
    <div className="rs-detail-shelves">
      <Shelf eyebrow="SUGGESTED" title="Similar titles" items={related} />
      <Shelf eyebrow={item.studioName || 'STUDIO'} title="More from this studio" items={studioMore} />
    </div>
    <MediaSettings mediaItemId={item.id} canDelete={isAdmin} onDeleted={() => { setSettingsOpen(false); navigate('/library'); }} open={settingsOpen} onOpenChange={setSettingsOpen} onChanged={() => getMediaItem(id).then(setItem).catch(() => {})} />
  </div>;
}
