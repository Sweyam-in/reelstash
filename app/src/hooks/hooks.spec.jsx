import { describe, it, expect, vi, afterEach } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { usePersistedState } from './usePersistedState';
import useRefetchOnFocus from './useRefetchOnFocus';
import { useDebounce } from '../pages/hooks/useDebounce';

describe('usePersistedState', () => {
  it('starts from the initial value when nothing is stored', () => {
    const { result } = renderHook(() => usePersistedState('studio', 'all'));
    expect(result.current[0]).toBe('all');
  });

  it('writes changes under a namespaced key', () => {
    const { result } = renderHook(() => usePersistedState('sort', 'recent'));
    act(() => result.current[1]('title'));
    expect(result.current[0]).toBe('title');
    expect(JSON.parse(localStorage.getItem('reelstash-filter:sort'))).toBe('title');
  });

  it('restores the stored value in a later mount', () => {
    localStorage.setItem('reelstash-filter:sort', JSON.stringify('rating'));
    const { result } = renderHook(() => usePersistedState('sort', 'recent'));
    expect(result.current[0]).toBe('rating');
  });

  it('round-trips objects and arrays', () => {
    const { result, unmount } = renderHook(() => usePersistedState('filters', { tags: [] }));
    act(() => result.current[1]({ tags: [1, 2], q: 'x' }));
    unmount();
    const again = renderHook(() => usePersistedState('filters', { tags: [] }));
    expect(again.result.current[0]).toEqual({ tags: [1, 2], q: 'x' });
  });

  it('keeps separate keys separate', () => {
    const a = renderHook(() => usePersistedState('a', 1));
    const b = renderHook(() => usePersistedState('b', 2));
    act(() => a.result.current[1](10));
    expect(b.result.current[0]).toBe(2);
  });

  it('falls back to the initial value when the stored JSON is corrupt', () => {
    localStorage.setItem('reelstash-filter:bad', '{oops');
    const { result } = renderHook(() => usePersistedState('bad', 'fallback'));
    expect(result.current[0]).toBe('fallback');
  });

  it('keeps working in memory when storage throws', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked'); });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked'); });
    const { result } = renderHook(() => usePersistedState('k', 'init'));
    expect(result.current[0]).toBe('init');
    act(() => result.current[1]('next'));
    expect(result.current[0]).toBe('next');
  });
});

describe('useRefetchOnFocus', () => {
  afterEach(() => { vi.useRealTimers(); Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true }); });

  function setup(minGap = 30000) {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-01-01T00:00:00Z'));
    const callback = vi.fn();
    const hook = renderHook(() => useRefetchOnFocus(callback, minGap));
    return { callback, ...hook };
  }

  it('does not fire when focus returns too soon', () => {
    const { callback } = setup();
    vi.advanceTimersByTime(5000);
    window.dispatchEvent(new Event('focus'));
    expect(callback).not.toHaveBeenCalled();
  });

  it('fires when the tab regains focus after the minimum gap', () => {
    const { callback } = setup();
    vi.advanceTimersByTime(31000);
    window.dispatchEvent(new Event('focus'));
    expect(callback).toHaveBeenCalledTimes(1);
  });

  it('also reacts to the tab becoming visible', () => {
    const { callback } = setup();
    vi.advanceTimersByTime(31000);
    document.dispatchEvent(new Event('visibilitychange'));
    expect(callback).toHaveBeenCalledTimes(1);
  });

  it('is rate limited: a second return inside the gap is ignored', () => {
    const { callback } = setup();
    vi.advanceTimersByTime(31000);
    window.dispatchEvent(new Event('focus'));
    vi.advanceTimersByTime(1000);
    window.dispatchEvent(new Event('focus'));
    expect(callback).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(31000);
    window.dispatchEvent(new Event('focus'));
    expect(callback).toHaveBeenCalledTimes(2);
  });

  it('ignores the tab being hidden', () => {
    const { callback } = setup();
    vi.advanceTimersByTime(31000);
    Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true });
    document.dispatchEvent(new Event('visibilitychange'));
    expect(callback).not.toHaveBeenCalled();
  });

  it('stops listening after unmount', () => {
    const { callback, unmount } = setup();
    unmount();
    vi.advanceTimersByTime(31000);
    window.dispatchEvent(new Event('focus'));
    expect(callback).not.toHaveBeenCalled();
  });

  it('always calls the latest callback', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-01-01T00:00:00Z'));
    const first = vi.fn(), second = vi.fn();
    const { rerender } = renderHook(({ cb }) => useRefetchOnFocus(cb, 1000), { initialProps: { cb: first } });
    rerender({ cb: second });
    vi.advanceTimersByTime(2000);
    window.dispatchEvent(new Event('focus'));
    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledTimes(1);
  });
});

describe('useDebounce', () => {
  afterEach(() => vi.useRealTimers());

  it('holds the old value until the delay passes', () => {
    vi.useFakeTimers();
    const { result, rerender } = renderHook(({ v }) => useDebounce(v, 250), { initialProps: { v: 'a' } });
    rerender({ v: 'ab' });
    expect(result.current).toBe('a');
    act(() => vi.advanceTimersByTime(249));
    expect(result.current).toBe('a');
    act(() => vi.advanceTimersByTime(1));
    expect(result.current).toBe('ab');
  });

  it('restarts the wait on every keystroke', () => {
    vi.useFakeTimers();
    const { result, rerender } = renderHook(({ v }) => useDebounce(v, 250), { initialProps: { v: '' } });
    for (const v of ['a', 'ab', 'abc']) {
      rerender({ v });
      act(() => vi.advanceTimersByTime(200));
    }
    expect(result.current).toBe('');
    act(() => vi.advanceTimersByTime(250));
    expect(result.current).toBe('abc');
  });
});
