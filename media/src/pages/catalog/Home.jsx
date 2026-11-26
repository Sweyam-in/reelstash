import React, { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { ArrowRight, ArrowUpRight, Building2, Layers3, Users, Heart, ListVideo, Play } from 'lucide-react';
import { getCategories, getContinueWatching, getNextUp, getHomeRecommendations, getHomeSeries, getHomeSummary, getPerformers, getSeries, getStudios } from '../../api';
import { useLibrary } from '../../libraryContext';
import { nextUpKicker, resumeKicker } from '../../trackPrefs';
import { adminHref } from '../../appLinks';
import { useMediaList, displayTitle, toneClass, formatRuntime, isNew, Artwork, MediaCard, Shelf, HERO_SLIDE_COUNT, HeroCarousel, Status, plural, DirectoryMark, countLabel, hashString } from './shared';

// Deterministic per-day pick of `count` items from `pool`, seeded by `seedKey` (e.g. today's date)
// so every visitor sees the same slideshow order and it only changes once a day.
export function dailySelection(pool, count, seedKey) {
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

// One of the four big "Explore" tiles on Home, with a peek of what's inside.
export function ExploreTile({ to, tone, icon: Icon, label, count, unit, units, peek, chips }) {
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

export function FaceCard({ person }) {
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

export const hoursLabel = seconds => {
  const hours = seconds / 3600;
  if (hours >= 10) return `${Math.round(hours).toLocaleString()} hours`;
  if (hours >= 1) return `${hours.toFixed(1)} hours`;
  return `${Math.max(1, Math.round(seconds / 60))} min`;
};
export const watchTimeLabel = seconds => {
  const minutes = Math.round(seconds / 60);
  return minutes >= 60 ? `${Math.floor(minutes / 60)}h ${String(minutes % 60).padStart(2, '0')}m` : `${minutes}m`;
};

// "Library at a glance" on Home: four quick numbers, each a door to the page that explains it.
export function LibraryPulse({ summary }) {
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
export function SeriesProgress({ items }) {
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
