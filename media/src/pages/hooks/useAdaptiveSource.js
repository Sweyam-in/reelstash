import { useEffect, useRef, useState } from 'react';
import Hls from 'hls.js';
import { getPlaybackInfo } from '../../api';
import { canRemuxAudio } from '../../trackPrefs';

const hlsUrl = (id, quality, audio = 0, burn = -1) => {
  const params = [];
  if (quality && quality !== 'balanced') params.push(`quality=${quality}`);
  if (audio > 0) params.push(`audio=${audio}`);
  if (burn >= 0) params.push(`burn=${burn}`);
  return `/api/media/${id}/hls/index.m3u8${params.length ? `?${params.join('&')}` : ''}`;
};
export const TRANSCODE_QUALITIES = [
  { value: 'high', label: 'High' },
  { value: 'balanced', label: 'Balanced' },
  { value: 'low', label: 'Low (720p)' },
];
const QUALITY_KEY = 'reelstash.transcodeQuality';
const readQuality = () => {
  try {
    const saved = localStorage.getItem(QUALITY_KEY);
    return TRANSCODE_QUALITIES.some(q => q.value === saved) ? saved : 'balanced';
  } catch { return 'balanced'; }
};
const streamUrl = (id, audio = 0) => `/api/media/${id}/stream${audio > 0 ? `?audio=${audio}` : ''}`;

// Stall watchdog thresholds, in seconds of a frozen playhead while "playing".
const NUDGE_AFTER_S = 6;
const DIRECT_SKIP_AFTER_S = 15; // original file: treat a long freeze as a damaged stretch
const HLS_RELOAD_AFTER_S = 40;  // transcode: segments can legitimately take a while
const MAX_AUTO_RELOADS = 3;
const MAX_NETWORK_RETRIES = 3;

// Damaged-file recovery: each consecutive failure without real progress jumps
// further past the bad spot, and after MAX_SKIP_FAILURES the player stops and
// offers the user a choice instead of looping.
const SKIP_STEPS_S = [2, 5, 15, 30, 60];
const MAX_SKIP_FAILURES = 6;
const PROGRESS_RESETS_FAILURES_S = 10;

