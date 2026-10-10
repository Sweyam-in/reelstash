import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes } from 'react-router-dom';

vi.mock('../api', () => ({
  getCollections: vi.fn(), createCollection: vi.fn(), getCollection: vi.fn(), updateCollection: vi.fn(), deleteCollection: vi.fn(),
  setCollectionPinned: vi.fn(), addCollectionItem: vi.fn(), moveCollectionItem: vi.fn(), removeCollectionItem: vi.fn(), getMediaItems: vi.fn(),
  getTags: vi.fn(), getCategories: vi.fn(), artworkUrl: () => '/art.jpg',
}));
vi.mock('../virtual', async importOriginal => {
  const actual = await importOriginal();
  // jsdom has no layout, so the real virtual window would render nothing.
  return { ...actual, VirtualWindow: ({ items, renderItem, className }) => <div className={className}>{items.map((item, i) => <React.Fragment key={item.id}>{renderItem(item, i)}</React.Fragment>)}</div> };
});
import { createCollection, getCategories, getCollection, getCollections, getTags, updateCollection } from '../api';
import { PlaylistDetail, Playlists } from './Playlists';
import { renderApp } from '../testing/render';

beforeEach(() => {
  vi.clearAllMocks();
  getTags.mockResolvedValue({ items: [{ id: 1, name: 'Marvel' }] });
  getCategories.mockResolvedValue({ items: [] });
  createCollection.mockResolvedValue({ id: 9 });
  updateCollection.mockResolvedValue({ id: 3 });
  getCollections.mockResolvedValue({ items: [
    { id: 1, name: 'Weekend', kind: 'MANUAL', mediaCount: 2, pinned: false, covers: [] },
    { id: 2, name: 'Unwatched Marvel', kind: 'SMART', mediaCount: 5, pinned: false, covers: [], rules: { match: 'all', conditions: [{ field: 'watched', value: false }] } },
  ] });
});

describe('Playlists page', () => {
  it('lists manual and smart playlists, marking the smart ones', async () => {
    renderApp(<Playlists />);
    expect(await screen.findByText('Weekend')).toBeInTheDocument();
    const smart = screen.getByText('Unwatched Marvel').closest('strong');
    expect(within(smart).getByText('Smart')).toBeInTheDocument();
    expect(within(screen.getByText('Weekend').closest('strong')).queryByText('Smart')).not.toBeInTheDocument();
  });

  it('creates a smart playlist from rules', async () => {
    renderApp(<Routes><Route path="/" element={<Playlists />} /><Route path="/playlists/:id" element={<p>detail page</p>} /></Routes>);
    await userEvent.click((await screen.findAllByRole('button', { name: /new playlist/i }))[0]);
    const dialog = await screen.findByRole('dialog');
    await userEvent.click(within(dialog).getByRole('radio', { name: /smart/i }));
    await userEvent.type(within(dialog).getByLabelText('Name'), 'Good and new');
    const create = within(dialog).getByRole('button', { name: 'Create playlist' });
    await userEvent.selectOptions(within(dialog).getByLabelText('Condition 1 field'), 'rating');
    await userEvent.selectOptions(within(dialog).getByLabelText('Condition 1 operator'), 'gte');
    await userEvent.click(within(dialog).getByRole('button', { name: /add condition/i }));
    await userEvent.selectOptions(within(dialog).getByLabelText('Condition 2 field'), 'addedWithin');
    // A blank value is caught before anything is sent.
    await userEvent.clear(within(dialog).getByLabelText('Condition 2 value'));
    expect(within(dialog).getByRole('alert')).toHaveTextContent(/whole number/);
    expect(create).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText('Condition 2 value'), '30');
    expect(within(dialog).queryByRole('alert')).not.toBeInTheDocument();
    await userEvent.click(create);
    await waitFor(() => expect(createCollection).toHaveBeenCalledWith({
      name: 'Good and new', description: '', kind: 'SMART',
      rules: { match: 'all', sort: 'added', conditions: [{ field: 'rating', op: 'gte', value: 4 }, { field: 'addedWithin', value: 30 }] },
    }));
    expect(await screen.findByText('detail page')).toBeInTheDocument();
  });

  it('still creates a manual playlist', async () => {
    renderApp(<Routes><Route path="/" element={<Playlists />} /><Route path="/playlists/:id" element={<p>detail page</p>} /></Routes>);
    await userEvent.click((await screen.findAllByRole('button', { name: /new playlist/i }))[0]);
    const dialog = await screen.findByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText('Name'), 'Mine');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Create playlist' }));
    await waitFor(() => expect(createCollection).toHaveBeenCalledWith({ name: 'Mine', description: '', kind: 'MANUAL' }));
  });
});

describe('Smart playlist page', () => {
  const smart = { id: 3, name: 'Unwatched Marvel', kind: 'SMART', pinned: false, description: '', rules: { match: 'any', limit: 10, conditions: [{ field: 'tag', op: 'is', value: 'Marvel' }, { field: 'watched', value: false }] } };
  beforeEach(() => {
    getCollection.mockResolvedValue({ collection: smart, items: [{ id: 11, title: 'Iron Man', studioName: 'Marvel' }], total: 1, limit: 30, offset: 0 });
  });
  const renderDetail = () => renderApp(<Routes><Route path="/playlists/:id" element={<PlaylistDetail />} /></Routes>, { route: '/playlists/3' });

  it('shows its rules, and no controls for picking or ordering titles', async () => {
    renderDetail();
    expect(await screen.findByText('Iron Man')).toBeInTheDocument();
    expect(screen.getByText('Tag is “Marvel”')).toBeInTheDocument();
    expect(screen.getByText('Not watched yet')).toBeInTheDocument();
    expect(screen.getByText(/matches any of these · first 10/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /edit rules/i })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /add titles/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /move iron man/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /remove iron man/i })).not.toBeInTheDocument();
  });

  it('edits the rules and saves them', async () => {
    renderDetail();
    await userEvent.click(await screen.findByRole('button', { name: /edit rules/i }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByLabelText('Condition 1 value')).toHaveValue('Marvel');
    await userEvent.selectOptions(within(dialog).getByLabelText('Match all or any'), 'all');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Remove condition 2' }));
    await userEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(updateCollection).toHaveBeenCalledWith('3', {
      name: 'Unwatched Marvel', description: '',
      rules: { match: 'all', sort: 'added', limit: 10, conditions: [{ field: 'tag', op: 'is', value: 'Marvel' }] },
    }));
  });

  it('explains an empty smart playlist', async () => {
    getCollection.mockResolvedValue({ collection: smart, items: [], total: 0, limit: 30, offset: 0 });
    renderDetail();
    expect(await screen.findByText(/nothing in your library matches these rules/i)).toBeInTheDocument();
  });
});
