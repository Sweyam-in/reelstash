import React, { useEffect, useRef, useState } from 'react';
import { flushSync } from 'react-dom';
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom';
import { ArrowLeft, ArrowRight, Bookmark, BookmarkCheck, Check, Ellipsis, Heart, Keyboard, ListPlus, ListVideo, Play, Plus, Settings2, Star } from 'lucide-react';
import { monogram } from '../../components/Monogram';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger, useToast } from '../../ui';
import { addWatchLater, artworkUrl, createMarker, getMarkers, removeWatchLater, getNextInSeries, subtitleUrl, startWatchSession, updateWatchSession, getMediaItem, getRelatedMedia, getMediaItems, performerImageUrl, studioImageUrl, updateMediaItem, updatePlayback } from '../../api';
import { useLibrary } from '../../libraryContext';
import { useAdaptiveSource, TRANSCODE_QUALITIES } from '../hooks/useAdaptiveSource';
import { useMediaTracks } from '../hooks/useMediaTracks';
import UpNext from '../../components/UpNext';
import PlayerControls from '../../components/PlayerControls';
import Markers from '../../components/Markers';
import QueuePanel from '../../components/QueuePanel';
import AddToPlaylist from '../../components/AddToPlaylist';
import PlayerShortcuts from '../../components/PlayerShortcuts';
import { isFullscreen as isPlayerFullscreen, onFullscreenChange, toggleFullscreen } from '../../fullscreen';
import { enqueue, nextInQueue, previousInQueue, setAutoplay, startQueue, syncPlaying, useQueue } from '../../queue';
import { editInAdmin } from '../../appLinks';
import { formatClock } from '../../playerUtils';
import { displayTitle, toneClass, formatRuntime, Artwork, ratingFromStars, Shelf, Status, countLabel } from './shared';

// A 5-star rating control. Ratings are stored server-side on a 0-10 scale (half-star precision
// isn't exposed in the UI), so callers convert with starsFromRating / ratingFromStars.
export const starsFromRating = rating => rating ? Math.round(rating / 2) : 0;
export function StarRating({ value = 0, onChange, size = 16 }) {
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

// "Part of <series>" on the video page: the series in order beside the player, with the current
// title marked and the next one called out.
export function SeriesSideList({ item, nextEpisode }) {
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
export function SimilarSideList({ items }) {
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

export const bestFile = item => (item.files || []).filter(file => file.available).sort((a, b) => (b.height || 0) - (a.height || 0))[0] || (item.files || [])[0];
export function qualityLabel(file) {
  if (!file?.width || !file?.height) return '';
  const short = Math.min(file.width, file.height);
  const long = Math.max(file.width, file.height);
  return long >= 3800 || short >= 2100 ? '4K' : short >= 1000 ? '1080p' : short >= 700 ? '720p' : 'SD';
}
export const formatBytes = bytes => { if (!bytes) return ''; const units = ['B', 'KB', 'MB', 'GB', 'TB']; let n = bytes; let i = 0; while (n >= 1024 && i < units.length - 1) { n /= 1024; i += 1; } return `${n.toFixed(n >= 10 || i === 0 ? 0 : 1)} ${units[i]}`; };
export const formatBitrate = bits => bits ? (bits >= 1e6 ? `${(bits / 1e6).toFixed(1)} Mbps` : `${Math.round(bits / 1e3)} kbps`) : '';

// The file the player opens: resolution, codecs, size and where it lives.
// Plain-language names for what the file is: picture quality, video and sound formats.
export function resolutionName(file) {
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
export const VIDEO_NAMES = { h264: 'H.264', avc: 'H.264', avc1: 'H.264', hevc: 'HEVC (H.265)', h265: 'HEVC (H.265)', av1: 'AV1', vp9: 'VP9', vp8: 'VP8', mpeg4: 'MPEG-4', mpeg2video: 'MPEG-2', vc1: 'VC-1', wmv3: 'WMV' };
export const AUDIO_NAMES = { aac: 'AAC', ac3: 'Dolby Digital', eac3: 'Dolby Digital Plus', truehd: 'Dolby TrueHD', dts: 'DTS', 'dts-hd': 'DTS-HD', mp3: 'MP3', opus: 'Opus', flac: 'FLAC', vorbis: 'Vorbis', pcm_s16le: 'PCM', alac: 'ALAC' };
export const codecName = (table, codec) => (codec ? table[String(codec).toLowerCase()] || String(codec).toUpperCase() : '');
export const isDolby = codec => /^(ac3|eac3|truehd)$/i.test(codec || '');

// The file the player opens, in words a viewer understands: quality, video and sound formats, size.
export function TechnicalCard({ item }) {
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

export function CreditImage({ src, studio }) {
  const [failed, setFailed] = useState(false);
  if (!src || failed) return null;
  return <img className={studio ? 'is-studio' : ''} src={src} alt="" loading="lazy" decoding="async" onError={() => setFailed(true)} />;
}

export function DetailFacts({ item }) {
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
  const [shortcutsOpen, setShortcutsOpen] = useState(false);
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
    if (event.key === '?') { setShortcutsOpen(true); event.preventDefault(); return; }
    if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
      video.muted = false;
      video.volume = Math.min(1, Math.max(0, Math.round((video.volume + (event.key === 'ArrowUp' ? 0.05 : -0.05)) * 100) / 100));
      event.preventDefault();
      return;
    }
    if (event.key === 'Home') { video.currentTime = 0; event.preventDefault(); return; }
    if (event.key === 'End' && Number.isFinite(video.duration)) { video.currentTime = Math.max(0, video.duration - 0.5); event.preventDefault(); return; }
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
            <DropdownMenu><DropdownMenuTrigger asChild><button type="button" className="rs-button rs-button-outline rs-button-lg rs-button-icon rs-detail-more" aria-label="More"><Ellipsis size={18} /></button></DropdownMenuTrigger><DropdownMenuContent align="end"><DropdownMenuItem onSelect={addToQueue}><ListPlus size={16} /> Add to queue</DropdownMenuItem><DropdownMenuItem onSelect={() => setPlaylistOpen(true)}><ListVideo size={16} /> Add to playlist</DropdownMenuItem><DropdownMenuItem onSelect={markWatched}><Check size={16} /> Mark as watched</DropdownMenuItem><DropdownMenuItem onSelect={() => setShortcutsOpen(true)}><Keyboard size={16} /> Keyboard shortcuts</DropdownMenuItem>{isAdmin && <DropdownMenuItem onSelect={() => editInAdmin('media', item.id)}><Settings2 size={16} /> Edit in Admin</DropdownMenuItem>}</DropdownMenuContent></DropdownMenu>
            <AddToPlaylist mediaItemId={item.id} open={playlistOpen} onOpenChange={setPlaylistOpen} />
            <PlayerShortcuts open={shortcutsOpen} onOpenChange={setShortcutsOpen} />
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
  </div>;
}