// Picks how to play a title in this browser:
//   direct — the original file, byte-range streamed (zero server cost)
//   hls    — server-side ffmpeg transcode to H.264/AAC HLS segments
// Direct is the default, and playback never switches to a transcode by
// itself: a file the server already knows the browser cannot decode starts in
// hls, but a decode error on a playable file is handled by skipping past the
// damaged stretch. The user can pick the transcode explicitly (setTranscode).
//
// `audio` is the wanted audio track, { index, codec }, or null for the file's first track. A
// non-default track cannot be chosen in the browser, so the server hands over a copy with that track
// (a cheap remux when the codecs allow, see canRemuxAudio) or a transcode that carries it.
export function useAdaptiveSource(videoRef, mediaItemId, enabled, onFatal, audio = null, burnIndex = -1) {
  const [mode, setMode] = useState(null);
  const [retryGeneration, setRetryGeneration] = useState(0);
  const [skips, setSkips] = useState(0); // damaged stretches skipped so far
  const [lastSkip, setLastSkip] = useState(0);
  const [quality, setQualityState] = useState(readQuality);
  const resumeAt = useRef(null); // position to restore after a reload
  const resumePlayback = useRef(false);
  const hlsRef = useRef(null);
  const skipState = useRef({ failures: 0, from: -1 });
  const fatalRef = useRef(onFatal);
  fatalRef.current = onFatal;
  const qualityRef = useRef(quality);
  qualityRef.current = quality;
  const [info, setInfo] = useState(null); // what the server said about the file
  const audioIndex = audio?.index > 0 ? audio.index : 0;
  const burn = Number.isInteger(burnIndex) && burnIndex >= 0 ? burnIndex : -1;
  // A track the browser cannot be handed cheaply forces the transcode for as long as it is chosen.
  const audioForcesTranscode = audioIndex > 0 && Boolean(info?.transcoding) && !canRemuxAudio(info.videoCodec, audio);
  // Picture subtitles are drawn on the server, so they also force a transcode.
  const sourceMode = audioForcesTranscode || burn >= 0 ? 'hls' : mode;
  // Only a transcode depends on quality; the original file must not reload when it changes.
  const sourceQuality = sourceMode === 'hls' ? quality : null;

  // Decide the initial mode.
  useEffect(() => {
    if (!enabled) return undefined;
    let cancelled = false;
    setMode(null);
    setSkips(0);
    resumeAt.current = null;
    resumePlayback.current = false;
    skipState.current = { failures: 0, from: -1 };
    setInfo(null);
    getPlaybackInfo(mediaItemId).then(result => {
      if (cancelled) return;
      const probe = document.createElement('video');
      const vOk = probe.canPlayType('video/mp4') !== '';
      const canDirect = result.directPlay && vOk;
      setInfo(result);
      setMode(canDirect || !result.transcoding ? 'direct' : 'hls');
    }).catch(() => { if (!cancelled) setMode('direct'); });
    return () => { cancelled = true; };
  }, [mediaItemId, enabled]);

  // Attach the source for the chosen mode.
  useEffect(() => {
    const video = videoRef.current;
    if (!video || !sourceMode) return undefined;
    const quality = qualityRef.current;
    const start = resumeAt.current;
    let cleanup = () => {};

    // Reloads the current source a little past `from`, resuming playback.
    const reloadAt = position => {
      resumeAt.current = Number.isFinite(position) ? position : null;
      resumePlayback.current = true;
      setRetryGeneration(generation => generation + 1);
    };
    let networkRetries = 0;

    // Jumps past a damaged stretch of the original file. Escalates the jump on
    // repeated failures at the same spot; gives up (user decides) after a few.
    const skipPastDamage = () => {
      const state = skipState.current;
      const at = Number.isFinite(video.currentTime) ? video.currentTime : 0;
      if (Math.abs(at - state.from) > PROGRESS_RESETS_FAILURES_S) state.failures = 0;
      if (state.failures >= MAX_SKIP_FAILURES) { fatalRef.current?.(); return; }
      const step = SKIP_STEPS_S[Math.min(state.failures, SKIP_STEPS_S.length - 1)];
      state.failures += 1;
      setLastSkip(step);
      const target = at + step;
      state.from = target; // progress is measured from where the skip lands
      if (Number.isFinite(video.duration) && target >= video.duration - 1) { fatalRef.current?.(); return; }
      setSkips(count => count + 1);
      reloadAt(target);
    };

    const onErr = () => {
      if (sourceMode !== 'direct') { fatalRef.current?.(); return; }
      const code = video.error?.code;
      const started = Number.isFinite(video.currentTime) && video.currentTime > 0;
      // A network read failure says nothing about the media at this position.
      // Retry in place so a transient disk/network hiccup doesn't skip content.
      if (code === MediaError.MEDIA_ERR_NETWORK) {
        if (networkRetries >= MAX_NETWORK_RETRIES) { fatalRef.current?.(); return; }
        networkRetries += 1;
        reloadAt(video.currentTime);
        return;
      }
      // Not decodable at all from the very start: skipping will not help.
      if (code === MediaError.MEDIA_ERR_SRC_NOT_SUPPORTED && !started) { fatalRef.current?.(); return; }
      skipPastDamage();
    };
    // Audio only (the classic "black video"): let the user choose to transcode.
    const onMeta = () => { if (sourceMode === 'direct' && video.videoWidth === 0 && video.videoHeight === 0) fatalRef.current?.(); };
    video.addEventListener('error', onErr);
    video.addEventListener('loadedmetadata', onMeta);
    cleanup = () => { video.removeEventListener('error', onErr); video.removeEventListener('loadedmetadata', onMeta); };

    if (sourceMode === 'direct') {
      video.src = streamUrl(mediaItemId, audioIndex);
    } else if (Hls.isSupported()) {
      const hls = new Hls({ startPosition: start ?? -1, maxBufferLength: 40, backBufferLength: 60, enableWorker: true });
      hlsRef.current = hls;
      let recoveries = 0;
      const onPlaying = () => { recoveries = 0; };
      video.addEventListener('playing', onPlaying);
      const previousCleanup = cleanup;
      cleanup = () => { previousCleanup(); video.removeEventListener('playing', onPlaying); };
      hls.on(Hls.Events.ERROR, (_e, data) => {
        if (!data.fatal) return;
        resumePlayback.current ||= !video.paused && !video.ended;
        if (++recoveries > 3) { fatalRef.current?.(); return; }
        if (data.type === Hls.ErrorTypes.NETWORK_ERROR) hls.startLoad();
        else if (data.type === Hls.ErrorTypes.MEDIA_ERROR) hls.recoverMediaError();
        else fatalRef.current?.();
      });
      hls.loadSource(hlsUrl(mediaItemId, quality, audioIndex, burn));
      hls.attachMedia(video);
    } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
      video.src = hlsUrl(mediaItemId, quality, audioIndex, burn); // Safari plays HLS natively
    } else {
      fatalRef.current?.();
    }

    // Stall watchdog: the video claims to be playing but the playhead has not
    // moved. Nudge first. After that, on the original file skip past the
    // presumed damage; on a transcode reload at the same spot.
    let lastTime = -1;
    let stalledFor = 0;
    let reloads = 0;
    const onProgress = () => { networkRetries = 0; };
    video.addEventListener('timeupdate', onProgress);
    const reloadAfter = sourceMode === 'direct' ? DIRECT_SKIP_AFTER_S : HLS_RELOAD_AFTER_S;
    const watchdog = setInterval(() => {
      if (video.paused || video.ended) { stalledFor = 0; return; }
      if (video.currentTime !== lastTime) { lastTime = video.currentTime; stalledFor = 0; reloads = 0; return; }
      stalledFor += 1;
      if (stalledFor === NUDGE_AFTER_S) {
        hlsRef.current?.startLoad();
        if (!video.seeking && Number.isFinite(video.currentTime)) video.currentTime += 0.1;
        video.play().catch(() => {});
      } else if (stalledFor >= reloadAfter) {
        stalledFor = 0;
        if (sourceMode === 'direct') { skipPastDamage(); return; }
        if (++reloads > MAX_AUTO_RELOADS) { fatalRef.current?.(); return; }
        reloadAt(video.currentTime);
      }
    }, 1000);
    const previousCleanup = cleanup;
    cleanup = () => { previousCleanup(); video.removeEventListener('timeupdate', onProgress); clearInterval(watchdog); };

    return () => {
      cleanup();
      if (hlsRef.current) { hlsRef.current.destroy(); hlsRef.current = null; }
      video.removeAttribute('src');
      video.load();
    };
  }, [sourceMode, sourceQuality, audioIndex, burn, mediaItemId, videoRef, retryGeneration]);

  const snapshotPosition = () => {
    const video = videoRef.current;
    if (video && Number.isFinite(video.currentTime)) resumeAt.current = video.currentTime;
    resumePlayback.current ||= Boolean(video && !video.paused && !video.ended);
  };

  return {
    mode: sourceMode,
    audioForcesTranscode,
    skips,
    lastSkip,
    quality,
    // Call just before changing the audio track, so playback resumes where it was.
    prepareSourceChange: snapshotPosition,
    setQuality: next => {
      if (!TRANSCODE_QUALITIES.some(q => q.value === next) || next === quality) return;
      try { localStorage.setItem(QUALITY_KEY, next); } catch { /* preference just won't persist */ }
      if (sourceMode === 'hls') snapshotPosition();
      setQualityState(next);
    },
    resumeAt,
    resumePlayback,
    // Explicit, user-chosen switch between the original file and a transcode.
    setTranscode: on => {
      const next = on ? 'hls' : 'direct';
      if (next === mode) return;
      snapshotPosition();
      skipState.current = { failures: 0, from: -1 };
      setMode(next);
    },
    retry: () => {
      snapshotPosition();
      skipState.current = { failures: 0, from: -1 };
      if (mode) setRetryGeneration(generation => generation + 1);
      else setMode('direct');
    },
    // Reload the original file a little further on (skip a bad spot by hand).
    skipAhead: seconds => {
      const video = videoRef.current;
      const at = video && Number.isFinite(video.currentTime) ? video.currentTime : 0;
      resumeAt.current = at + seconds;
      resumePlayback.current = true;
      skipState.current = { failures: 0, from: -1 };
      setRetryGeneration(generation => generation + 1);
    },
  };
}
