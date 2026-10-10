import { formatClock } from './playerUtils.js';

// Choosing which audio and subtitle track to start with, from a person's language defaults.
// prefs: { audioLanguage, subtitleLanguage, subtitleMode: 'off' | 'on' | 'foreign' }; languages are
// the same 2-letter codes the server reports on each track.

/** Index (among the audio tracks) to start in: the preferred language, else the file's default, else the first. */
export function pickAudioIndex(audio = [], prefs = {}) {
  if (!audio.length) return 0;
  if (prefs.audioLanguage) {
    const match = audio.find(track => track.language === prefs.audioLanguage);
    if (match) return match.index;
  }
  const flagged = audio.find(track => track.default);
  return (flagged || audio[0]).index;
}

/**
 * The subtitle track id to show, or null for none.
 *  - off: never.
 *  - on: the preferred language whenever the file has it.
 *  - foreign: only when the audio being played is not in the preferred language (a forced track in that
 *    language still shows, since it translates only the foreign parts).
 * A full track is preferred over a forced one, and a track from the file over a sidecar only when
 * nothing else separates them.
 */
export function pickSubtitleId(subtitles = [], audioTrack = null, prefs = {}) {
  const mode = prefs.subtitleMode || 'off';
  const language = prefs.subtitleLanguage;
  if (mode === 'off' || !language) return null;
  const inLanguage = subtitles.filter(track => track.language === language && !track.burn);
  if (!inLanguage.length) return null;
  const full = inLanguage.filter(track => !track.forced);
  const forced = inLanguage.filter(track => track.forced);
  const best = list => list.find(track => track.default) || list.find(track => track.source === 'embedded') || list[0];
  if (mode === 'foreign') {
    const heardLanguage = audioTrack?.language;
    if (heardLanguage && heardLanguage === language) return forced.length ? best(forced).id : null;
  }
  return (full.length ? best(full) : best(forced)).id;
}

/** Whether the server can hand the browser this audio track without re-encoding the video. */
export function canRemuxAudio(videoCodec, audioTrack) {
  return String(videoCodec).toLowerCase() === 'h264' && ['aac', 'mp3'].includes(String(audioTrack?.codec).toLowerCase());
}

/** "S1E4", "S2", or "" — where a title sits in its show. */
export function episodeCode(item) {
  if (item?.kind !== 'episode' || item.seasonNumber == null) return '';
  return `S${item.seasonNumber}${item.episodeNumber != null ? `E${item.episodeNumber}` : ''}`;
}

/** The line under a Continue watching card: where the person stopped. */
export function resumeKicker(item) {
  const code = episodeCode(item);
  const at = item?.positionSeconds > 0 ? `Resume at ${formatClock(item.positionSeconds)}` : '';
  return [code, at].filter(Boolean).join(' · ') || item?.seriesName || item?.studioName || '';
}

/** The line under a Next up card: which episode is waiting, and in what show. */
export function nextUpKicker(item) {
  return [episodeCode(item) || 'Next', item?.seriesName].filter(Boolean).join(' · ');
}
