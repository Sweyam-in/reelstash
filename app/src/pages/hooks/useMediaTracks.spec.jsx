import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';

vi.mock('../../api', () => ({ getTracks: vi.fn(), getPreferences: vi.fn() }));
import { getPreferences, getTracks } from '../../api';
import { resetPreferenceCache, useMediaTracks } from './useMediaTracks';

const tracks = {
  videoCodec: 'h264',
  audio: [{ index: 0, language: 'ja', codec: 'aac', default: true, label: 'Japanese' }, { index: 1, language: 'en', codec: 'aac', label: 'English' }],
  subtitles: [{ id: 'e0', source: 'embedded', language: 'en', label: 'English' }, { id: 'x1', source: 'external', language: 'es', label: 'Spanish' }],
};

beforeEach(() => {
  resetPreferenceCache();
  getTracks.mockResolvedValue(tracks);
  getPreferences.mockResolvedValue({ audioLanguage: null, subtitleLanguage: null, subtitleMode: 'off' });
});
afterEach(() => vi.useRealTimers());

describe('useMediaTracks', () => {
  it('waits for the tracks, then starts from the language defaults', async () => {
    getPreferences.mockResolvedValue({ audioLanguage: 'en', subtitleLanguage: 'es', subtitleMode: 'on' });
    const { result } = renderHook(() => useMediaTracks(7, true));
    expect(result.current.ready).toBe(false);
    await waitFor(() => expect(result.current.ready).toBe(true));
    expect(result.current.audioIndex).toBe(1);
    expect(result.current.audioSelection).toEqual({ index: 1, codec: 'aac' });
    expect(result.current.subtitleId).toBe('x1');
  });

  it('plays the file as it is with no preferences, and offers the tracks anyway', async () => {
    const { result } = renderHook(() => useMediaTracks(7, true));
    await waitFor(() => expect(result.current.ready).toBe(true));
    expect(result.current.audioIndex).toBe(0);
    expect(result.current.audioSelection).toBeNull();
    expect(result.current.subtitleId).toBeNull();
    expect(result.current.audio).toHaveLength(2);
    expect(result.current.subtitles).toHaveLength(2);
  });

  it('turns subtitles on for foreign audio only', async () => {
    getPreferences.mockResolvedValue({ audioLanguage: null, subtitleLanguage: 'en', subtitleMode: 'foreign' });
    const { result } = renderHook(() => useMediaTracks(7, true));
    await waitFor(() => expect(result.current.ready).toBe(true));
    expect(result.current.subtitleId).toBe('e0'); // Japanese audio plays by default
  });

  it('does nothing until enabled, and starts over for another title', async () => {
    const { result, rerender } = renderHook(({ id, on }) => useMediaTracks(id, on), { initialProps: { id: 7, on: false } });
    expect(result.current.ready).toBe(false);
    expect(getTracks).not.toHaveBeenCalled();
    rerender({ id: 7, on: true });
    await waitFor(() => expect(result.current.ready).toBe(true));
    rerender({ id: 8, on: true });
    expect(result.current.ready).toBe(false);
    expect(result.current.subtitles).toEqual([]);
    await waitFor(() => expect(result.current.ready).toBe(true));
    expect(getTracks).toHaveBeenLastCalledWith(8);
  });

  it('toggles subtitles off and back to the last one used', async () => {
    const { result } = renderHook(() => useMediaTracks(7, true));
    await waitFor(() => expect(result.current.ready).toBe(true));
    act(() => { result.current.toggleSubtitles(); });
    expect(result.current.subtitleId).toBe('e0'); // nothing used yet: the first
    act(() => result.current.setSubtitleId('x1'));
    act(() => { result.current.toggleSubtitles(); });
    expect(result.current.subtitleId).toBeNull();
    act(() => { result.current.toggleSubtitles(); });
    expect(result.current.subtitleId).toBe('x1');
  });

  it('reports false from toggleSubtitles when there are none', async () => {
    getTracks.mockResolvedValue({ ...tracks, subtitles: [] });
    const { result } = renderHook(() => useMediaTracks(7, true));
    await waitFor(() => expect(result.current.ready).toBe(true));
    let handled;
    act(() => { handled = result.current.toggleSubtitles(); });
    expect(handled).toBe(false);
  });

  it('starts the player without track information when the server is slow, and keeps the first audio track', async () => {
    vi.useFakeTimers();
    let release;
    getTracks.mockReturnValue(new Promise(resolve => { release = resolve; }));
    getPreferences.mockResolvedValue({ audioLanguage: 'en', subtitleLanguage: 'en', subtitleMode: 'on' });
    const { result } = renderHook(() => useMediaTracks(7, true));
    await act(async () => { await vi.advanceTimersByTimeAsync(1600); });
    expect(result.current.ready).toBe(true);
    expect(result.current.audioIndex).toBe(0);
    await act(async () => { release(tracks); });
    expect(result.current.subtitles).toHaveLength(2);
    expect(result.current.audioIndex).toBe(0); // audio is not switched once playback may have begun
    expect(result.current.subtitleId).toBe('e0'); // text can still be shown
  });

  it('survives the tracks request failing', async () => {
    getTracks.mockRejectedValue(new Error('boom'));
    const { result } = renderHook(() => useMediaTracks(7, true));
    await waitFor(() => expect(result.current.ready).toBe(true));
    expect(result.current.audio).toEqual([]);
  });
});
