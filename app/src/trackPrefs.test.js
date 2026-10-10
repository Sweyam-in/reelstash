import test from 'node:test';
import assert from 'node:assert/strict';
import { canRemuxAudio, episodeCode, nextUpKicker, pickAudioIndex, pickSubtitleId, resumeKicker } from './trackPrefs.js';

const audio = [
  { index: 0, language: 'ja', codec: 'aac', default: true },
  { index: 1, language: 'en', codec: 'ac3' },
  { index: 2, language: 'fr', codec: 'aac' },
];
const subs = [
  { id: 'e0', source: 'embedded', language: 'en' },
  { id: 'e1', source: 'embedded', language: 'en', forced: true },
  { id: 'x1', source: 'external', language: 'es' },
  { id: 'x2', source: 'external', language: 'de', forced: true },
];

test('audio: the preferred language wins, then the flagged default, then the first track', () => {
  assert.equal(pickAudioIndex(audio, { audioLanguage: 'en' }), 1);
  assert.equal(pickAudioIndex(audio, { audioLanguage: 'fr' }), 2);
  assert.equal(pickAudioIndex(audio, { audioLanguage: 'de' }), 0, 'language the file lacks falls back');
  assert.equal(pickAudioIndex(audio, {}), 0);
  assert.equal(pickAudioIndex([{ index: 0, language: 'ja' }, { index: 1, language: 'en', default: true }], {}), 1);
  assert.equal(pickAudioIndex([], { audioLanguage: 'en' }), 0);
});

test('subtitles: off, or no language chosen, shows nothing', () => {
  assert.equal(pickSubtitleId(subs, audio[0], { subtitleMode: 'off', subtitleLanguage: 'en' }), null);
  assert.equal(pickSubtitleId(subs, audio[0], { subtitleMode: 'on' }), null);
  assert.equal(pickSubtitleId(subs, audio[0], {}), null);
});

test('subtitles: "on" shows the language whenever the file has it, preferring a full track', () => {
  assert.equal(pickSubtitleId(subs, audio[1], { subtitleMode: 'on', subtitleLanguage: 'en' }), 'e0');
  assert.equal(pickSubtitleId(subs, audio[0], { subtitleMode: 'on', subtitleLanguage: 'es' }), 'x1');
  assert.equal(pickSubtitleId(subs, audio[0], { subtitleMode: 'on', subtitleLanguage: 'fr' }), null, 'no French track');
  assert.equal(pickSubtitleId(subs, audio[0], { subtitleMode: 'on', subtitleLanguage: 'de' }), 'x2', 'a forced track is better than none');
});

test('subtitles: "foreign" shows them only when the audio is in another language', () => {
  const prefs = { subtitleMode: 'foreign', subtitleLanguage: 'en' };
  assert.equal(pickSubtitleId(subs, audio[0], prefs), 'e0', 'Japanese audio: show English');
  assert.equal(pickSubtitleId(subs, audio[1], prefs), 'e1', 'English audio: only the forced track');
  assert.equal(pickSubtitleId(subs.filter(t => !t.forced), audio[1], prefs), null, 'English audio, no forced track: none');
  assert.equal(pickSubtitleId(subs, { index: 0 }, prefs), 'e0', 'unknown audio language: show them');
});

test('a default-flagged subtitle breaks ties', () => {
  const two = [{ id: 'a', language: 'en', source: 'embedded' }, { id: 'b', language: 'en', source: 'external', default: true }];
  assert.equal(pickSubtitleId(two, null, { subtitleMode: 'on', subtitleLanguage: 'en' }), 'b');
});

test('remuxing another audio track needs h264 video and aac or mp3 sound', () => {
  assert.equal(canRemuxAudio('h264', { codec: 'aac' }), true);
  assert.equal(canRemuxAudio('H264', { codec: 'MP3' }), true);
  assert.equal(canRemuxAudio('h264', { codec: 'ac3' }), false);
  assert.equal(canRemuxAudio('hevc', { codec: 'aac' }), false);
  assert.equal(canRemuxAudio('h264', undefined), false);
});

test('episode codes and the lines under Continue watching and Next up cards', () => {
  assert.equal(episodeCode({ kind: 'episode', seasonNumber: 2, episodeNumber: 5 }), 'S2E5');
  assert.equal(episodeCode({ kind: 'episode', seasonNumber: 2 }), 'S2');
  assert.equal(episodeCode({ kind: 'movie', seasonNumber: 2 }), '');
  assert.equal(episodeCode({}), '');
  assert.equal(resumeKicker({ kind: 'episode', seasonNumber: 1, episodeNumber: 3, positionSeconds: 2052 }), 'S1E3 · Resume at 34:12');
  assert.equal(resumeKicker({ positionSeconds: 3725 }), 'Resume at 1:02:05');
  assert.equal(resumeKicker({ studioName: 'Acme' }), 'Acme');
  assert.equal(nextUpKicker({ kind: 'episode', seasonNumber: 1, episodeNumber: 4, seriesName: 'The Show' }), 'S1E4 · The Show');
  assert.equal(nextUpKicker({ seriesName: 'Saga' }), 'Next · Saga');
});
