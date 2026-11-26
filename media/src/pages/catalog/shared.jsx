import React, { useEffect, useRef, useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import { AlertTriangle, ArrowLeft, ArrowRight, BookmarkCheck, BookmarkPlus, Check, Film, Heart, Play, RefreshCw, Search, Shuffle, Star } from 'lucide-react';
import { monogram } from '../../components/Monogram';
import { useToast } from '../../ui';
import { addWatchLater, artworkUrl, previewUrl, removeWatchLater, getCategories, getMediaItems, getPerformers, getSeries, getStudios, performerImageUrl, studioImageUrl } from '../../api';
import { useCardStyle } from '../../cardStyle';
import { fetchRandomTitles, playAll } from '../../queueActions';

export function useMediaList(params) {
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

export function displayTitle(item) {
  const title = item?.title || 'Untitled video';
  const clean = item?.studioName && title.toLowerCase().startsWith(`${item.studioName.toLowerCase()}: `)
    ? title.slice(item.studioName.length + 2) : title;
  return clean.replace(/\s*\([^()]{35,}\)\s*$/, '').trim() || title;
}

// Every title, person and studio gets one of eight "Spectrum" tones from its id, so a grid reads as
// varied colour instead of a grey wall, and the same thing always wears the same colour.
export const TONE_COUNT = 8;
export const toneOf = key => hashString(String(key)) % TONE_COUNT;
export const toneClass = key => `rs-tone-${toneOf(key)}`;

export function formatRuntime(seconds) {
  if (!seconds || seconds < 60) return '';
  const minutes = Math.round(seconds / 60);
  return minutes >= 60 ? `${Math.floor(minutes / 60)}h ${String(minutes % 60).padStart(2, '0')}m` : `${minutes}m`;
}

export const NEW_FOR_MS = 14 * 24 * 3600 * 1000;
export const isNew = item => Boolean(item.dateAdded) && Date.now() - new Date(item.dateAdded).getTime() < NEW_FOR_MS;

// Up to two initials for poster fallbacks ("After Hours" -> "AH", "scene-100" -> "S1").
export function titleInitials(title) {
  const words = String(title).replace(/[^\p{L}\p{N}\s-]/gu, '').split(/[\s-]+/).filter(Boolean);
  return (words.length > 1 ? words[0][0] + words[1][0] : (words[0] || '?').slice(0, 2)).toUpperCase();
}

export function artSources(item, preferBackdrop, style) {
  const thumb = artworkUrl(item.id, 'thumbnail', item.posterImageId);
  const poster = item.posterImageId ? artworkUrl(item.id, 'poster', item.posterImageId) : null;
  const sources = [];
  if (preferBackdrop && item.backdropImageId) sources.push(artworkUrl(item.id, 'backdrop', item.backdropImageId));
  if (style === 'thumbnail' && !preferBackdrop) sources.push(thumb, poster);
  else sources.push(poster, thumb);
  return [...new Set(sources.filter(Boolean))];
}

// Artwork loads lazily: grids and shelves hold hundreds of cards, most of them off screen.
// The hero passes eager so the first slide is not held back.
export function Artwork({ item, className = '', preferBackdrop = false, eager = false }) {
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
    <img key={src} ref={reveal} src={src} alt="" decoding="async" loading={eager ? 'eager' : 'lazy'} className={loadedSrc === src ? 'is-loaded' : ''} onLoad={event => { if (event.currentTarget.naturalWidth > 0) setLoadedSrc(src); }} onError={() => { setLoadedSrc(''); if (step < sources.length - 1) setStep(next => next + 1); }} />
  </div>;
}

// A short silent clip that plays while the pointer rests on a card (mouse devices only).
export const canHover = () => typeof window !== 'undefined' && !!window.matchMedia?.('(hover: hover)').matches;

export function CardPreview({ item }) {
  const [failed, setFailed] = useState(false);
  if (failed) return null;
  return <video className="rs-card-preview" ref={el => { if (el) el.muted = true; }} src={previewUrl(item.id)} muted loop autoPlay playsInline preload="auto" aria-hidden="true" tabIndex={-1} onError={() => setFailed(true)} />;
}

export const ratingFromStars = stars => stars * 2;

// Where the title page's back link returns to, and what it's called there.
export const BACK_LABELS = { '/library': 'Library', '/favorites': 'Favorites', '/watch-later': 'Watch later', '/search': 'Search', '/performers': 'Performers', '/studios': 'Studios', '/series': 'Series', '/categories': 'Categories', '/playlists': 'Playlists', '/activity': 'Activity', '/movies': 'Movies', '/shows': 'TV shows', '/directors': 'Directors', '/': 'Home' };
export function backLabel(from) {
  const path = from.split('?')[0];
  if (BACK_LABELS[path]) return BACK_LABELS[path];
  const detail = path.match(/^\/(performers|studios|series|categories)\/[^/]+$/);
  return detail ? { performers: 'Performer', studios: 'Studio', series: 'Series', categories: 'Category' }[detail[1]] : 'Back';
}

export function MediaCard({ item, exclude = [], kicker, showPlayCount = false, mostPlayed = false, wide = false }) {
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
export function Shelf({ title, eyebrow, href, hrefLabel = 'View all', items, tone, limit = 12, className = '', renderItem = item => <MediaCard key={item.id} item={item} /> }) {
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

export const HERO_AUTOPLAY_MS = 7000;
export const HERO_SLIDE_COUNT = 10;

export function HeroCarousel({ items, eyebrow = 'FROM YOUR COLLECTION', exclude = [], bleed = false }) {
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
        <Artwork item={item} className="rs-feature-art" preferBackdrop eager />
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
export function HeroSaveButton({ item, tabIndex }) {
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

export function Status({ loading, error, empty, retry, title, message, href, action, icon: Icon = Film }) {
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

export function greetingFor(date = new Date()) {
  const hour = date.getHours();
  return hour < 5 ? 'Up late' : hour < 12 ? 'Good morning' : hour < 17 ? 'Good afternoon' : hour < 22 ? 'Good evening' : 'Up late';
}

export const plural = (count, one, many = `${one}s`) => `${count.toLocaleString()} ${count === 1 ? one : many}`;

// Studio names for the "Filter by studio" dropdown (paged in 200 at a time).
export function useStudioNames(enabled = true) {
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

// Play / Shuffle for whatever list is on screen: fills the queue from the same filters and opens the first title.
export function PlayButtons({ filters, ordered = false, shuffleLabel = 'Shuffle' }) {
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

export function mediaQuery(filters) {
  return ({ limit, offset }) => getMediaItems({ ...filters, limit, offset });
}

export const WATCHED_FILTERS = [{ value: 'any', label: 'Any' }, { value: 'unwatched', label: 'Not watched' }, { value: 'inProgress', label: 'In progress' }, { value: 'watched', label: 'Watched' }];
// The page names in the words of the library's mode (Performers vs Cast & crew, ...).
export function dimensionTerms(type, terms) {
  return {
    performers: { title: terms.people, singular: terms.person },
    studios: { title: terms.studios, singular: terms.studio },
    series: { title: terms.seriesPlural, singular: terms.series },
    categories: { title: terms.categories, singular: terms.category },
  }[type];
}

export function cleanParams(params) {
  return Object.fromEntries(Object.entries(params).filter(([, value]) => value !== '' && value != null));
}

export const dimensions = {
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

export function DirectoryMark({ item, index, type, portrait = false }) {
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

export function countLabel(count) {
  return `${count} ${count === 1 ? 'title' : 'titles'}`;
}

export function PerformerCard({ item, index, note, below = false }) {
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

export function hashString(value) {
  let h = 2166136261;
  for (const ch of String(value)) { h ^= ch.codePointAt(0); h = Math.imul(h, 16777619); }
  return h >>> 0;
}
