import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';

vi.mock('../../api', () => ({ getPlaybackInfo: vi.fn() }));
vi.mock('hls.js', () => ({ default: class { static isSupported() { return false; } } }));

import { getPlaybackInfo } from '../../api';
import { useAdaptiveSource } from './useAdaptiveSource';

// jsdom has no MediaError or media playback; give the hook a stand-in <video>.
function fakeVideo() {
  const video = document.createElement('video');
  video.load = vi.fn();
  Object.defineProperty(video, 'currentTime', { value: 100, writable: true });
  Object.defineProperty(video, 'duration', { value: 1000, configurable: true });
  Object.defineProperty(video, 'paused', { value: false, configurable: true });
  return video;
}
const failWith = (video, code) => {
  Object.defineProperty(video, 'error', { value: { code }, configurable: true });
  act(() => { video.dispatchEvent(new Event('error')); });
};

describe('useAdaptiveSource', () => {
  beforeEach(() => {
    globalThis.MediaError = { MEDIA_ERR_NETWORK: 2, MEDIA_ERR_DECODE: 3, MEDIA_ERR_SRC_NOT_SUPPORTED: 4 };
    getPlaybackInfo.mockResolvedValue({ directPlay: true, transcoding: true });
    // jsdom's canPlayType always answers "", which would force hls.
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
  });
  afterEach(() => { delete globalThis.MediaError; });

  function setup() {
    const video = fakeVideo();
    const onFatal = vi.fn();
    const hook = renderHook(() => useAdaptiveSource({ current: video }, 7, true, onFatal));
    return { video, onFatal, hook };
  }

  it('starts on the original file', async () => {
    const { hook, video } = setup();
    await waitFor(() => expect(hook.result.current.mode).toBe('direct'));
    expect(video.src).toContain('/api/media/7/stream');
  });

  it('skips past a decode error instead of switching to a transcode', async () => {
    const { hook, video, onFatal } = setup();
    await waitFor(() => expect(hook.result.current.mode).toBe('direct'));
    failWith(video, 3);
    await waitFor(() => expect(hook.result.current.skips).toBe(1));
    expect(hook.result.current.mode).toBe('direct');
    expect(hook.result.current.resumeAt.current).toBe(102);
    expect(hook.result.current.resumePlayback.current).toBe(true);
    expect(onFatal).not.toHaveBeenCalled();
  });

  it('jumps further on repeated failures, then hands the choice to the user', async () => {
    const { hook, video, onFatal } = setup();
    await waitFor(() => expect(hook.result.current.mode).toBe('direct'));
    const targets = [];
    for (let i = 0; i < 6; i += 1) {
      failWith(video, 3);
      targets.push(hook.result.current.resumeAt.current);
      video.currentTime = hook.result.current.resumeAt.current; // where the reload would land
    }
    expect(targets).toEqual([102, 107, 122, 152, 212, 272]);
    expect(onFatal).not.toHaveBeenCalled();
    failWith(video, 3);
    expect(onFatal).toHaveBeenCalledTimes(1);
    expect(hook.result.current.mode).toBe('direct');
  });

  it('switches to a transcode only when asked, keeping the position', async () => {
    const { hook } = setup();
    await waitFor(() => expect(hook.result.current.mode).toBe('direct'));
    act(() => hook.result.current.setTranscode(true));
    expect(hook.result.current.mode).toBe('hls');
    expect(hook.result.current.resumeAt.current).toBe(100);
    act(() => hook.result.current.setTranscode(false));
    expect(hook.result.current.mode).toBe('direct');
  });

  it('does not play a file that cannot start at all, it reports it', async () => {
    const { video, onFatal, hook } = setup();
    await waitFor(() => expect(hook.result.current.mode).toBe('direct'));
    video.currentTime = 0;
    failWith(video, 4);
    expect(onFatal).toHaveBeenCalledTimes(1);
    expect(hook.result.current.skips).toBe(0);
  });
});

