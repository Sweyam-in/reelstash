import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../api', async importOriginal => ({
  ...(await importOriginal()),
  getMediaItem: vi.fn(), setMediaReel: vi.fn(), getCategories: vi.fn(() => Promise.resolve({ items: [] })),
}));
import { getMediaItem, setMediaReel } from '../api';
import MediaSettings from './MediaSettings';
import { renderApp } from '../testing/render';

const item = { id: 7, title: 'Beach sunset', files: [], cast: [], categories: [], reel: true, reelDetected: true, reelOverride: null };

beforeEach(() => {
  vi.clearAllMocks();
  getMediaItem.mockResolvedValue(item);
});

describe('title settings: Reel tab', () => {
  it('shows the detection and keeps a title out of Reels by hand', async () => {
    setMediaReel.mockResolvedValue({ ...item, reel: false, reelOverride: false });
    renderApp(<MediaSettings mediaItemId={7} open onOpenChange={() => {}} initialTab="reel" />);
    expect(await screen.findByText(/Detected as a portrait video/)).toBeInTheDocument();
    expect(screen.getByRole('radio', { name: 'Automatic' })).toBeChecked();
    getMediaItem.mockResolvedValue({ ...item, reel: false, reelOverride: false });
    await userEvent.click(screen.getByRole('radio', { name: 'Never a reel' }));
    expect(setMediaReel).toHaveBeenCalledWith(7, false);
    await waitFor(() => expect(screen.getByRole('radio', { name: 'Never a reel' })).toBeChecked());
    expect(screen.getByText(/does not show under Reels/)).toBeInTheDocument();
  });

  it('goes back to automatic', async () => {
    getMediaItem.mockResolvedValue({ ...item, reelOverride: true });
    setMediaReel.mockResolvedValue(item);
    renderApp(<MediaSettings mediaItemId={7} open onOpenChange={() => {}} initialTab="reel" />);
    await userEvent.click(await screen.findByRole('radio', { name: 'Automatic' }));
    expect(setMediaReel).toHaveBeenCalledWith(7, null);
  });
});
