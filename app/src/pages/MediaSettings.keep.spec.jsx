import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../api', async importOriginal => ({
  ...(await importOriginal()),
  getMediaItem: vi.fn(), updateMediaMetadata: vi.fn(), getCategories: vi.fn(() => Promise.resolve({ items: [] })),
}));
import { getMediaItem, updateMediaMetadata } from '../api';
import MediaSettings from './MediaSettings';
import { renderApp } from '../testing/render';

const item = { id: 7, title: 'Dunes of the South', description: 'Sand', files: [], cast: [{ id: 3, name: 'Maya Okafor' }], categories: ['Nature'], studioName: 'Halcyon Pictures', studioId: 2 };

beforeEach(() => {
  vi.clearAllMocks();
  updateMediaMetadata.mockResolvedValue({ nfoUpdated: 1, nfoFailed: 0 });
});

describe('title settings: Keep my edits', () => {
  it('turning it on keeps every field as it is', async () => {
    getMediaItem.mockResolvedValue(item);
    renderApp(<MediaSettings mediaItemId={7} open onOpenChange={() => {}} />);
    await userEvent.click(await screen.findByRole('checkbox', { name: /Keep my edits/ }));
    await userEvent.click(screen.getByRole('button', { name: /^Save/ }));
    await waitFor(() => expect(updateMediaMetadata).toHaveBeenCalled());
    expect(updateMediaMetadata).toHaveBeenCalledWith(7, expect.objectContaining({ keepEdits: true, title: 'Dunes of the South', description: 'Sand', categories: ['Nature'], studio: 'Halcyon Pictures' }));
  });

  it('shows as on for a title with kept fields, and turning it off clears them', async () => {
    getMediaItem.mockResolvedValue({ ...item, lockedFields: ['title'] });
    renderApp(<MediaSettings mediaItemId={7} open onOpenChange={() => {}} />);
    const box = await screen.findByRole('checkbox', { name: /Keep my edits/ });
    expect(box).toBeChecked();
    await userEvent.click(box);
    await userEvent.click(screen.getByRole('button', { name: /^Save/ }));
    await waitFor(() => expect(updateMediaMetadata).toHaveBeenCalledWith(7, { keepEdits: false }));
  });
});
