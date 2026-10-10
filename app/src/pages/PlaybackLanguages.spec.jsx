import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../api', () => ({ getPreferences: vi.fn(), updatePreferences: vi.fn(), setAppLockSettings: vi.fn() }));
vi.mock('../components/AppLock', () => ({ useAppLock: () => ({}) }));
vi.mock('../libraryContext', () => ({ useLibrary: () => ({ libraries: [] }) }));
import { getPreferences, updatePreferences } from '../api';
import { PlaybackLanguages } from './UserSettings';
import { renderApp } from '../testing/render';

const languages = [{ code: 'en', label: 'English' }, { code: 'ja', label: 'Japanese' }, { code: 'es', label: 'Spanish' }];

beforeEach(() => {
  vi.clearAllMocks();
  getPreferences.mockResolvedValue({ audioLanguage: null, subtitleLanguage: null, subtitleMode: 'off', languages });
  updatePreferences.mockResolvedValue({});
});

describe('audio and subtitle preferences', () => {
  it('shows the saved choices', async () => {
    getPreferences.mockResolvedValue({ audioLanguage: 'ja', subtitleLanguage: 'en', subtitleMode: 'foreign', languages });
    renderApp(<PlaybackLanguages />);
    expect(await screen.findByLabelText('Audio language')).toHaveValue('ja');
    expect(screen.getByLabelText('Subtitle language')).toHaveValue('en');
    expect(screen.getByRole('radio', { name: /only|another language/i, checked: true })).toBeInTheDocument();
  });

  it('saves a new audio language', async () => {
    renderApp(<PlaybackLanguages />);
    await userEvent.selectOptions(await screen.findByLabelText('Audio language'), 'ja');
    await waitFor(() => expect(updatePreferences).toHaveBeenCalledWith({ audioLanguage: 'ja', subtitleLanguage: '', subtitleMode: 'off' }));
    expect(await screen.findByText('Language settings saved')).toBeInTheDocument();
  });

  it('turning subtitles on picks a language to show, starting from the audio language', async () => {
    getPreferences.mockResolvedValue({ audioLanguage: 'ja', subtitleLanguage: null, subtitleMode: 'off', languages });
    renderApp(<PlaybackLanguages />);
    await screen.findByLabelText('Audio language');
    expect(screen.getByLabelText('Subtitle language')).toBeDisabled();
    await userEvent.click(screen.getByRole('radio', { name: /always/i }));
    await waitFor(() => expect(updatePreferences).toHaveBeenCalledWith({ audioLanguage: 'ja', subtitleLanguage: 'ja', subtitleMode: 'on' }));
  });

  it('puts the choice back when saving fails', async () => {
    updatePreferences.mockRejectedValue(new Error('nope'));
    renderApp(<PlaybackLanguages />);
    const select = await screen.findByLabelText('Audio language');
    await userEvent.selectOptions(select, 'es');
    expect(await screen.findByText('nope')).toBeInTheDocument();
    expect(select).toHaveValue('');
  });
});
