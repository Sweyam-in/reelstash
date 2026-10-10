import React, { useEffect, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { Play, Tv } from 'lucide-react';
import { artworkUrl, getMediaItems, getSeriesItem } from '../api';
import SeriesCover from '../components/SeriesCover';
import { useLibrary } from '../libraryContext';

// A TV show in an entertainment library: its poster and synopsis, then its episodes one season at
// a time. Episodes are ordinary titles, so each row opens the usual title page to play it.

const minutes = seconds => seconds ? `${Math.round(seconds / 60)} min` : null;

function EpisodeRow({ item, terms }) {
  const [imageOk, setImageOk] = useState(true);
  const progress = item.positionSeconds && item.durationSeconds ? Math.min(100, item.positionSeconds / item.durationSeconds * 100) : 0;
  return <li>
    <Link to={`/library/${item.id}`} className="rs-episode-row">
      <span className="rs-episode-thumb">
        {imageOk ? <img src={artworkUrl(item.id, 'thumbnail', item.posterImageId)} alt="" loading="lazy" onError={() => setImageOk(false)} /> : <Tv size={22} />}
        {progress > 0 && <i style={{ width: `${progress}%` }} />}
      </span>
      <span className="rs-episode-copy">
        <small>{item.episodeNumber != null ? `${terms.episode} ${item.episodeNumber}` : terms.episode}{item.watched ? ' · Watched' : ''}</small>
        <strong>{episodeTitle(item)}</strong>
        {item.description && <p>{item.description}</p>}
      </span>
      <span className="rs-episode-meta">{[item.releaseDate?.slice(0, 10), minutes(item.durationSeconds)].filter(Boolean).join(' · ')}</span>
    </Link>
  </li>;
}

// Provider titles read "Show - S01E02 - Name"; the show and number are already on screen.
function episodeTitle(item) {
  const parts = item.title.split(/\s+-\s+/);
  if (parts.length >= 3 && /^S\d+E\d+$/i.test(parts[1])) return parts.slice(2).join(' - ');
  return item.title;
}

export default function ShowDetail() {
  const { name: id } = useParams();
  const { terms } = useLibrary();
  const navigate = useNavigate();
  const [show, setShow] = useState(null);
  const [error, setError] = useState('');
  const [season, setSeason] = useState(null);
  const [episodes, setEpisodes] = useState(null);

  useEffect(() => {
    let live = true;
    setShow(null);
    getSeriesItem(id).then(result => {
      if (!live) return;
      setShow(result);
      setSeason(result.seasons?.[0]?.number ?? null);
    }).catch(err => { if (live) setError(err.status === 404 ? 'This show isn’t in this library.' : err.message); });
    return () => { live = false; };
  }, [id]);

  useEffect(() => {
    if (!show) return undefined;
    let live = true;
    setEpisodes(null);
    const params = { seriesId: id, sort: 'series', limit: 200, ...(season != null ? { season } : {}) };
    getMediaItems(params).then(result => { if (live) setEpisodes(result.items || []); }).catch(() => { if (live) setEpisodes([]); });
    return () => { live = false; };
  }, [id, show, season]);

  if (error) return <div className="rs-page"><p className="rs-show-error">{error}</p></div>;
  if (!show) return <div className="rs-page"><p className="rs-virtual-status">Loading…</p></div>;
  const seasons = show.seasons || [];
  const firstEpisode = seasons[0]?.firstEpisodeId || show.firstMediaItemId;
  const year = show.releaseDate?.slice(0, 4);

  return <div className="rs-page rs-show">
    <header className="rs-show-hero">
      <span className="rs-show-poster"><SeriesCover series={show} /></span>
      <div className="rs-show-copy">
        <p className="rs-eyebrow">{terms.show}</p>
        <h1>{show.name}</h1>
        <p className="rs-show-meta">{[year, seasons.length ? `${seasons.length} ${seasons.length === 1 ? 'season' : 'seasons'}` : null, `${show.mediaCount} ${show.mediaCount === 1 ? 'episode' : 'episodes'}`, show.studioName].filter(Boolean).join(' · ')}</p>
        {show.description && <p className="rs-show-description">{show.description}</p>}
        {firstEpisode && <div className="rs-detail-actions"><button type="button" className="rs-button rs-button-primary" onClick={() => navigate(`/library/${firstEpisode}`)}><Play size={17} fill="currentColor" /> Play from the start</button></div>}
      </div>
    </header>

    {seasons.length > 1 && <nav className="rs-season-tabs" aria-label={terms.seasons}>
      {seasons.map(s => <button type="button" key={s.number} aria-pressed={season === s.number} className={season === s.number ? 'is-active' : ''} onClick={() => setSeason(s.number)}>
        {s.number === 0 ? 'Specials' : `${terms.season} ${s.number}`}<small>{s.episodeCount}</small>
      </button>)}
    </nav>}
    {seasons.length === 1 && <h2 className="rs-season-heading">{seasons[0].number === 0 ? 'Specials' : `${terms.season} ${seasons[0].number}`}</h2>}

    {episodes === null && <p className="rs-virtual-status">Loading episodes…</p>}
    {episodes?.length === 0 && <p className="rs-virtual-status">No episodes here yet.</p>}
    {!!episodes?.length && <ol className="rs-episode-list">{episodes.map(item => <EpisodeRow key={item.id} item={item} terms={terms} />)}</ol>}
  </div>;
}
