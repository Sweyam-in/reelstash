import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../../api', () => ({ cancelJob: vi.fn(), clearFinishedJobs: vi.fn(), getBackgroundJobs: vi.fn(), getBackgroundJob: vi.fn(), getLibraries: vi.fn(), retryJob: vi.fn(), runJob: vi.fn() }));
const live = { connected: false, handlers: {} };
vi.mock('../../events', () => ({ useEventsConnected: () => live.connected, useServerEvent: (name, handler) => { live.handlers[name] = handler; } }));
import { cancelJob, clearFinishedJobs, getBackgroundJobs, getLibraries, runJob } from '../../api';
import { renderConsole } from '../testing';
import { JobsPage } from './JobsPage';

const job = (over = {}) => ({ id: 1, kind: 'DUPLICATE_SCAN', status: 'COMPLETED', attempts: 1, createdAt: '2026-09-30 10:00:00+00', startedAt: '2026-09-30 10:00:01+00', finishedAt: '2026-09-30 10:01:00+00', durationSeconds: 59, ...over });
const snapshot = (items = [], extra = {}) => ({ items, counts: {}, latestByKind: {}, thumbnailsPending: 0, previewsPending: 0, ...extra });

beforeEach(() => {
  vi.clearAllMocks();
  live.connected = false; live.handlers = {};
  getBackgroundJobs.mockResolvedValue(snapshot());
  getLibraries.mockResolvedValue({ items: [{ id: 5, name: 'Movies' }] });
  runJob.mockResolvedValue({}); cancelJob.mockResolvedValue({}); clearFinishedJobs.mockResolvedValue({ deleted: 2 });
});

const row = name => screen.getByText(name).closest('tr');

describe('JobsPage', () => {
  it('lists every runnable task and runs a harmless one straight away', async () => {
    renderConsole(<JobsPage />, { route: '/admin/jobs' });
    await screen.findByText('Find duplicates');
    await userEvent.click(within(row('Find duplicates')).getByRole('button', { name: /Run/ }));
    await waitFor(() => expect(runJob).toHaveBeenCalledWith('DUPLICATE_SCAN', 0, { reason: undefined }));
  });

  it('puts a destructive task behind typed confirmation', async () => {
    renderConsole(<JobsPage />, { route: '/admin/jobs' });
    await screen.findByText('Remove missing titles');
    await userEvent.click(within(row('Remove missing titles')).getByRole('button', { name: /Run/ }));
    const dialog = await screen.findByRole('alertdialog');
    expect(within(dialog).getByText(/Their watch history, ratings and playlist entries go with them/)).toBeInTheDocument();
    const confirm = within(dialog).getByRole('button', { name: 'Remove missing titles' });
    expect(confirm).toBeDisabled();
    expect(runJob).not.toHaveBeenCalled();
    await userEvent.type(within(dialog).getByLabelText(/Type/), 'remove');
    expect(confirm).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText(/Reason/), 'cleanup after the move');
    await userEvent.click(confirm);
    await waitFor(() => expect(runJob).toHaveBeenCalledWith('REMOVE_MISSING', 0, { reason: 'cleanup after the move' }));
  });

  it('shows what is running with a stop button', async () => {
    getBackgroundJobs.mockResolvedValue(snapshot([job({ id: 7, kind: 'DUPLICATE_SCAN', status: 'RUNNING', finishedAt: null, progressDone: 3, progressTotal: 10 })]));
    renderConsole(<JobsPage />, { route: '/admin/jobs' });
    const running = (await screen.findByRole('region', { name: 'Running now' }));
    await userEvent.click(within(running).getByRole('button', { name: /Stop/ }));
    await waitFor(() => expect(cancelJob).toHaveBeenCalledWith(7));
  });

  it('asks the server for one page of history at a time, filtered by status', async () => {
    const failed = job({ id: 3, status: 'FAILED', error: 'ffprobe: moov atom not found' });
    getBackgroundJobs.mockImplementation(async params => (params.status === 'FAILED' ? snapshot([failed], { total: 31, counts: { FAILED: 31 } }) : snapshot([job({ id: 2 })], { total: 1, counts: { COMPLETED: 1 } })));
    renderConsole(<JobsPage />, { route: '/admin/jobs?tab=history&status=FAILED&size=25' });
    const row = await screen.findByRole('row', { name: /Find duplicates, FAILED/ });
    expect(getBackgroundJobs).toHaveBeenCalledWith(expect.objectContaining({ status: 'FAILED', limit: 25, offset: 0 }));
    expect(await screen.findByText('1–25 of 31 jobs')).toBeInTheDocument();
    await userEvent.click(row);
    await waitFor(() => expect(screen.getAllByText('ffprobe: moov atom not found').length).toBeGreaterThan(1));
  });

  it('moves to the next page by asking for the next offset', async () => {
    getBackgroundJobs.mockResolvedValue(snapshot([job({ id: 2 })], { total: 60, counts: { COMPLETED: 60 } }));
    renderConsole(<JobsPage />, { route: '/admin/jobs?tab=history&size=25' });
    await screen.findByRole('row', { name: /Find duplicates, COMPLETED/ });
    await userEvent.click(screen.getByRole('button', { name: /next/i }));
    await waitFor(() => expect(getBackgroundJobs).toHaveBeenCalledWith(expect.objectContaining({ limit: 25, offset: 25 })));
  });

  it('confirms before clearing the finished history', async () => {
    getBackgroundJobs.mockResolvedValue(snapshot([job({ id: 2 })], { counts: { COMPLETED: 2 } }));
    renderConsole(<JobsPage />, { route: '/admin/jobs?tab=history' });
    await userEvent.click(await screen.findByRole('button', { name: /Clear finished/ }));
    const dialog = await screen.findByRole('alertdialog');
    expect(within(dialog).getByText(/Running and queued jobs are kept/)).toBeInTheDocument();
    expect(clearFinishedJobs).not.toHaveBeenCalled();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Clear history' }));
    await waitFor(() => expect(clearFinishedJobs).toHaveBeenCalled());
  });

  it('says whether updates are live or polling', async () => {
    live.connected = true;
    renderConsole(<JobsPage />, { route: '/admin/jobs' });
    expect(await screen.findByText('Live')).toBeInTheDocument();
  });
});
