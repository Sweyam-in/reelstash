import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { Check, ListVideo, Play, RotateCcw, Settings2, Shuffle, Star } from 'lucide-react';
import { useToast } from '@spatika/react';
import { artworkUrl, getMediaItems, getSeriesItem, performerImageUrl, seriesBackdropUrl, studioImageUrl } from '../api';
import { monogram } from '../components/Monogram';
import SeriesCover from '../components/SeriesCover';
import SeriesSettings from './SeriesSettings';
import { useLibrary } from '../libraryContext';
import { playAll } from '../queueActions';

// A series page: the key art of its titles as a backdrop, a hero with the way in (play / resume / shuffle),
// progress through the series, then every title in order as a list with its own synopsis and cast.

const plural = (count, one) => `${count.toLocaleString()} ${count === 1 ? one : `${one}s`}`;
const duration = seconds => {
  if (!seconds || seconds < 60) return '';
  const minutes = Math.round(seconds / 60);
  return minutes >= 60 ? `${Math.floor(minutes / 60)}h ${String(minutes % 60).padStart(2, '0')}m` : `${minutes}m`;
};

function PartRow({ item, index, from }) {
  const [imageOk, setImageOk] = useState(true);
  const progress = item.positionSeconds && item.durationSeconds ? Math.min(100, item.positionSeconds / item.durationSeconds * 100) : 0;
  const number = item.seriesPosition ?? index + 1;
  return <li>
    <Link to={`/library/${item.id}`} state={{ from }} className={`rs-episode-row rs-series-row${item.watched ? ' is-watched' : ''}`}>
      <span className="rs-series-row-no">{String(number).padStart(2, '0')}</span>
      <span className="rs-episode-thumb">
        {imageOk ? <img src={artworkUrl(item.id, 'thumbnail', item.posterImageId)} alt="" loading="lazy" onError={() => setImageOk(false)} /> : <ListVideo size={22} />}
        <span className="rs-series-row-play" aria-hidden="true"><Play size={18} fill="currentColor" /></span>
        {progress > 0 && <i style={{ width: `${progress}%` }} />}
      </span>
      <span className="rs-episode-copy">
        <small>{[item.watched ? 'Watched' : progress > 0 ? 'In progress' : null, item.studioName].filter(Boolean).join(' · ') || `Part ${number}`}</small>
        <strong>{item.title}</strong>
        {item.description && <p>{item.description}</p>}
        {!!item.cast?.length && <span className="rs-series-row-cast" aria-label={`Starring ${item.cast.map(person => person.name).join(', ')}`}><em>Starring</em><span className="rs-sd-group">{item.cast.slice(0, 5).map(person => <Avatar key={person.id} person={person} size="sm" />)}{item.cast.length > 5 && <span className="rs-sd-more" title={item.cast.slice(5).map(person => person.name).join(', ')}>+{item.cast.length - 5}</span>}</span></span>}
      </span>
      <span className="rs-episode-meta">{item.watched && <Check size={14} />}<Rating rating={item.rating} count={item.ratingCount} />{duration(item.durationSeconds)}</span>
    </Link>
  </li>;
}

const stars = rating => (rating / 2).toFixed(1);

function Avatar({ person, size = 'sm' }) {
  const [failed, setFailed] = useState(false);
  const mono = monogram(person.name);
  return <span className={`rs-sd-avatar is-${size}`} title={person.name}>
    {person.profileImageId && !failed
      ? <img src={performerImageUrl(person.id, person.profileImageId)} alt="" loading="lazy" onError={() => setFailed(true)} />
      : <span className="rs-mono" style={mono.style} aria-hidden="true">{mono.initials}</span>}
  </span>;
}

function Rating({ rating, count, size = 15 }) {
  if (!rating) return null;
  return <span className="rs-sd-rating" title={count ? `${count} ${count === 1 ? 'rating' : 'ratings'}` : undefined}><Star size={size} fill="currentColor" />{stars(rating)}</span>;
}

