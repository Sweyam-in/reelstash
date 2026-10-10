import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Cast, CheckCheck, Maximize2, Minimize2, Pause, PictureInPicture2, Play, Settings, SkipBack, SkipForward, Volume2, Zap, VolumeX } from 'lucide-react';
import PlayerTimeline from './PlayerTimeline';
import { formatClock } from '../playerUtils';
import { canPictureInPicture, canSetVolume, togglePictureInPicture } from '../fullscreen';

const IDLE_HIDE_MS = 2500;
const DOUBLE_TAP_MS = 260;

/**
 * The player's controls, drawn over the video instead of the browser's own so the seek bar can
 * show hover previews and markers. Works with mouse, touch and keyboard:
 *  - mouse: the bar appears on movement and hides after a rest while playing; click toggles play,
 *    double-click toggles fullscreen
 *  - touch: the first tap while the bar is hidden only reveals it; the next tap toggles play;
 *    double-tapping the left or right side seeks back or forward 10s
 *  - keyboard: handled by the frame (space, arrows, F, M) and by each control
 * `frameRef` is the element that goes fullscreen, so the controls stay usable there.
 */
const SPEEDS = [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2];

// The browser's own track lists (audio and subtitles). Empty when the file has one track or the
// browser does not expose them (Safari and Chrome list text tracks; audio tracks are Safari only).
function useTracks(videoRef, mediaId) {
  const [tracks, setTracks] = useState({ audio: [], text: [] });
  useEffect(() => {
    const video = videoRef.current;
    if (!video) return undefined;
    const read = () => setTracks({
      audio: Array.from(video.audioTracks || []),
      text: Array.from(video.textTracks || []).filter(track => track.kind === 'subtitles' || track.kind === 'captions'),
    });
    const lists = [video.audioTracks, video.textTracks].filter(list => list && typeof list.addEventListener === 'function');
    const events = ['addtrack', 'removetrack', 'change'];
    lists.forEach(list => events.forEach(name => list.addEventListener(name, read)));
    video.addEventListener('loadedmetadata', read);
    read();
    return () => { lists.forEach(list => events.forEach(name => list.removeEventListener(name, read))); video.removeEventListener('loadedmetadata', read); };
  }, [videoRef, mediaId]);
  return tracks;
}

// "2:08 / 11:14", next to the volume like YouTube's.
function PlayerClock({ videoRef, mediaId }) {
  const [clock, setClock] = useState({ current: 0, duration: 0 });
  useEffect(() => {
    const video = videoRef.current;
    if (!video) return undefined;
    const sync = () => setClock({ current: video.currentTime || 0, duration: Number.isFinite(video.duration) ? video.duration : 0 });
    const events = ['timeupdate', 'durationchange', 'loadedmetadata', 'seeked', 'emptied'];
    events.forEach(name => video.addEventListener(name, sync));
    sync();
    return () => events.forEach(name => video.removeEventListener(name, sync));
  }, [videoRef, mediaId]);
  return <span className="rs-controls-clock">{formatClock(clock.current)} / {formatClock(clock.duration)}</span>;
}

const trackLabel = (track, index) => track.label || track.language || `Track ${index + 1}`;

// True while the video has been asked to play but has nothing to show yet: the first frames of a
// title (a slow disk, a transcode starting, a file the browser has to read far into before it
// plays) or a stall mid-way. The overlay fades in after a moment, so a quick start shows nothing.
export function useBuffering(videoRef, mediaId, enabled) {
  const [buffering, setBuffering] = useState(false);
  useEffect(() => {
    const video = videoRef.current;
    if (!video) return undefined;
    const waiting = () => { if (!video.paused) setBuffering(true); };
    const onPlay = () => setBuffering(video.readyState < HTMLMediaElement.HAVE_FUTURE_DATA);
    const done = () => setBuffering(false);
    const progress = () => { if (video.paused || video.readyState >= HTMLMediaElement.HAVE_FUTURE_DATA) setBuffering(false); };
    const on = { waiting, seeking: waiting, play: onPlay, playing: done, pause: done, ended: done, emptied: done, error: done, canplay: progress, seeked: progress };
    Object.entries(on).forEach(([name, fn]) => video.addEventListener(name, fn));
    setBuffering(!video.paused && video.readyState < HTMLMediaElement.HAVE_FUTURE_DATA);
    return () => Object.entries(on).forEach(([name, fn]) => video.removeEventListener(name, fn));
  }, [videoRef, mediaId, enabled]);
  return buffering;
}