describe('transcode quality', () => {
  beforeEach(() => {
    getPlaybackInfo.mockResolvedValue({ directPlay: true, transcoding: true });
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
    localStorage.clear();
  });

  it('remembers the chosen quality and keeps the position when it changes mid-transcode', async () => {
    const video = fakeVideo();
    const hook = renderHook(() => useAdaptiveSource({ current: video }, 7, true, vi.fn()));
    await waitFor(() => expect(hook.result.current.mode).toBe('direct'));
    expect(hook.result.current.quality).toBe('balanced');
    act(() => hook.result.current.setTranscode(true));
    act(() => hook.result.current.setQuality('low'));
    expect(hook.result.current.quality).toBe('low');
    expect(hook.result.current.resumeAt.current).toBe(100);
    expect(localStorage.getItem('reelstash.transcodeQuality')).toBe('low');
    act(() => hook.result.current.setQuality('bogus'));
    expect(hook.result.current.quality).toBe('low');
  });

  it('starts from the saved quality', async () => {
    localStorage.setItem('reelstash.transcodeQuality', 'high');
    const video = fakeVideo();
    const hook = renderHook(() => useAdaptiveSource({ current: video }, 7, true, vi.fn()));
    expect(hook.result.current.quality).toBe('high');
  });
});

describe('audio track selection', () => {
  beforeEach(() => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
    localStorage.clear();
  });
  const render = (video, audio) => renderHook(({ track }) => useAdaptiveSource({ current: video }, 7, true, vi.fn(), track), { initialProps: { track: audio } });

  it('plays the original file untouched for the first audio track', async () => {
    getPlaybackInfo.mockResolvedValue({ directPlay: true, transcoding: true, videoCodec: 'h264' });
    const video = fakeVideo();
    const hook = render(video, null);
    await waitFor(() => expect(hook.result.current.mode).toBe('direct'));
    expect(video.src).toMatch(/\/api\/media\/7\/stream$/);
  });

  it('asks for a cheap remux of another track when the codecs allow it', async () => {
    getPlaybackInfo.mockResolvedValue({ directPlay: true, transcoding: true, videoCodec: 'h264' });
    const video = fakeVideo();
    const hook = render(video, { index: 1, codec: 'aac' });
    await waitFor(() => expect(hook.result.current.mode).toBe('direct'));
    expect(video.src).toContain('/api/media/7/stream?audio=1');
    expect(hook.result.current.audioForcesTranscode).toBe(false);
  });

  it('transcodes a track the browser cannot be handed cheaply, and says why', async () => {
    getPlaybackInfo.mockResolvedValue({ directPlay: true, transcoding: true, videoCodec: 'h264' });
    const video = fakeVideo();
    const hook = render(video, { index: 2, codec: 'ac3' });
    await waitFor(() => expect(hook.result.current.mode).toBe('hls'));
    expect(hook.result.current.audioForcesTranscode).toBe(true);
    expect(video.src).toContain('/api/media/7/hls/index.m3u8?audio=2');
  });

  it('never attaches the wrong source while the track changes, and goes back to direct play afterwards', async () => {
    getPlaybackInfo.mockResolvedValue({ directPlay: true, transcoding: true, videoCodec: 'h264' });
    const video = fakeVideo();
    const hook = render(video, null);
    await waitFor(() => expect(hook.result.current.mode).toBe('direct'));
    const sources = [];
    const watch = vi.spyOn(video, 'src', 'set');
    act(() => hook.result.current.prepareSourceChange());
    hook.rerender({ track: { index: 1, codec: 'dts' } });
    await waitFor(() => expect(hook.result.current.mode).toBe('hls'));
    watch.mock.calls.forEach(([url]) => sources.push(url));
    expect(sources.every(url => !url.includes('/stream?audio='))).toBe(true);
    expect(hook.result.current.resumeAt.current).toBe(100);
    hook.rerender({ track: null });
    await waitFor(() => expect(hook.result.current.mode).toBe('direct'));
    expect(video.src).toMatch(/\/stream$/);
  });

  it('keeps a transcode the person asked for when the audio track changes', async () => {
    getPlaybackInfo.mockResolvedValue({ directPlay: true, transcoding: true, videoCodec: 'h264' });
    const video = fakeVideo();
    const hook = render(video, null);
    await waitFor(() => expect(hook.result.current.mode).toBe('direct'));
    act(() => hook.result.current.setTranscode(true));
    hook.rerender({ track: { index: 1, codec: 'aac' } });
    expect(hook.result.current.mode).toBe('hls');
    expect(video.src).toContain('audio=1');
  });
});
