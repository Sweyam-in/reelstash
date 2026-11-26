import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../../api', () => ({ deleteTorrents: vi.fn(), moveTorrents: vi.fn(), createFileDir: vi.fn(), getFileLocations: vi.fn(), listFiles: vi.fn() }));
import { deleteTorrents } from '../../api';
import TorrentsPage, { DeleteTorrentsDialog } from './TorrentsPage';
import { DownloadsContext } from './context';
import { ruleAfter, ruleConditions } from './MoveRulesPage';
import { renderApp } from '../../testing/render';

beforeEach(() => { vi.clearAllMocks(); deleteTorrents.mockResolvedValue({ ok: true }); });

describe('DeleteTorrentsDialog', () => {
  const torrents = [{ hash: 'a', name: 'Film', library: { id: 1, name: 'Movies' } }, { hash: 'b', name: 'Other' }];

  it('keeps files by default and warns about library titles before deleting files', async () => {
    const onDone = vi.fn();
    renderApp(<DeleteTorrentsDialog torrents={torrents} onClose={() => {}} onDone={onDone} />);
    expect(await screen.findByText('Remove 2 torrents?')).toBeInTheDocument();
    expect(screen.queryByText(/in your library/)).not.toBeInTheDocument();
    await userEvent.click(screen.getByLabelText('Also delete the downloaded files'));
    expect(screen.getByText(/“Film” is in your library “Movies”/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Remove and delete files' }));
    expect(deleteTorrents).toHaveBeenCalledWith(['a', 'b'], true);
    expect(onDone).toHaveBeenCalled();
  });

  it('removes without files', async () => {
    renderApp(<DeleteTorrentsDialog torrents={[torrents[1]]} onClose={() => {}} onDone={() => {}} />);
    await userEvent.click(await screen.findByRole('button', { name: 'Remove' }));
    expect(deleteTorrents).toHaveBeenCalledWith(['b'], false);
  });
});

describe('torrent list sorting', () => {
  const torrents = [
    { hash: 's', name: 'Slow', state: 'downloading', progress: 0.5, dlspeed: 100, upspeed: 0, size: 1, added_on: 1, eta: 10, ratio: 0.1, num_seeds: 1, num_leechs: 0 },
    { hash: 'f', name: 'Fast', state: 'stalledDL', progress: 0.5, dlspeed: '8000', upspeed: 0, size: 2, added_on: 2, eta: 8640000, ratio: 0.2, num_seeds: 0, num_leechs: 4 },
    { hash: 'e', name: 'Broken', state: 'error', progress: 0.5, dlspeed: 0, upspeed: 0, size: 3, added_on: 3, eta: -1, ratio: -1, num_seeds: 0, num_leechs: 0 },
  ];

  function renderList() {
    return renderApp(
      <DownloadsContext.Provider value={{
        snap: { connected: true, categories: [], tags: [], server: {}, warnings: [] },
        error: '', torrents, reload: vi.fn(), openCategory: vi.fn(),
      }}>
        <TorrentsPage filter="all" category="" tag="" onAdd={() => {}} />
      </DownloadsContext.Provider>,
    );
  }

  function names() {
    return screen.getAllByRole('row').slice(1).map(row => row.querySelector('.rs-dl-name').textContent);
  }

  it('sorts by the status you see and by download speed, and keeps the column on later clicks', async () => {
    const user = userEvent.setup();
    renderList();
    expect(names()).toEqual(['Broken', 'Fast', 'Slow']);
    await user.click(screen.getByRole('button', { name: 'Status' }));
    expect(names()).toEqual(['Slow', 'Fast', 'Broken']);
    await user.click(screen.getByRole('button', { name: 'Down' }));
    expect(names()).toEqual(['Fast', 'Slow', 'Broken']);
    await user.click(screen.getByRole('button', { name: 'Down' }));
    expect(names()).toEqual(['Broken', 'Slow', 'Fast']);
    await user.click(screen.getByRole('button', { name: 'Down' }));
    expect(names()).toEqual(['Fast', 'Slow', 'Broken']);
    expect(screen.getByRole('button', { name: 'Down' })).toHaveAttribute('aria-sort', 'descending');
  });
});

describe('rule summaries', () => {
  it('describes conditions and what happens after the move', () => {
    expect(ruleConditions({})).toBe('every finished torrent');
    expect(ruleConditions({ matchCategory: 'movies', minSizeMb: 2048, videoOnly: true })).toBe('category is “movies”, at least 2.0 GB, has a video file');
    expect(ruleAfter({ scanAfter: true, removeAfter: 'never' })).toBe('scan the library');
    expect(ruleAfter({ scanAfter: false, removeAfter: 'seeded', removeRatio: 2, removeSeedMinutes: 120 })).toBe('remove after ratio 2 or 2 h seeding');
    expect(ruleAfter({ removeAfter: 'never' })).toBe('keep seeding');
  });
});
