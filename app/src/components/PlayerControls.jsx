import React, { useCallback, useEffect, useRef, useState } from 'react';
import { ArrowLeft, BookmarkPlus, Cast, Check, CheckCheck, ChevronLeft, ChevronRight, Flag, ListVideo, Maximize2, Minimize2, Pause, PictureInPicture2, Play, RotateCcw, RotateCw, SkipBack, SkipForward, SlidersHorizontal, Volume2, VolumeX, Zap, Moon, Captions } from 'lucide-react';
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

// The playhead and length, kept in step with the video.
function useClock(videoRef, mediaId) {
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
  return clock;
}

// "2:08 / 11:14", next to the volume like YouTube's.
function PlayerClock({ videoRef, mediaId }) {
  const clock = useClock(videoRef, mediaId);
  return <span className="rs-controls-clock">{formatClock(clock.current)} / {formatClock(clock.duration)}</span>;
}

// "Next marker: Sandstorm time-lapse · 16:20", a chip above the bar that jumps there.
function NextMarkerChip({ videoRef, mediaId, markers, visible }) {
  const clock = useClock(videoRef, mediaId);
  const next = [...(markers || [])].sort((a, b) => a.seconds - b.seconds).find(marker => marker.seconds > clock.current + 0.5);
  if (!next) return null;
  return <div className={`rs-marker-chip-wrap${visible ? ' is-visible' : ''}`}>
    <button type="button" className="rs-marker-chip" tabIndex={visible ? 0 : -1} onClick={() => { const video = videoRef.current; if (video) video.currentTime = next.seconds; }}>
      <Flag size={16} aria-hidden="true" />Next marker: {next.title ? `${next.title} · ` : ''}{formatClock(next.seconds)}
    </button>
  </div>;
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

const SLEEP_OPTIONS = [0, 15, 30, 45, 60, 90];

// One row of the settings popover: a name, the current choice and a chevron into its list.
function SettingsRow({ icon: Icon, label, value, onClick }) {
  return <button type="button" role="menuitem" className="rs-controls-row-item" onClick={onClick}>
    <Icon size={18} aria-hidden="true" /><span>{label}</span><span className="rs-controls-row-gap" /><span className="rs-controls-row-value">{value}</span><ChevronRight size={14} aria-hidden="true" />
  </button>;
}

// `trackInfo` carries the server's audio and subtitle lists and the current choice; without it the menu
// falls back to whatever tracks the browser itself exposes.
// `title`, `kicker` and `badge` fill the bar across the top; `queueItems` feeds the Queue list.
export default function PlayerControls({ videoRef, frameRef, mediaId, markers, enabled = true, onAddMarker, onToggleFullscreen, isFullscreen, onPrev, onNext, nextLabel = 'Next in queue', playbackRate = 1, onRateChange, onMarkWatched, trackInfo, transcode, title, kicker, badge, queueItems = [], onQueueGo, idle = false }) {
  const [paused, setPaused] = useState(true);
  const [muted, setMuted] = useState(false);
  const [volume, setVolume] = useState(1);
  const [pip, setPip] = useState(false);
  const [awake, setAwake] = useState(true); // false once activity has stopped while playing
  const [panel, setPanel] = useState(null); // null | 'settings' | 'markers' | 'queue'
  const [view, setView] = useState('root'); // inside settings: root | quality | transcode | audio | subtitles | sleep
  const [sleepMinutes, setSleepMinutes] = useState(0);
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
  const menuOpen = panel !== null;
  const closePanel = () => { setPanel(null); setView('root'); };
  const togglePanel = name => { setView('root'); setPanel(current => (current === name ? null : name)); };

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return undefined;
    const sync = () => { setPaused(video.paused); setMuted(video.muted || video.volume === 0); setVolume(video.volume); setPip(canPictureInPicture(video)); };
    const events = ['play', 'pause', 'ended', 'volumechange', 'loadedmetadata', 'emptied'];
    events.forEach(name => video.addEventListener(name, sync));
    sync();
    return () => events.forEach(name => video.removeEventListener(name, sync));
  }, [videoRef, mediaId, enabled]);

  // Sleep timer: pauses once the chosen number of minutes has passed.
  useEffect(() => {
    if (!sleepMinutes) return undefined;
    const timer = setTimeout(() => { videoRef.current?.pause(); setSleepMinutes(0); }, sleepMinutes * 60000);
    return () => clearTimeout(timer);
  }, [sleepMinutes, videoRef]);

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

  // `idle`: the title page's cover is showing, so the bar and its companions stay out of the way.
  const visible = !idle && (paused || awake || menuOpen);
  // A small player must not clip the menu: cap it to the room above the control bar (it then scrolls).
  const menuMaxHeight = menuOpen && frameRef.current ? Math.max(96, frameRef.current.clientHeight - 90) : undefined;
  const activeText = tracks.text.findIndex(track => track.mode === 'showing');
  const activeAudio = tracks.audio.findIndex(track => track.enabled);
  const chooseText = index => tracks.text.forEach((track, i) => { track.mode = i === index ? 'showing' : 'disabled'; });
  const chooseAudio = index => tracks.audio.forEach((track, i) => { track.enabled = i === index; });
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

  // The settings popover's lists. `pick` runs the choice and returns to the main list.
  const pick = action => () => { action(); setView('root'); };
  const subtitleList = trackInfo?.subtitles?.length > 0
    ? { value: trackInfo.subtitles.find(track => track.id === trackInfo.subtitleId)?.label || 'Off', options: [{ label: 'Off', on: !trackInfo.subtitleId, run: () => trackInfo.onSubtitle(null) }, ...trackInfo.subtitles.map(track => ({ label: track.label, on: track.id === trackInfo.subtitleId, run: () => trackInfo.onSubtitle(track.id) }))] }
    : tracks.text.length > 0 ? { value: activeText >= 0 ? trackLabel(tracks.text[activeText], activeText) : 'Off', options: [{ label: 'Off', on: activeText < 0, run: () => chooseText(-1) }, ...tracks.text.map((track, i) => ({ label: trackLabel(track, i), on: i === activeText, run: () => chooseText(i) }))] } : null;
  const audioList = trackInfo?.audio?.length > 1
    ? { value: trackInfo.audio.find(track => track.index === trackInfo.audioIndex)?.label || '', options: trackInfo.audio.map(track => ({ label: track.label, on: track.index === trackInfo.audioIndex, run: () => trackInfo.onAudio(track.index) })) }
    : tracks.audio.length > 1 ? { value: activeAudio >= 0 ? trackLabel(tracks.audio[activeAudio], activeAudio) : '', options: tracks.audio.map((track, i) => ({ label: trackLabel(track, i), on: i === activeAudio, run: () => chooseAudio(i) })) } : null;
  const qualityList = transcode ? {
    value: transcode.on ? (transcode.qualities.find(q => q.value === transcode.quality)?.label || 'Transcoded') : 'Original',
    options: [{ label: 'Original', on: !transcode.on, disabled: transcode.disabled && !transcode.on ? false : transcode.forced, run: () => { if (transcode.on) transcode.onToggle(); } },
      ...transcode.qualities.map(q => ({ label: q.label, on: transcode.on && q.value === transcode.quality, disabled: transcode.disabled && !transcode.on, run: () => { if (!transcode.on) transcode.onToggle(); transcode.onQuality(q.value); } }))],
  } : null;
  const sleepList = { value: sleepMinutes ? `${sleepMinutes} min` : 'Off', options: SLEEP_OPTIONS.map(minutes => ({ label: minutes ? `${minutes} minutes` : 'Off', on: minutes === sleepMinutes, run: () => setSleepMinutes(minutes) })) };
  const lists = { quality: ['Quality', qualityList], audio: ['Audio', audioList], subtitles: ['Subtitles', subtitleList], sleep: ['Sleep timer', sleepList] };
  const markerList = [...(markers || [])].sort((a, b) => a.seconds - b.seconds);
  const hasMarkers = markerList.length > 0;

  const castOrAirPlay = async () => {
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
    {(title || badge) && <div className={`rs-controls-top${visible ? ' is-visible' : ''}`}>
      {isFullscreen && <button type="button" className="rs-controls-back" onClick={onToggleFullscreen} aria-label="Exit fullscreen"><ArrowLeft size={20} aria-hidden="true" /></button>}
      <div className="rs-controls-titles">{kicker && <span>{kicker}</span>}<strong>{title}</strong></div>
      <span className="rs-controls-gap" />
      {badge && <span className="rs-controls-badge-pill">{badge}</span>}
    </div>}
    {hasMarkers && <NextMarkerChip videoRef={videoRef} mediaId={mediaId} markers={markers} visible={visible} />}
    <div className={`rs-controls${visible ? ' is-visible' : ''}`}>
      <PlayerTimeline videoRef={videoRef} mediaId={mediaId} markers={markers} enabled={enabled} bare />
      <div className="rs-controls-row">
      <div className="rs-controls-left">
      <button type="button" className="rs-controls-btn" onClick={togglePlay} aria-label={paused ? 'Play' : 'Pause'}>
        {paused ? <Play size={24} fill="currentColor" /> : <Pause size={24} fill="currentColor" />}
      </button>
      <button type="button" className="rs-controls-btn" onClick={() => seekBy(-10)} aria-label="Back 10 seconds"><RotateCcw size={22} /></button>
      <button type="button" className="rs-controls-btn" onClick={() => seekBy(10)} aria-label="Forward 10 seconds"><RotateCw size={22} /></button>
      {onPrev && <button type="button" className="rs-controls-btn" onClick={onPrev} aria-label="Previous in queue"><SkipBack size={22} /></button>}
      {onNext && <button type="button" className="rs-controls-btn" onClick={onNext} aria-label={nextLabel}><SkipForward size={22} /></button>}
      <span className="rs-controls-volume">
        <button type="button" className="rs-controls-btn" onClick={toggleMute} aria-label={muted ? 'Unmute' : 'Mute'} aria-pressed={muted}>
          {muted ? <VolumeX size={22} /> : <Volume2 size={22} />}
        </button>
        {volumeOk && <input className="rs-controls-volume-slider" type="range" min={0} max={1} step={0.05} value={muted ? 0 : volume} style={{ '--volume-level': `${(muted ? 0 : volume) * 100}%` }} onChange={changeVolume} aria-label="Volume" />}
      </span>
      <PlayerClock videoRef={videoRef} mediaId={mediaId} />
      </div>
      <div className="rs-controls-right">
      {playbackRate !== 1 && <span className="rs-controls-rate">{playbackRate}×</span>}
      {pipError && <span className="rs-controls-chapter" role="status">{pipError}</span>}
      {(markers || []).some(marker => /intro|opening|recap/i.test(marker.title || '')) && <button type="button" className="rs-controls-btn rs-controls-text" onClick={() => { const intro = markers.find(marker => /intro|opening|recap/i.test(marker.title || '')); const video = videoRef.current; if (intro && video) video.currentTime = intro.seconds; }} aria-label="Skip intro">Skip intro</button>}
      <span className="rs-controls-menu-wrap">
        <button type="button" className={`rs-controls-btn${panel === 'markers' ? ' is-on' : ''}`} onClick={() => togglePanel('markers')} aria-label="Markers" aria-expanded={panel === 'markers'} aria-haspopup="menu"><Flag size={22} /></button>
        {panel === 'markers' && <div className="rs-controls-menu" role="menu" aria-label="Markers" style={menuMaxHeight ? { maxHeight: menuMaxHeight } : undefined} onKeyDown={event => { if (event.key === 'Escape') { event.stopPropagation(); closePanel(); } }}>
          {onAddMarker && <div className="rs-controls-menu-group"><button type="button" role="menuitem" onClick={async () => { await onAddMarker(); closePanel(); }}><BookmarkPlus size={18} /> Mark this moment<kbd>M</kbd></button></div>}
          <div className="rs-controls-menu-group">
            {markerList.length === 0 && <p>No markers yet</p>}
            {markerList.map(marker => <button key={marker.id} type="button" role="menuitem" className="rs-controls-row-item" onClick={() => { const video = videoRef.current; if (video) video.currentTime = marker.seconds; closePanel(); }}><span className="rs-controls-row-time">{formatClock(marker.seconds)}</span><span>{marker.title || 'Marker'}</span></button>)}
          </div>
        </div>}
      </span>
      <span className="rs-controls-menu-wrap">
        <button type="button" className={`rs-controls-btn${panel === 'queue' ? ' is-on' : ''}`} onClick={() => togglePanel('queue')} aria-label="Queue" aria-expanded={panel === 'queue'} aria-haspopup="menu"><ListVideo size={22} /></button>
        {panel === 'queue' && <div className="rs-controls-menu" role="menu" aria-label="Queue" style={menuMaxHeight ? { maxHeight: menuMaxHeight } : undefined} onKeyDown={event => { if (event.key === 'Escape') { event.stopPropagation(); closePanel(); } }}>
          <div className="rs-controls-menu-group">
            {queueItems.length === 0 && <p>Your queue is empty</p>}
            {queueItems.map(entry => <button key={entry.id} type="button" role="menuitem" className="rs-controls-row-item" aria-current={String(entry.id) === String(mediaId) ? 'true' : undefined} onClick={() => { closePanel(); onQueueGo?.(entry); }}><span>{entry.title}</span>{String(entry.id) === String(mediaId) && <span className="rs-controls-row-gap" />}{String(entry.id) === String(mediaId) && <Check size={14} aria-hidden="true" />}</button>)}
          </div>
        </div>}
      </span>
      <span className="rs-controls-menu-wrap">
        <button type="button" className={`rs-controls-btn rs-controls-speed${panel === 'settings' ? ' is-on' : ''}`} onClick={() => togglePanel('settings')} aria-label="Playback settings" aria-expanded={panel === 'settings'} aria-haspopup="menu">
          <SlidersHorizontal size={22} />{transcode?.on && <span className="rs-controls-badge">HD</span>}
        </button>
        {panel === 'settings' && <div className="rs-controls-menu" role="menu" aria-label="Playback settings" style={menuMaxHeight ? { maxHeight: menuMaxHeight } : undefined} onKeyDown={event => { if (event.key === 'Escape') { event.stopPropagation(); if (view !== 'root') setView('root'); else closePanel(); } }}>
          {view === 'root' ? <>
            {onAddMarker && <div className="rs-controls-menu-group">
              <button type="button" role="menuitem" className="rs-controls-mark" onClick={async () => { await onAddMarker(); closePanel(); }}><BookmarkPlus size={18} /> Mark this moment<kbd>M</kbd></button>
            </div>}
            <div className="rs-controls-menu-group rs-controls-speeds">
              <p>Speed</p>
              {SPEEDS.map(rate => <button key={rate} type="button" role="menuitemradio" aria-checked={rate === playbackRate} onClick={() => onRateChange?.(rate)}>{rate}×</button>)}
            </div>
            <div className="rs-controls-menu-group rs-controls-rows">
              {qualityList && <SettingsRow icon={SlidersHorizontal} label="Quality" value={qualityList.value} onClick={() => setView('quality')} />}
              {transcode && <SettingsRow icon={Zap} label="Transcode" value={transcode.on ? 'On' : 'Off'} onClick={() => setView('transcode')} />}
              {audioList && <SettingsRow icon={Volume2} label="Audio" value={audioList.value} onClick={() => setView('audio')} />}
              {subtitleList && <SettingsRow icon={Captions} label="Subtitles" value={subtitleList.value} onClick={() => setView('subtitles')} />}
              <SettingsRow icon={Moon} label="Sleep timer" value={sleepList.value} onClick={() => setView('sleep')} />
            </div>
            {onMarkWatched && <div className="rs-controls-menu-group rs-controls-foot">
              <button type="button" role="menuitem" onClick={() => { onMarkWatched(); closePanel(); }}><CheckCheck size={18} /> Mark as watched</button>
            </div>}
          </> : <div className="rs-controls-menu-group rs-controls-sub">
            <button type="button" className="rs-controls-subback" onClick={() => setView('root')} aria-label="Back to settings"><ChevronLeft size={16} aria-hidden="true" />{view === 'transcode' ? 'Transcode' : lists[view]?.[0]}</button>
            {view === 'transcode'
              ? <>
                <button type="button" role="menuitemradio" aria-checked={!transcode.on} disabled={transcode.disabled && !transcode.on ? false : transcode.forced} onClick={pick(() => { if (transcode.on) transcode.onToggle(); })} title={transcode.forced ? 'This audio track needs the server to re-encode the video.' : undefined}>Off</button>
                <button type="button" role="menuitemradio" aria-checked={transcode.on} disabled={transcode.disabled && !transcode.on} onClick={pick(() => { if (!transcode.on) transcode.onToggle(); })} title="Re-encode on the server. Use it if the original file will not play in this browser.">On</button>
              </>
              : lists[view]?.[1]?.options.map(option => <button key={option.label} type="button" role="menuitemradio" aria-checked={option.on} disabled={option.disabled} onClick={pick(option.run)}>{option.label}</button>)}
          </div>}
        </div>}
      </span>
      <button type="button" className="rs-controls-btn" onClick={castOrAirPlay} aria-label="Cast or AirPlay"><Cast size={22} /></button>
      {pip && <button type="button" className="rs-controls-btn" onClick={async () => { const problem = await togglePictureInPicture(videoRef.current); if (problem) { setPipError(problem); setTimeout(() => setPipError(''), 4000); } }} aria-label="Picture in picture"><PictureInPicture2 size={22} /></button>}
      <button type="button" className="rs-controls-btn" onClick={onToggleFullscreen} aria-label={isFullscreen ? 'Exit fullscreen' : 'Enter fullscreen'}>
        {isFullscreen ? <Minimize2 size={22} /> : <Maximize2 size={22} />}
      </button>
      </div>
      </div>
    </div>
  </>;
}
