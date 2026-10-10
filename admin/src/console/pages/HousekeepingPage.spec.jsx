import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../../api', () => ({ cancelJob: vi.fn(), getHousekeeping: vi.fn(), runHousekeeping: vi.fn(), updateHousekeepingSettings: vi.fn() }));
vi.mock('../../events', () => ({ useEventsConnected: () => false, useServerEvent: () => {} }));
import { getHousekeeping, runHousekeeping, updateHousekeepingSettings } from '../../api';
import { renderConsole } from '../testing';
import { HousekeepingPage } from './HousekeepingPage';

const data = {
  tasks: [
    { id: 'thumbs', group: 'files', title: 'Unused thumbnails', description: 'No title uses them.', default: true },
    { id: 'empty-dirs', group: 'library', title: 'Empty folders', description: 'Folders with no videos.', default: false },
  ],
  settings: { jobDays: 30, notificationDays: 30, capacityDays: 365, cacheDays: 14 }, defaults: {}, active: null, lastAnalyze: null, lastCleanup: null, storage: [],
};

beforeEach(() => {
  vi.clearAllMocks();
  getHousekeeping.mockResolvedValue(data);
  runHousekeeping.mockResolvedValue({ jobId: 4 });
  updateHousekeepingSettings.mockResolvedValue({});
});

describe('HousekeepingPage', () => {
  it('analyzes everything without asking, since nothing is removed', async () => {
    renderConsole(<HousekeepingPage />, { route: '/admin/housekeeping' });
    await userEvent.click(await screen.findByRole('button', { name: 'Analyze' }));
    await waitFor(() => expect(runHousekeeping).toHaveBeenCalledWith(['thumbs', 'empty-dirs'], true, { reason: undefined }));
  });

  it('cleans up only the preselected tasks after a confirmation that says it is permanent', async () => {
    renderConsole(<HousekeepingPage />, { route: '/admin/housekeeping' });
    await userEvent.click(await screen.findByRole('button', { name: 'Clean up' }));
    const dialog = await screen.findByRole('alertdialog');
    expect(within(dialog).getByText('No. This is permanent and cannot be undone.')).toBeInTheDocument();
    expect(within(dialog).queryByLabelText(/Type/)).not.toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Clean up' }));
    await waitFor(() => expect(runHousekeeping).toHaveBeenCalledWith(['thumbs'], false, { reason: '' }));
  });

  it('requires typed confirmation once files in library folders are selected', async () => {
    renderConsole(<HousekeepingPage />, { route: '/admin/housekeeping' });
    await userEvent.click(await screen.findByRole('checkbox', { name: /Empty folders/ }));
    await userEvent.click(screen.getByRole('button', { name: 'Clean up' }));
    const dialog = await screen.findByRole('alertdialog');
    const confirm = within(dialog).getByRole('button', { name: 'Clean up' });
    expect(confirm).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText(/Type/), 'delete files');
    await userEvent.click(confirm);
    await waitFor(() => expect(runHousekeeping).toHaveBeenCalledWith(['thumbs', 'empty-dirs'], false, { reason: '' }));
  });

  it('keeps Save retention disabled until a value changes, and says what is wrong with a bad one', async () => {
    renderConsole(<HousekeepingPage />, { route: '/admin/housekeeping' });
    const save = await screen.findByRole('button', { name: 'Save retention' });
    expect(save).toBeDisabled();
    const field = screen.getByLabelText('Job history');
    await userEvent.clear(field);
    await userEvent.type(field, '0');
    expect(save).toBeEnabled();
    await userEvent.click(save);
    expect(await screen.findByText('Fix the highlighted fields first.')).toBeInTheDocument();
    expect(updateHousekeepingSettings).not.toHaveBeenCalled();
    await userEvent.clear(field);
    await userEvent.type(field, '45');
    await userEvent.click(save);
    await waitFor(() => expect(updateHousekeepingSettings).toHaveBeenCalledWith({ jobDays: 45, notificationDays: 30, capacityDays: 365, cacheDays: 14 }));
  });
});
