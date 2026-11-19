import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../../api', () => ({ deleteTorrents: vi.fn(), moveTorrents: vi.fn(), createFileDir: vi.fn(), getFileLocations: vi.fn(), listFiles: vi.fn() }));
import { deleteTorrents } from '../../api';
import { DeleteTorrentsDialog } from './TorrentsPage';
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

describe('rule summaries', () => {
  it('describes conditions and what happens after the move', () => {
    expect(ruleConditions({})).toBe('every finished torrent');
    expect(ruleConditions({ matchCategory: 'movies', minSizeMb: 2048, videoOnly: true })).toBe('category is “movies”, at least 2.0 GB, has a video file');
    expect(ruleAfter({ scanAfter: true, removeAfter: 'never' })).toBe('scan the library');
    expect(ruleAfter({ scanAfter: false, removeAfter: 'seeded', removeRatio: 2, removeSeedMinutes: 120 })).toBe('remove after ratio 2 or 2 h seeding');
    expect(ruleAfter({ removeAfter: 'never' })).toBe('keep seeding');
  });
});
