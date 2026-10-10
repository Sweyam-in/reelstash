import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../api', () => ({
  getTracks: vi.fn(), refreshTracks: vi.fn(), searchSubtitles: vi.fn(), downloadSubtitle: vi.fn(), deleteSubtitle: vi.fn(),
  getSubtitleProvider: vi.fn(), getPreferences: vi.fn(),
}));
vi.mock('../appLinks', () => ({ adminHref: path => path }));
import { deleteSubtitle, downloadSubtitle, getPreferences, getSubtitleProvider, getTracks, searchSubtitles } from '../api';
import MediaTracksTab from './MediaTracksTab';
import { renderApp } from '../testing/render';

const item = { id: 7, title: 'Some Movie', files: [{ id: 1, available: true }] };
const tracks = {
  audio: [{ index: 0, label: 'English · AAC Stereo', default: true }, { index: 1, label: 'Japanese · AAC 5.1', default: false }],
  subtitles: [
    { id: 'e0', source: 'embedded', codec: 'subrip', label: 'English' },
    { id: 'xabc', source: 'external', codec: 'srt', label: 'Spanish', fileName: 'Some Movie.es.srt' },
  ],
  hiddenSubtitles: 1,
};

beforeEach(() => {
  vi.clearAllMocks();
  getTracks.mockResolvedValue(tracks);
  getPreferences.mockResolvedValue({ languages: [{ code: 'en', label: 'English' }, { code: 'es', label: 'Spanish' }] });
  getSubtitleProvider.mockResolvedValue({ enabled: true, canDownload: true });
});

describe('Audio & subtitles tab', () => {
  it('lists audio tracks, subtitles and where each subtitle lives', async () => {
    renderApp(<MediaTracksTab item={item} />);
    expect(await screen.findByText('Japanese · AAC 5.1')).toBeInTheDocument();
    expect(screen.getByText('Inside the video · SUBRIP')).toBeInTheDocument();
    expect(screen.getByText('Some Movie.es.srt')).toBeInTheDocument();
    expect(screen.getByText(/stored as pictures/)).toBeInTheDocument();
    // Only files beside the video can be deleted.
    expect(screen.getAllByRole('button', { name: /^Delete / })).toHaveLength(1);
  });

  it('deletes a subtitle file after confirming', async () => {
    deleteSubtitle.mockResolvedValue(null);
    renderApp(<MediaTracksTab item={item} />);
    await userEvent.click(await screen.findByRole('button', { name: 'Delete Some Movie.es.srt' }));
    await userEvent.click(screen.getByRole('button', { name: 'Delete file' }));
    await waitFor(() => expect(deleteSubtitle).toHaveBeenCalledWith(7, 'xabc'));
    expect(getTracks).toHaveBeenCalledTimes(2);
  });

  it('searches OpenSubtitles and saves a result next to the video', async () => {
    searchSubtitles.mockResolvedValue({ canDownload: true, hashed: true, languages: ['en'], items: [
      { fileId: 22, language: 'en', release: 'Some.Movie.1080p', downloads: 1200, hashMatch: true, hearingImpaired: true },
    ] });
    downloadSubtitle.mockResolvedValue({ fileName: 'Some Movie.en.sdh.srt', remaining: 4, trackId: 'xdef' });
    renderApp(<MediaTracksTab item={item} />);
    expect(await screen.findByLabelText('Search OpenSubtitles')).toHaveValue('Some Movie');
    await userEvent.click(screen.getByRole('button', { name: 'Search' }));
    expect(await screen.findByText('Made for this file')).toBeInTheDocument();
    expect(searchSubtitles).toHaveBeenCalledWith(7, { q: 'Some Movie' });
    await userEvent.click(screen.getByRole('button', { name: /Download/ }));
    await waitFor(() => expect(downloadSubtitle).toHaveBeenCalledWith(7, { fileId: 22, language: 'en', hearingImpaired: true, foreignPartsOnly: undefined }));
    expect(await screen.findByText(/Saved as Some Movie.en.sdh.srt/)).toBeInTheDocument();
  });

  it('points to Providers when OpenSubtitles is not set up', async () => {
    getSubtitleProvider.mockResolvedValue({ enabled: false });
    renderApp(<MediaTracksTab item={item} />);
    expect(await screen.findByText(/Connect an OpenSubtitles.com account/)).toBeInTheDocument();
    expect(screen.queryByLabelText('Search OpenSubtitles')).not.toBeInTheDocument();
  });

  it('cannot download with only an API key', async () => {
    getSubtitleProvider.mockResolvedValue({ enabled: true, canDownload: false });
    renderApp(<MediaTracksTab item={item} />);
    expect(await screen.findByText(/To download, add your OpenSubtitles username and password/)).toBeInTheDocument();
  });
});