// `trackInfo` carries the server's audio and subtitle lists and the current choice; without it the menu
// falls back to whatever tracks the browser itself exposes.
export default function PlayerControls({ videoRef, frameRef, mediaId, markers, enabled = true, onToggleFullscreen, isFullscreen, onPrev, onNext, nextLabel = 'Next in queue', playbackRate = 1, onRateChange, onMarkWatched, trackInfo, transcode }) {
  const [paused, setPaused] = useState(true);
  const [muted, setMuted] = useState(false);
  const [volume, setVolume] = useState(1);
  const [pip, setPip] = useState(false);
  const [awake, setAwake] = useState(true); // false once activity has stopped while playing
  const [menuOpen, setMenuOpen] = useState(false);
  const [pipError, setPipError] = useState('');
  const tracks = useTracks(videoRef, mediaId);
  const idleTimer = useRef();
  const tapRef = useRef(null);
  const pointerType = useRef('mouse');
  const lastTap = useRef(null);
  const tapTimer = useRef();
  const [flash, setFlash] = useState(null);
  const buffering = useBuffering(videoRef, mediaId, enabled);
  const volumeOk = canSetVolume();

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return undefined;
    const sync = () => { setPaused(video.paused); setMuted(video.muted || video.volume === 0); setVolume(video.volume); setPip(canPictureInPicture(video)); };
    const events = ['play', 'pause', 'ended', 'volumechange', 'loadedmetadata', 'emptied'];
    events.forEach(name => video.addEventListener(name, sync));
    sync();
    return () => events.forEach(name => video.removeEventListener(name, sync));
  }, [videoRef, mediaId, enabled]);

  const wake = useCallback(() => {
    setAwake(true);
    clearTimeout(idleTimer.current);
    idleTimer.current = setTimeout(() => setAwake(false), IDLE_HIDE_MS);
  }, []);

  // Any activity in the frame shows the bar. A press on the video area itself is decided by onTap,
  // which needs to know whether the bar was already showing.
  useEffect(() => {
    const frame = frameRef.current;
    if (!frame) return undefined;
    const onActivity = event => { if (event.type === 'pointerdown' && event.target === tapRef.current) return; wake(); };
    const events = ['pointermove', 'pointerdown', 'keydown', 'focusin'];
    events.forEach(name => frame.addEventListener(name, onActivity));
    return () => { events.forEach(name => frame.removeEventListener(name, onActivity)); clearTimeout(idleTimer.current); };
  }, [frameRef, wake]);

  const visible = paused || awake || menuOpen;
  // A small player must not clip the menu: cap it to the room above the control bar (it then scrolls).
  const menuMaxHeight = menuOpen && frameRef.current ? Math.max(96, frameRef.current.clientHeight - 70) : undefined;
  const activeText = tracks.text.findIndex(track => track.mode === 'showing');
  const activeAudio = tracks.audio.findIndex(track => track.enabled);
  const chooseText = index => tracks.text.forEach((track, i) => { track.mode = i === index ? 'showing' : 'disabled'; });
  const chooseAudio = index => tracks.audio.forEach((track, i) => { track.enabled = i === index; });
  const currentChapter = useChapter(videoRef, markers);
  const togglePlay = () => {
    const video = videoRef.current;
    if (!video) return;
    if (video.paused) video.play().catch(() => {}); else video.pause();
  };
  const seekBy = seconds => {
    const video = videoRef.current;
    if (!video) return;
    const end = Number.isFinite(video.duration) ? video.duration : Infinity;
    video.currentTime = Math.max(0, Math.min(end, video.currentTime + seconds));
  };
  const singleTap = () => {
    if (!visible) { wake(); return; }
    wake();
    togglePlay();
  };
  // Touch: a quick second tap on the left or right third seeks 10s (and keeps seeking while taps continue).
  const onTap = event => {
    if (pointerType.current !== 'touch') { wake(); togglePlay(); return; }
    const rect = event.currentTarget.getBoundingClientRect();
    const x = (event.clientX - rect.left) / (rect.width || 1);
    const side = x < 0.4 ? -1 : x > 0.6 ? 1 : 0;
    const last = lastTap.current;
    const now = Date.now();
    if (side && last && last.side === side && now - last.at < DOUBLE_TAP_MS) {
      clearTimeout(tapTimer.current);
      lastTap.current = { side, at: now };
      seekBy(side * 10);
      wake();
      setFlash(prev => ({ side, key: (prev?.key || 0) + 1, total: prev && prev.side === side && now - prev.at < 900 ? prev.total + 10 : 10, at: now }));
      return;
    }
    lastTap.current = { side, at: now };
    clearTimeout(tapTimer.current);
    tapTimer.current = setTimeout(() => { lastTap.current = null; singleTap(); }, DOUBLE_TAP_MS);
  };
  useEffect(() => () => clearTimeout(tapTimer.current), []);
  useEffect(() => {
    if (!flash) return undefined;
    const timer = setTimeout(() => setFlash(null), 700);
    return () => clearTimeout(timer);
  }, [flash]);
  const toggleMute = () => {
    const video = videoRef.current;
    if (!video) return;
    video.muted = !video.muted;
    if (!video.muted && video.volume === 0) video.volume = 0.5;
  };
  const changeVolume = event => {
    const video = videoRef.current;
    if (!video) return;
    const next = Number(event.target.value);
    video.volume = next;
    video.muted = next === 0;
  };

  return <>
    <div
      ref={tapRef}
      className="rs-tap-layer"
      aria-hidden="true"
      onPointerDown={event => { pointerType.current = event.pointerType || 'mouse'; }}
      onClick={onTap}
      onDoubleClick={() => { if (pointerType.current !== 'touch') onToggleFullscreen(); }}
    />
    {buffering && <div className="rs-player-buffering" role="status" aria-label="Loading video"><span /></div>}
    {flash && <div key={flash.key} className={`rs-seek-flash rs-seek-flash-${flash.side < 0 ? 'back' : 'fwd'}`} aria-hidden="true">{flash.side < 0 ? `« ${flash.total}s` : `${flash.total}s »`}</div>}
    <div className={`rs-controls${visible ? ' is-visible' : ''}`}>
      <PlayerTimeline videoRef={videoRef} mediaId={mediaId} markers={markers} enabled={enabled} bare />
      <div className="rs-controls-row">
      <div className="rs-controls-left">
      <button type="button" className="rs-controls-btn" onClick={togglePlay} aria-label={paused ? 'Play' : 'Pause'}>
        {paused ? <Play size={18} fill="currentColor" /> : <Pause size={18} fill="currentColor" />}
      </button>
      {onPrev && <button type="button" className="rs-controls-btn" onClick={onPrev} aria-label="Previous in queue"><SkipBack size={18} /></button>}
      {onNext && <button type="button" className="rs-controls-btn" onClick={onNext} aria-label={nextLabel}><SkipForward size={18} /></button>}
      <span className="rs-controls-volume">
        <button type="button" className="rs-controls-btn" onClick={toggleMute} aria-label={muted ? 'Unmute' : 'Mute'} aria-pressed={muted}>
          {muted ? <VolumeX size={18} /> : <Volume2 size={18} />}
        </button>
        {volumeOk && <input className="rs-controls-volume-slider" type="range" min={0} max={1} step={0.05} value={muted ? 0 : volume} style={{ '--volume-level': `${(muted ? 0 : volume) * 100}%` }} onChange={changeVolume} aria-label="Volume" />}
      </span>
      <PlayerClock videoRef={videoRef} mediaId={mediaId} />
      {currentChapter && <span className="rs-controls-chapter" title={currentChapter}>{currentChapter}</span>}
      </div>
      <div className="rs-controls-right">
      <span className="rs-controls-menu-wrap">
        <button type="button" className="rs-controls-btn rs-controls-speed" onClick={() => setMenuOpen(open => !open)} aria-label="Playback settings" aria-expanded={menuOpen} aria-haspopup="menu">
          {playbackRate !== 1 ? <span>{playbackRate}×</span> : <Settings size={18} />}{transcode?.on && <span className="rs-controls-badge">HD</span>}
        </button>
        {menuOpen && <div className="rs-controls-menu" role="menu" style={menuMaxHeight ? { maxHeight: menuMaxHeight } : undefined} onKeyDown={event => { if (event.key === 'Escape') { event.stopPropagation(); setMenuOpen(false); } }}>
          <div className="rs-controls-menu-group">
            <p>Speed</p>
            {SPEEDS.map(rate => <button key={rate} type="button" role="menuitemradio" aria-checked={rate === playbackRate} onClick={() => onRateChange?.(rate)}>{rate}×</button>)}
          </div>
          {trackInfo?.subtitles?.length > 0 ? <div className="rs-controls-menu-group">
            <p>Subtitles</p>
            <button type="button" role="menuitemradio" aria-checked={!trackInfo.subtitleId} onClick={() => trackInfo.onSubtitle(null)}>Off</button>
            {trackInfo.subtitles.map(track => <button key={track.id} type="button" role="menuitemradio" aria-checked={track.id === trackInfo.subtitleId} onClick={() => trackInfo.onSubtitle(track.id)}>{track.label}</button>)}
          </div> : tracks.text.length > 0 && <div className="rs-controls-menu-group">
            <p>Subtitles</p>
            <button type="button" role="menuitemradio" aria-checked={activeText < 0} onClick={() => chooseText(-1)}>Off</button>
            {tracks.text.map((track, i) => <button key={i} type="button" role="menuitemradio" aria-checked={i === activeText} onClick={() => chooseText(i)}>{trackLabel(track, i)}</button>)}
          </div>}
          {trackInfo?.audio?.length > 1 ? <div className="rs-controls-menu-group">
            <p>Audio</p>
            {trackInfo.audio.map(track => <button key={track.index} type="button" role="menuitemradio" aria-checked={track.index === trackInfo.audioIndex} onClick={() => trackInfo.onAudio(track.index)}>{track.label}</button>)}
          </div> : tracks.audio.length > 1 && <div className="rs-controls-menu-group">
            <p>Audio</p>
            {tracks.audio.map((track, i) => <button key={i} type="button" role="menuitemradio" aria-checked={i === activeAudio} onClick={() => chooseAudio(i)}>{trackLabel(track, i)}</button>)}
          </div>}
          {transcode && <div className="rs-controls-menu-group">
            <p>Transcode</p>
            <button type="button" role="menuitemcheckbox" aria-checked={transcode.on} disabled={transcode.disabled} onClick={transcode.onToggle} title={transcode.forced ? 'This audio track needs the server to re-encode the video.' : 'Re-encode on the server. Use it if the original file will not play in this browser.'}><Zap size={15} /> {transcode.on ? 'Transcoding on' : 'Transcode video'}</button>
            {transcode.on && transcode.qualities.map(q => <button key={q.value} type="button" role="menuitemradio" aria-checked={q.value === transcode.quality} onClick={() => transcode.onQuality(q.value)}>{q.label}</button>)}
          </div>}
          {onMarkWatched && <div className="rs-controls-menu-group">
            <button type="button" role="menuitem" onClick={() => { onMarkWatched(); setMenuOpen(false); }}><CheckCheck size={15} /> Mark as watched</button>
          </div>}
        </div>}
      </span>
      {pipError && <span className="rs-controls-chapter" role="status">{pipError}</span>}
      {(markers || []).some(marker => /intro|opening|recap/i.test(marker.title || '')) && <button type="button" className="rs-controls-btn" onClick={() => { const intro = markers.find(marker => /intro|opening|recap/i.test(marker.title || '')); const video = videoRef.current; if (intro && video) video.currentTime = intro.seconds; }} aria-label="Skip intro">Skip intro</button>}
      <button type="button" className="rs-controls-btn" onClick={async () => {
        const video = videoRef.current;
        try {
          if (video?.remote?.prompt) { await video.remote.prompt(); return; }
          if (video?.webkitShowPlaybackTargetPicker) { video.webkitShowPlaybackTargetPicker(); return; }
        } catch (err) {
          setPipError(err?.message || 'Casting is not available');
          setTimeout(() => setPipError(''), 4000);
          return;
        }
        setPipError('This browser has no Cast or AirPlay control');
        setTimeout(() => setPipError(''), 4000);
      }} aria-label="Cast or AirPlay"><Cast size={18} /></button>
      {pip && <button type="button" className="rs-controls-btn" onClick={async () => { const problem = await togglePictureInPicture(videoRef.current); if (problem) { setPipError(problem); setTimeout(() => setPipError(''), 4000); } }} aria-label="Picture in picture"><PictureInPicture2 size={18} /></button>}
      <button type="button" className="rs-controls-btn" onClick={onToggleFullscreen} aria-label={isFullscreen ? 'Exit fullscreen' : 'Enter fullscreen'}>
        {isFullscreen ? <Minimize2 size={18} /> : <Maximize2 size={18} />}
      </button>
      </div>
      </div>
    </div>
  </>;
}

// The title of the marker the playhead is past, so markers double as chapters.
function useChapter(videoRef, markers) {
  const [title, setTitle] = useState('');
  useEffect(() => {
    const video = videoRef.current;
    if (!video || !markers?.length) { setTitle(''); return undefined; }
    const sorted = [...markers].sort((a, b) => a.seconds - b.seconds);
    const sync = () => {
      const passed = sorted.filter(marker => marker.seconds <= video.currentTime + 0.25);
      setTitle(passed.length ? passed[passed.length - 1].title : '');
    };
    video.addEventListener('timeupdate', sync);
    video.addEventListener('seeked', sync);
    sync();
    return () => { video.removeEventListener('timeupdate', sync); video.removeEventListener('seeked', sync); };
  }, [videoRef, markers]);
  return title;
}
