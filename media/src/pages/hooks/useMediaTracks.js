import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { getPreferences, getTracks } from '../../api';
import { pickAudioIndex, pickSubtitleId } from '../../trackPrefs';

// How long the player waits for track information before it starts without it.
const TRACKS_WAIT_MS = 1500;

let cachedPrefs = null;
/** Forget the cached language defaults (after the person changes them). */
export function resetPreferenceCache() { cachedPrefs = null; }
async function loadPreferences() {
  if (!cachedPrefs) cachedPrefs = getPreferences().catch(() => ({})); // no preferences is fine: defaults apply
  return cachedPrefs;
}

/**
 * The audio and subtitle tracks of a title and which are chosen, starting from the person's language
 * defaults. `ready` turns true once the tracks are known (or after a short wait), so the player can
 * open the right audio track straight away instead of reloading. The audio choice is only applied
 * before playback starts; subtitles, which are just text, may still be picked when the tracks arrive late.
 */
export function useMediaTracks(mediaItemId, enabled) {
  const [data, setData] = useState({ audio: [], subtitles: [], videoCodec: '' });
  const [ready, setReady] = useState(false);
  const [audioIndex, setAudioIndexState] = useState(0);
  const [subtitleId, setSubtitleId] = useState(null);
  const lastSubtitle = useRef(null); // what "turn subtitles back on" returns to
  const started = useRef(false);

  useEffect(() => {
    setData({ audio: [], subtitles: [], videoCodec: '' });
    setAudioIndexState(0);
    setSubtitleId(null);
    lastSubtitle.current = null;
    started.current = false;
    setReady(false);
    if (!enabled) return undefined;
    let live = true;
    const giveUp = setTimeout(() => { if (live) { started.current = true; setReady(true); } }, TRACKS_WAIT_MS);
    Promise.allSettled([getTracks(mediaItemId), loadPreferences()]).then(([tracks, prefs]) => {
      if (!live) return;
      clearTimeout(giveUp);
      if (tracks.status === 'fulfilled') {
        const info = tracks.value;
        setData({ audio: info.audio || [], subtitles: info.subtitles || [], videoCodec: info.videoCodec || '' });
        const p = prefs.status === 'fulfilled' ? prefs.value : {};
        // Audio is decided before the player opens; if the wait ran out, the file's own first track plays.
        const audioPick = started.current ? 0 : pickAudioIndex(info.audio, p);
        setAudioIndexState(audioPick);
        const subtitlePick = pickSubtitleId(info.subtitles, (info.audio || []).find(t => t.index === audioPick), p);
        setSubtitleId(subtitlePick);
        lastSubtitle.current = subtitlePick;
      }
      started.current = true;
      setReady(true);
    });
    return () => { live = false; clearTimeout(giveUp); };
  }, [mediaItemId, enabled]);

  const audioTrack = useMemo(() => data.audio.find(t => t.index === audioIndex) || null, [data.audio, audioIndex]);
  const pickSubtitle = useCallback(id => { setSubtitleId(id); if (id) lastSubtitle.current = id; }, []);
  // "c": off when showing, otherwise the last one used (or the first available).
  const toggleSubtitles = useCallback(() => {
    if (!data.subtitles.length) return false;
    if (subtitleId) { setSubtitleId(null); return true; }
    const back = data.subtitles.find(t => t.id === lastSubtitle.current) || data.subtitles[0];
    pickSubtitle(back.id);
    return true;
  }, [data.subtitles, subtitleId, pickSubtitle]);

  return {
    ready, audio: data.audio, subtitles: data.subtitles, videoCodec: data.videoCodec,
    audioIndex, audioTrack, subtitleId,
    // The player's source hook wants { index, codec } for a non-default track.
    audioSelection: audioTrack && audioTrack.index > 0 ? { index: audioTrack.index, codec: audioTrack.codec } : null,
    setAudioIndex: setAudioIndexState, setSubtitleId: pickSubtitle, toggleSubtitles,
  };
}
