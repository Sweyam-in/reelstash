import test from 'node:test';
import assert from 'node:assert/strict';
import {
  __resetVolumeProbeForTests, canElementFullscreen, canPictureInPicture, canSetVolume, isFullscreen, onFullscreenChange, toggleFullscreen,
} from './fullscreen.js';

const calls = [];
const spy = name => async function spied() { calls.push(name); };

test('standard API: frame goes fullscreen, second toggle exits', async () => {
  calls.length = 0;
  const frame = { requestFullscreen: spy('frame.request') };
  const doc = { fullscreenEnabled: true, fullscreenElement: null, exitFullscreen: spy('doc.exit') };
  await toggleFullscreen(frame, {}, doc);
  doc.fullscreenElement = frame;
  await toggleFullscreen(frame, {}, doc);
  assert.deepEqual(calls, ['frame.request', 'doc.exit']);
  assert.equal(isFullscreen(frame, {}, doc), true);
});

test('old Safari: falls back to the webkit-prefixed API', async () => {
  calls.length = 0;
  const frame = { webkitRequestFullscreen: spy('frame.webkitRequest') };
  const doc = { webkitFullscreenEnabled: true, webkitFullscreenElement: null, webkitExitFullscreen: spy('doc.webkitExit') };
  await toggleFullscreen(frame, {}, doc);
  doc.webkitFullscreenElement = frame;
  await toggleFullscreen(frame, {}, doc);
  assert.deepEqual(calls, ['frame.webkitRequest', 'doc.webkitExit']);
});

test('iPhone Safari: no element fullscreen, so the video itself is used', async () => {
  calls.length = 0;
  const frame = {}; // no requestFullscreen on iPhone
  const video = { webkitEnterFullscreen() { calls.push('video.enter'); }, webkitExitFullscreen() { calls.push('video.exit'); }, webkitDisplayingFullscreen: false };
  const doc = { fullscreenEnabled: false };
  assert.equal(canElementFullscreen(frame, doc), false);
  await toggleFullscreen(frame, video, doc);
  video.webkitDisplayingFullscreen = true;
  assert.equal(isFullscreen(frame, video, doc), true);
  await toggleFullscreen(frame, video, doc);
  assert.deepEqual(calls, ['video.enter', 'video.exit']);
});

test('a refused element request falls through to the video', async () => {
  calls.length = 0;
  const frame = { requestFullscreen: async () => { throw new Error('denied'); } };
  const video = { webkitEnterFullscreen() { calls.push('video.enter'); } };
  await toggleFullscreen(frame, video, { fullscreenEnabled: true, fullscreenElement: null });
  assert.deepEqual(calls, ['video.enter']);
});

test('nothing available is a quiet no-op', async () => {
  await toggleFullscreen({}, {}, { fullscreenEnabled: false });
});

test('onFullscreenChange listens on document and video, and cleans up', () => {
  const added = [];
  const removed = [];
  const doc = { addEventListener: n => added.push(`doc:${n}`), removeEventListener: n => removed.push(`doc:${n}`) };
  const video = { addEventListener: n => added.push(`video:${n}`), removeEventListener: n => removed.push(`video:${n}`) };
  const stop = onFullscreenChange(video, () => {}, doc);
  stop();
  assert.deepEqual(added.sort(), ['doc:fullscreenchange', 'doc:webkitfullscreenchange', 'video:webkitbeginfullscreen', 'video:webkitendfullscreen']);
  assert.deepEqual(removed.sort(), added.sort());
});

test('Picture-in-Picture detection', () => {
  assert.equal(canPictureInPicture(null, {}), false);
  assert.equal(canPictureInPicture({}, { pictureInPictureEnabled: true }), true);
  assert.equal(canPictureInPicture({ disablePictureInPicture: true }, { pictureInPictureEnabled: true }), false);
  assert.equal(canPictureInPicture({}, { pictureInPictureEnabled: false }), false); // Firefox
  const oldSafari = { webkitSetPresentationMode() {}, webkitSupportsPresentationMode: mode => mode === 'picture-in-picture' };
  assert.equal(canPictureInPicture(oldSafari, {}), true);
});

test('volume probe: settable on desktop, not on iOS', () => {
  __resetVolumeProbeForTests();
  assert.equal(canSetVolume(() => ({ volume: 1 })), true);
  __resetVolumeProbeForTests();
  assert.equal(canSetVolume(() => ({ get volume() { return 1; }, set volume(_) {} })), false);
  __resetVolumeProbeForTests();
  assert.equal(canSetVolume(() => { throw new Error('no DOM'); }), false);
});
