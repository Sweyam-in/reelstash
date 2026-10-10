import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../api', () => ({
  getMediaItems: vi.fn(), updateMediaItem: vi.fn(), updatePlayback: vi.fn(() => Promise.resolve()),
  artworkUrl: id => `/art/${id}.jpg`,
}));
// Playback itself is the shared adaptive-source hook's job, tested on its own.
vi.mock('./hooks/useAdaptiveSource', () => ({ useAdaptiveSource: () => ({ mode: 'direct' }) }));
import { getMediaItems, updateMediaItem } from '../api';
import Reels from './Reels';
import { renderApp } from '../testing/render';

const reels = [
  { id: 1, title: 'Beach sunset', studioName: 'Phone', performers: ['Asha'], favorite: false },
  { id: 2, title: 'Night market', favorite: false },
];

beforeEach(() => {
  vi.clearAllMocks();
  Element.prototype.scrollTo ??= function scrollTo() {};
  getMediaItems.mockResolvedValue({ items: reels, total: 2 });
});

describe('Reels page', () => {
  it('asks for the library’s reels in a stable shuffle and plays the first one', async () => {
    const { container } = renderApp(<Reels />);
    expect(await screen.findByText('Beach sunset')).toBeInTheDocument();
    expect(screen.getByText('Phone · Asha')).toBeInTheDocument();
    const params = getMediaItems.mock.calls[0][0];
    expect(params).toMatchObject({ reels: 'true', sort: 'shuffle', offset: 0 });
    expect(params.seed).toBeTruthy();
    // Only the reel on screen has a player; the rest show a still.
    expect(container.querySelectorAll('video')).toHaveLength(1);
    expect(container.querySelector('.rs-reel.is-active')).toHaveAttribute('aria-label', 'Beach sunset');
  });

  it('switches to newest first', async () => {
    renderApp(<Reels />);
    await screen.findByText('Beach sunset');
    await userEvent.click(screen.getByRole('button', { name: /Newest/ }));
    await waitFor(() => expect(getMediaItems).toHaveBeenLastCalledWith(expect.objectContaining({ reels: 'true', sort: '' })));
  });

  it('favorites a reel', async () => {
    updateMediaItem.mockResolvedValue({ id: 1, favorite: true });
    renderApp(<Reels />);
    await screen.findByText('Beach sunset');
    await userEvent.click(screen.getAllByRole('button', { name: 'Add to favorites' })[0]);
    expect(updateMediaItem).toHaveBeenCalledWith(1, { favorite: true });
    expect(await screen.findByRole('button', { name: 'Remove from favorites' })).toHaveAttribute('aria-pressed', 'true');
  });

  it('explains an empty library', async () => {
    getMediaItems.mockResolvedValue({ items: [], total: 0 });
    renderApp(<Reels />);
    expect(await screen.findByText('No reels yet')).toBeInTheDocument();
  });
});
