import React from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act, fireEvent } from '@testing-library/react';
import UpNext from './UpNext';

const item = { id: 12, title: 'The Long Night', seriesName: 'The Show', kind: 'episode', seasonNumber: 1, episodeNumber: 4 };

describe('UpNext', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('names the next episode and counts down to playing it, once', () => {
    const onPlay = vi.fn();
    render(<UpNext item={item} seconds={3} onPlay={onPlay} onCancel={vi.fn()} />);
    expect(screen.getByText('The Long Night')).toBeInTheDocument();
    expect(screen.getByText('S1E4 · The Show')).toBeInTheDocument();
    expect(screen.getByText('Up next in 3s')).toBeInTheDocument();
    act(() => { vi.advanceTimersByTime(2000); });
    expect(onPlay).not.toHaveBeenCalled();
    act(() => { vi.advanceTimersByTime(5000); });
    expect(onPlay).toHaveBeenCalledTimes(1);
  });

  it('plays immediately on request, without playing a second time when the timer runs out', () => {
    const onPlay = vi.fn();
    render(<UpNext item={item} seconds={2} onPlay={onPlay} onCancel={vi.fn()} />);
    fireEvent.click(screen.getByRole('button', { name: /play now/i }));
    act(() => { vi.advanceTimersByTime(5000); });
    expect(onPlay).toHaveBeenCalledTimes(1);
  });

  it('cancels from the button and from Escape', () => {
    const onCancel = vi.fn();
    render(<UpNext item={item} onPlay={vi.fn()} onCancel={onCancel} />);
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }));
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(onCancel).toHaveBeenCalledTimes(2);
  });
});
