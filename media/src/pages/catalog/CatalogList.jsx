import React, { useEffect, useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { ArrowRight, Search, SearchX } from 'lucide-react';
import { Tabs, TabsList, TabsTrigger } from '../../ui';
import { getCategories, getPerformers } from '../../api';
import { useLibrary } from '../../libraryContext';
import { useDebounce } from '../hooks/useDebounce';
import { FilterChip, FilterPanel, FilterSelect } from '../../components/FilterControls';
import { usePersistedState } from '../../hooks/usePersistedState';
import { useScrollRestoration } from '../../hooks/useScrollRestoration';
import { useColumnCount, useInfiniteList, VirtualWindow } from '../../virtual';
import { cardMetrics, useCardDensity, useCardStyle } from '../../cardStyle';
import { ratingFromStars, MediaCard, Status, useStudioNames, PlayButtons, mediaQuery, WATCHED_FILTERS, PerformerCard } from './shared';

export function SearchPerformers({ query, favoritesOnly = false, full = false, onMore }) {
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

export const DURATION_FILTERS = [
  { value: 'any', label: 'Any length' },
  { value: 'u15', label: 'Under 15 min', params: { maxDuration: 900 } },
  { value: 'u30', label: 'Under 30 min', params: { maxDuration: 1800 } },
  { value: '30-60', label: '30–60 min', params: { minDuration: 1800, maxDuration: 3600 } },
  { value: 'o60', label: 'Over 1 hour', params: { minDuration: 3600 } },
];
export const QUALITY_FILTERS = [{ value: 'any', label: 'Any quality' }, { value: '4k', label: '4K' }, { value: '1080p', label: '1080p' }, { value: '720p', label: '720p' }, { value: 'sd', label: 'SD' }];
export const ADDED_FILTERS = [{ value: 'any', label: 'Any time' }, { value: '1', label: 'Today' }, { value: '7', label: 'This week' }, { value: '30', label: 'This month' }, { value: '90', label: 'Last 3 months' }];
export const SORT_OPTIONS = [
  { value: 'recent', label: 'Recently added' }, { value: 'oldest', label: 'Oldest first' }, { value: 'title', label: 'Title A–Z' },
  { value: 'watched', label: 'Most played' }, { value: 'rated', label: 'Highest rated' },
];

// People and categories for the Library's filter pills (top people by titles; type to search the rest).
export function usePeopleOptions(enabled) {
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

export function useCategoryOptions(enabled) {
  const [items, setItems] = useState([]);
  useEffect(() => {
    if (!enabled) return undefined;
    let live = true;
    getCategories().then(result => { if (live) setItems((result.items || []).filter(c => c.mediaCount > 0).sort((a, b) => b.mediaCount - a.mediaCount)); }).catch(() => {});
    return () => { live = false; };
  }, [enabled]);
  return items;
}

export const SCOPES = [['all', 'Everything'], ['videos', 'Videos'], ['reels', 'Reels'], ['unwatched', 'Not watched'], ['inProgress', 'In progress']];

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
  const [cardDensity] = useCardDensity();
  const cards = cardMetrics(cardStyle, cardDensity);
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
