import React, { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { LayoutGrid, Rows3, ArrowUpRight, Heart, ListVideo, Search } from 'lucide-react';
import { monogram } from '../../components/Monogram';
import { Tabs, TabsList, TabsTrigger } from '../../ui';
import { getPerformers, performerImageUrl, getWatchActivity } from '../../api';
import { useLibrary } from '../../libraryContext';
import SeriesCover from '../../components/SeriesCover';
import { useDebounce } from '../hooks/useDebounce';
import { FilterSelect } from '../../components/FilterControls';
import { usePersistedState } from '../../hooks/usePersistedState';
import { useScrollRestoration } from '../../hooks/useScrollRestoration';
import { useInfiniteList, VirtualWindow } from '../../virtual';
import { toneClass, Status, dimensionTerms, dimensions, DirectoryMark, countLabel, PerformerCard } from './shared';

export function StudioCard({ item, index }) {
  const count = item.mediaCount || 0;
  return <Link to={`/studios/${item.id}`} className={`rs-studio-card ${toneClass(item.id)}`} aria-label={`${item.name}, ${countLabel(count)}`}>
    <span className="rs-studio-card-logo"><DirectoryMark item={item} index={index} type="studios" /></span>
    <span className="rs-studio-card-copy"><strong>{item.name}</strong><small>{countLabel(count)}</small></span>
  </Link>;
}

export function SeriesCard({ item, base = 'series', view = 'grid' }) {
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

export function NameTile({ item, type }) {
  const count = item.mediaCount || 0;
  return <Link to={`/${type}/${item.id}`} className={`rs-name-tile ${toneClass(`${type}:${item.id}`)}`} aria-label={`${item.name}, ${countLabel(count)}`}>
    <strong>{item.name}</strong>
    <small>{countLabel(count)}</small>
    <ArrowUpRight size={18} />
  </Link>;
}

// Entertainment libraries split two directories: people into cast and directors, and series into
// TV shows and collections of movies.
export const BROWSE_VARIANTS = {
  directors: { params: { role: 'director' }, title: terms => terms.directors, singular: terms => terms.director, blurb: 'Who directed the titles in this library.' },
  shows: { params: { kind: 'show' }, title: terms => terms.shows, singular: terms => terms.show, blurb: 'Every show, with its seasons and episodes.' },
  collections: { params: { kind: 'collection' }, title: terms => terms.seriesPlural, singular: terms => terms.series, blurb: 'Movies that belong together.' },
};

// "You watch most this month": the people behind the titles played most in the last 30 days.
export function MostWatchedStrip() {
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
  const { terms, mode } = useLibrary();
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
    <header className="rs-page-header rs-directory-header"><div className="rs-directory-headrow"><div className="rs-directory-title"><p className="rs-eyebrow">Discover</p><h1>{config.title}<span className="rs-count">{loading && !items.length ? '…' : total.toLocaleString()}</span></h1>{type !== 'performers' && <p>{extra?.blurb || blurbs[type]}</p>}</div><div className="rs-directory-toolbar"><label className="rs-search"><Search size={18} /><input aria-label={`Search ${config.title.toLowerCase()}`} value={query} onChange={e => setQuery(e.target.value)} placeholder={`Search ${config.title.toLowerCase()}`} /></label>{type === 'series' && <div className="rs-view-toggle" role="group" aria-label="View"><button type="button" aria-pressed={view !== 'list'} title="Grid view" onClick={() => setView('grid')}><LayoutGrid size={16} /></button><button type="button" aria-pressed={view === 'list'} title="List view" onClick={() => setView('list')}><Rows3 size={16} /></button></div>}{type === 'categories' ? <button type="button" className="rs-button rs-button-outline" aria-pressed={showEmpty} onClick={() => setShowEmpty(value => !value)}>{showEmpty ? 'Show used only' : 'Show all categories'}</button> : type !== 'performers' && <span>A — Z</span>}{type === 'performers' && <FilterSelect label="Sort" value={sort} onChange={setSort} options={[{ value: 'count', label: 'Most titles' }, { value: 'name', label: 'Name A–Z' }, { value: 'name_desc', label: 'Name Z–A' }]} />}</div></div></header>
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
    <Status loading={loading && !items.length} error={error} empty={!loading && !error && !items.length && !query && !favoriteFilter && !letter} retry={refresh} title={`No ${config.title.toLowerCase()} yet`} message={type === 'series' ? 'Series appear here once titles are grouped into them.' : type === 'categories' ? 'No titles have categories yet.' : 'They appear here as your library is scanned.'} href={type === 'series' || type === 'categories' ? '/library' : undefined} action="Browse titles" />
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
