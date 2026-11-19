import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../../api', () => ({ getDVDs: vi.fn(), convertDVDs: vi.fn() }));
import { convertDVDs, getDVDs } from '../../api';
import { renderConsole } from '../testing';
import { DVDsPage } from './DVDsPage';

const disc = (over = {}) => ({
  id: 1, storageLocationId: 4, storageLocation: 'disk1', path: 'reelstash/DADDY_RIDE_ALONG/VIDEO_TS',
  name: 'DADDY_RIDE_ALONG', titles: 2, size: 43_000_000, mp4Ready: 0, mp4Total: 2, onDisk: true, ...over,
});

beforeEach(() => {
  vi.clearAllMocks();
  getDVDs.mockResolvedValue({ items: [] });
  convertDVDs.mockResolvedValue({ backgroundJobId: 9 });
});

describe('DVDsPage', () => {
  it('explains that a scan has to notice a disc before it can be converted', async () => {
    renderConsole(<DVDsPage />, { route: '/admin/dvds' });
    expect(await screen.findByText('No DVDs found')).toBeInTheDocument();
    expect(screen.getByText(/Scan a library that contains VIDEO_TS folders/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Convert' })).not.toBeInTheDocument();
  });

  it('converts a detected disc only after the admin confirms', async () => {
    getDVDs.mockResolvedValue({ items: [disc(), disc({ id: 2, name: 'OTHER', path: 'reelstash/OTHER/VIDEO_TS', mp4Ready: 1, mp4Total: 1, titles: 1 })] });
    renderConsole(<DVDsPage />, { route: '/admin/dvds' });
    expect(await screen.findByText('DADDY_RIDE_ALONG')).toBeInTheDocument();
    expect(screen.getByText('MP4 ready')).toBeInTheDocument();
    const ready = screen.getByText('OTHER').closest('tr');
    expect(within(ready).queryByRole('button', { name: 'Convert' })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: 'Convert' }));
    expect(convertDVDs).not.toHaveBeenCalled();
    const dialog = await screen.findByRole('alertdialog');
    expect(within(dialog).getByText(/The VOB files stay on disk/)).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Convert' }));
    await waitFor(() => expect(convertDVDs).toHaveBeenCalledWith([1]));
  });

  it('does not offer conversion for a disc that is no longer on disk', async () => {
    getDVDs.mockResolvedValue({ items: [disc({ onDisk: false, mp4Total: 2, mp4Ready: 0 })] });
    renderConsole(<DVDsPage />, { route: '/admin/dvds' });
    expect(await screen.findByText('Missing')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Convert' })).not.toBeInTheDocument();
  });
});
