import React, { useEffect, useMemo, useState } from 'react';
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom';
import { Globe, Heart, Bell, BellRing, Play, SearchX, Settings2, Shuffle } from 'lucide-react';
import { splitBio } from '../../bioFacts';
import { useToast } from '../../ui';
import { getCategories, getPerformer, getSeriesItem, getStudio, performerImageUrl, setPerformerFavorite, setPerformerNotify, getRelatedPerformers } from '../../api';
import { useLibrary } from '../../libraryContext';
import SeriesCover from '../../components/SeriesCover';
import { FilterSelect } from '../../components/FilterControls';
import { useScrollRestoration } from '../../hooks/useScrollRestoration';
import { useColumnCount, useInfiniteList, VirtualWindow } from '../../virtual';
import { cardMetrics, useCardDensity, useCardStyle } from '../../cardStyle';
import { editInAdmin } from '../../appLinks';
import { useMediaList, displayTitle, toneClass, formatRuntime, Artwork, MediaCard, Shelf, HERO_SLIDE_COUNT, HeroCarousel, Status, plural, useStudioNames, PlayButtons, mediaQuery, WATCHED_FILTERS, dimensionTerms, dimensions, DirectoryMark, countLabel, hashString } from './shared';

// Studio / rating / sort filters shared by the Library and the performer / studio / category / series pages.
export function TitleFilters({ studios, studio, onStudio, sort, onSort, rating, onRating, watched, onWatched, kind, onKind, seriesOrder = false, performerOrder = false }) {
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

// Deterministic per-performer palette: same performer always gets the same backdrop.
export function performerTheme(entity) {
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
export function statLabels(meta = {}) {
  return ['height', 'birthplace', 'nationality'].filter(key => meta?.[key])
    .concat(meta?.birthdate ? ['born', 'birthdate', 'date of birth', 'birthday', 'age'] : []);
}

// "Label: value" lines in a bio become a compact fact grid; the rest stays prose.
export function ExpandableBio({ text, skip }) {
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

export function ageFrom(value) {
  const born = new Date(value);
  if (Number.isNaN(born.getTime())) return null;
  const now = new Date();
  let age = now.getFullYear() - born.getFullYear();
  if (now < new Date(now.getFullYear(), born.getMonth(), born.getDate())) age -= 1;
  return age >= 0 && age < 120 ? age : null;
}

export function SeriesHero({ entity, name, countLabel, onSettings }) {
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

// "Start here" on a person's page: the title you play most and the newest one you have not watched.
export function StartHere({ titles, name }) {
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
  const [cardDensity] = useCardDensity();
  const cards = cardMetrics(cardStyle, cardDensity);
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
  const editEntity = () => editInAdmin(type, entity.id);
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
        {isAdmin && <button type="button" className="rs-button rs-button-outline" onClick={editEntity}><Settings2 size={17} /> Edit</button>}
      </div>}
      {entity && total > 0 && <dl className="rs-person-stats" aria-label="Your watching">
        <div><dt>Watched</dt><dd>{watchedTotal.toLocaleString()} of {total.toLocaleString()}</dd></div>
        <div><dt>Your plays</dt><dd>{insights.plays.toLocaleString()}</dd></div>
        <div><dt>Favorites</dt><dd>{favoriteTotal.toLocaleString()}</dd></div>
        <div><dt>Your avg rating</dt><dd>{insights.avgRating ? `★ ${insights.avgRating.toFixed(1)}` : '—'}</dd></div>
      </dl>}
    </header> : type === 'series' ? <SeriesHero entity={entity} name={name} countLabel={collectionLabel} onSettings={isAdmin && entity ? editEntity : null} /> : <header className={`rs-page-header rs-entity-header${portrait ? ' rs-entity-hero' : ''}${type === 'studios' ? ' rs-studio-hero' : ''}`}>
      {type === 'studios' && entity && <span className={`rs-studio-hero-logo${entity.logoImageId ? '' : ' is-mono'}`}><DirectoryMark item={entity} index={0} type="studios" /></span>}
      {portrait && entity && <span className="rs-entity-portrait"><DirectoryMark item={entity} index={0} type="performers" portrait /></span>}
      <div><p className="rs-eyebrow">{config.singular.toUpperCase()} / YOUR ARCHIVE</p><h1>{name || 'Loading…'}</h1><p>{collectionLabel}</p>
        {type === 'performers' && entity && (entity.notes || entity.metadata?.bio) && <p className="rs-performer-bio rs-entity-description">{entity.notes || entity.metadata.bio}</p>}
        {type === 'performers' && entity && <div className="rs-detail-actions">
          <button type="button" className={`rs-button rs-button-outline ${entity.favorite ? 'is-favorite' : ''}`} aria-pressed={!!entity.favorite} onClick={toggleFavorite}><Heart size={17} fill={entity.favorite ? 'currentColor' : 'none'} /> {entity.favorite ? 'Favorited' : 'Favorite'}</button>
          {isAdmin && <button type="button" className="rs-button rs-button-outline" onClick={editEntity}><Settings2 size={17} /> Edit</button>}
        </div>}
        {type === 'studios' && entity?.website && <a className="rs-studio-website" href={/^https?:\/\//i.test(entity.website) ? entity.website : `https://${entity.website}`} target="_blank" rel="noopener noreferrer"><Globe size={15} /> {entity.website.replace(/^https?:\/\//i, '').replace(/\/$/, '')}</a>}
        {type === 'studios' && entity?.description && <p className="rs-entity-description">{entity.description}</p>}
      </div>
      {isAdmin && type === 'studios' && entity && <div className="rs-studio-hero-actions"><button type="button" className="rs-button rs-button-outline" onClick={editEntity}><Settings2 size={17} /> Edit</button></div>}
    </header>}
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