export default function SeriesDetail() {
  const { name: id } = useParams();
  const navigate = useNavigate();
  const { toast } = useToast();
  const { isAdmin } = useLibrary();
  const [series, setSeries] = useState(null);
  const [error, setError] = useState('');
  const [items, setItems] = useState(null);
  const [season, setSeason] = useState('all');
  const [settingsOpen, setSettingsOpen] = useState(false);

  const loadSeries = useCallback(() => getSeriesItem(id).then(setSeries).catch(err => setError(err.status === 404 ? 'This series isn’t in this library.' : err.message)), [id]);
  const loadItems = useCallback(() => getMediaItems({ seriesId: id, sort: 'series', limit: 200 }).then(result => setItems(result.items || [])).catch(() => setItems([])), [id]);
  useEffect(() => { setSeries(null); setItems(null); setError(''); setSeason('all'); loadSeries(); loadItems(); }, [id, loadSeries, loadItems]);

  const seasons = series?.seasons || [];
  const shown = useMemo(() => !items ? [] : season === 'all' ? items : items.filter(item => item.seasonNumber === season), [items, season]);
  const stats = useMemo(() => {
    const all = items || [];
    const watched = all.filter(item => item.watched).length;
    const runtime = all.reduce((sum, item) => sum + (item.durationSeconds || 0), 0);
    const cast = new Map();
    const cats = new Map();
    all.forEach(item => {
      (item.cast || []).forEach(person => cast.set(person.id, { person, count: (cast.get(person.id)?.count || 0) + 1 }));
      (item.categories || []).forEach(name => cats.set(name, (cats.get(name) || 0) + 1));
    });
    const rated = all.filter(item => item.rating > 0);
    const rating = rated.length ? rated.reduce((sum, item) => sum + item.rating, 0) / rated.length : 0;
    const logo = all.find(item => item.studioLogoImageId);
    const resume = all.find(item => item.positionSeconds > 0 && !item.watched);
    const next = resume || all.find(item => !item.watched) || null;
    const backdrop = all.find(item => item.backdropImageId) || all.find(item => item.posterImageId) || all[0];
    return { watched, runtime, cast: [...cast.values()].sort((a, b) => b.count - a.count), categories: [...cats.entries()].sort((a, b) => b[1] - a[1]).map(([name]) => name).slice(0, 10), rating, rated: rated.length, logo, resume, next, backdrop };
  }, [items]);

  if (error) return <div className="rs-page"><p className="rs-show-error">{error}</p></div>;
  if (!series) return <div className="rs-page"><p className="rs-virtual-status">Loading…</p></div>;

  const total = items?.length ?? series.mediaCount ?? 0;
  const allWatched = total > 0 && stats.watched === total;
  const backdropSrc = series.backdropImageId ? seriesBackdropUrl(series.id, series.backdropImageId) : stats.backdrop ? (stats.backdrop.backdropImageId ? artworkUrl(stats.backdrop.id, 'backdrop', stats.backdrop.backdropImageId) : artworkUrl(stats.backdrop.id, 'thumbnail', stats.backdrop.posterImageId)) : null;
  const facts = [series.releaseDate?.slice(0, 4), series.studioName, plural(total, 'title'), duration(stats.runtime)].filter(Boolean);

  function play(list, options) {
    if (!playAll(list, { navigate, ...options })) toast({ title: 'Nothing to play here yet' });
  }
  function playFrom(start) {
    const at = (items || []).findIndex(item => item.id === start.id);
    play((items || []).slice(Math.max(0, at)));
  }
  const primary = allWatched ? { label: 'Watch again', icon: RotateCcw, run: () => play(items) } : stats.next ? { label: stats.resume ? 'Resume' : stats.watched ? 'Continue' : 'Play', icon: Play, run: () => playFrom(stats.next) } : null;

  return <div className="rs-series-page">
    <header className="rs-series-banner">
      {backdropSrc && <div className={`rs-series-backdrop${series.backdropImageId ? ' is-uploaded' : ''}`} style={{ backgroundImage: `url("${backdropSrc}")` }} aria-hidden="true" />}
      <div className="rs-series-banner-body">
        <span className="rs-series-banner-poster"><SeriesCover series={series} /></span>
        <div className="rs-series-banner-copy">
          <p className="rs-eyebrow">Series</p>
          <h1>{series.name}</h1>
          <p className="rs-show-meta">{facts.join('  ·  ')}</p>
          {stats.rating > 0 && <p className="rs-sd-banner-rating"><Rating rating={stats.rating} size={17} /><small>average of {stats.rated} rated {stats.rated === 1 ? 'title' : 'titles'}</small></p>}
          {total > 0 && <div className="rs-series-progress" aria-label={`${stats.watched} of ${total} watched`}><span><i style={{ width: `${stats.watched / total * 100}%` }} /></span><small>{stats.watched} of {total} watched</small></div>}
          <div className="rs-detail-actions">
            {primary && <button type="button" className="rs-button rs-button-primary" onClick={primary.run}><primary.icon size={17} fill={primary.icon === Play ? 'currentColor' : 'none'} /> {primary.label}{stats.next && !allWatched ? <small className="rs-series-primary-note">{stats.next.seriesPosition != null ? `No. ${stats.next.seriesPosition}` : ''}</small> : null}</button>}
            {total > 1 && <button type="button" className="rs-button rs-button-outline" onClick={() => play(items, { shuffle: true })}><Shuffle size={16} /> Shuffle</button>}
            {isAdmin && <button type="button" className="rs-button rs-button-outline" onClick={() => setSettingsOpen(true)}><Settings2 size={17} /> Settings</button>}
          </div>
        </div>
      </div>
    </header>

    <div className="rs-series-content">
    {seasons.length > 1 && <nav className="rs-season-tabs" aria-label="Seasons">
      <button type="button" className={season === 'all' ? 'is-active' : ''} aria-pressed={season === 'all'} onClick={() => setSeason('all')}>All<small>{total}</small></button>
      {seasons.map(s => <button type="button" key={s.number} className={season === s.number ? 'is-active' : ''} aria-pressed={season === s.number} onClick={() => setSeason(s.number)}>{s.number === 0 ? 'Specials' : `Season ${s.number}`}<small>{s.episodeCount}</small></button>)}
    </nav>}
    {seasons.length <= 1 && total > 0 && <h2 className="rs-season-heading">In this series</h2>}

    <div className="rs-sd-layout">
    <div className="rs-sd-main">
    {items === null && <p className="rs-virtual-status">Loading titles…</p>}
    {items?.length === 0 && <p className="rs-virtual-status">{isAdmin ? 'No videos yet. Open Settings to add videos, or match the series with a provider and pair its scenes.' : 'No videos here yet.'}</p>}
    {!!shown.length && <ol className="rs-episode-list rs-series-list">{shown.map((item, index) => <PartRow key={item.id} item={item} index={index} from={`/series/${id}`} />)}</ol>}
    </div>
    <aside className="rs-sd-aside" aria-label="About this series">
      <section>
        <h3>About</h3>
        <p className="rs-sd-desc">{series.description || 'No description yet.'}</p>
        {stats.rating > 0 && <div className="rs-sd-aside-rating"><Rating rating={stats.rating} size={16} /><span>of 5 · {stats.rated} rated</span></div>}
      </section>
      <section>
        <h3>Details</h3>
        <dl className="rs-sd-facts">
          {series.studioName && <div><dt>Studio</dt><dd>{series.studioId ? <Link to={`/studios/${series.studioId}`} className="rs-sd-studio">{stats.logo && <img src={studioImageUrl(series.studioId, stats.logo.studioLogoImageId)} alt="" />}{series.studioName}</Link> : series.studioName}</dd></div>}
          {series.releaseDate && <div><dt>Released</dt><dd>{series.releaseDate.slice(0, 4)}</dd></div>}
          <div><dt>Titles</dt><dd>{total}</dd></div>
          {!!stats.runtime && <div><dt>Runtime</dt><dd>{duration(stats.runtime)}</dd></div>}
          {seasons.length > 1 && <div><dt>Seasons</dt><dd>{seasons.length}</dd></div>}
          <div><dt>Watched</dt><dd>{stats.watched} of {total}</dd></div>
        </dl>
      </section>
      {!!stats.cast.length && <section>
        <h3>Starring<span>{stats.cast.length}</span></h3>
        <ul className="rs-sd-cast">{stats.cast.slice(0, 12).map(({ person, count }) => <li key={person.id}><Link to={`/performers/${person.id}`}><Avatar person={person} size="md" /><span><strong>{person.name}</strong><small>{count} {count === 1 ? 'title' : 'titles'}</small></span></Link></li>)}</ul>
      </section>}
      {!!stats.categories.length && <section>
        <h3>Categories</h3>
        <div className="rs-sd-tags">{stats.categories.map(name => <span key={name}>{name}</span>)}</div>
      </section>}
    </aside>
    </div>

    </div>
    <SeriesSettings seriesId={series.id} open={settingsOpen} onOpenChange={setSettingsOpen} onChanged={() => { loadSeries(); loadItems(); }} onDeleted={() => navigate('/series')} />
  </div>;
}
