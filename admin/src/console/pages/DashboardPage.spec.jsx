import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, within } from '@testing-library/react';

vi.mock('../../api', () => ({
  getOverview: vi.fn(), getAnalytics: vi.fn(), getUsageAnalytics: vi.fn(), getLibraries: vi.fn(), getStorageLocations: vi.fn(), getBackgroundJobs: vi.fn(),
  startLibraryScan: vi.fn(), cancelJob: vi.fn(), getHealth: vi.fn(), getMatchSuggestions: vi.fn(),
}));
vi.mock('../../events', () => ({ useEventsConnected: () => false, useServerEvent: () => {} }));
import { getAnalytics, getBackgroundJobs, getHealth, getLibraries, getMatchSuggestions, getOverview, getStorageLocations, getUsageAnalytics } from '../../api';
import { renderConsole } from '../testing';
import { SignalsProvider } from '../lib/signals';
import { DashboardPage } from './DashboardPage';

const health = { checks: [{ id: 'database', label: 'Database', status: 'ok', detail: 'fine' }, { id: 'providers', label: 'Metadata providers', status: 'warning', detail: 'x-plugin has no API key' }],
  runtime: { uptimeSeconds: 7200 }, streams: { streams: 1 }, jobs: { pending: 0 }, database: { latencyMs: 3 } };
const overview = { failedBackgroundJobs: 2, duplicateGroups: 0, filesMissingMetadata: 5 };
const disks = [{ id: 1, name: 'Media', online: true, totalCapacity: 100, usedCapacity: 95, availableCapacity: 5 }, { id: 2, name: 'Ext', online: false, totalCapacity: 100, usedCapacity: 10, availableCapacity: 90 }];

function setup({ overviewData = overview, healthData = health, diskData = disks, review = 3 } = {}) {
  getOverview.mockResolvedValue(overviewData);
  getHealth.mockResolvedValue(healthData);
  getStorageLocations.mockResolvedValue({ items: diskData });
  getMatchSuggestions.mockResolvedValue({ items: [], total: review });
  getAnalytics.mockResolvedValue({ totalMediaItems: 10, totalPerformers: 2, totalStudios: 1, totalSizeBytes: 1e9, itemsAddedByDay: [] });
  getUsageAnalytics.mockResolvedValue({ watch: { hours: 1, sessions: 2, daily: [], completed: 0, byHour: [] }, users: { active: 1, total: 2, activeToday: 0 }, library: { addedItems: 0, daily: [] } });
  getLibraries.mockResolvedValue({ items: [] });
  getBackgroundJobs.mockResolvedValue({ items: [], counts: {}, latestByKind: {} });
  return renderConsole(<SignalsProvider><DashboardPage /></SignalsProvider>, { route: '/admin' });
}

beforeEach(() => vi.clearAllMocks());

describe('DashboardPage', () => {
  it('orders "needs attention" by urgency and derives every row from a real signal', async () => {
    setup();
    const panel = await screen.findByRole('region', { name: 'Needs attention' });
    const rows = await within(panel).findAllByRole('listitem');
    const titles = rows.map(r => r.querySelector('.console-attention__title').textContent);
    expect(titles[0]).toMatch(/1 storage location offline/);
    expect(titles[1]).toMatch(/2 jobs failed this week/);
    expect(titles.some(t => /Low space on 1 disk/.test(t))).toBe(true);
    expect(titles.some(t => /3 titles waiting for review/.test(t))).toBe(true);
    expect(titles[titles.length - 1]).toMatch(/5 titles without metadata/);
    // severity is spelled out, not colour alone
    expect(within(rows[0]).getByText(/critical/)).toBeInTheDocument();
  });

  it('says plainly when nothing needs attention', async () => {
    setup({ overviewData: { failedBackgroundJobs: 0, duplicateGroups: 0, filesMissingMetadata: 0 }, healthData: { ...health, checks: [{ id: 'database', label: 'Database', status: 'ok', detail: '' }] }, diskData: [{ id: 1, name: 'Media', online: true, totalCapacity: 100, usedCapacity: 20, availableCapacity: 80 }], review: 0 });
    expect(await screen.findByText('Nothing needs attention')).toBeInTheDocument();
    expect(await screen.findByText('All systems normal')).toBeInTheDocument();
  });

  it('shows the system status banner above everything else', async () => {
    setup();
    const banner = await screen.findByText(/Working, with 1 warning/);
    const attention = await screen.findByRole('region', { name: 'Needs attention' });
    expect(banner.compareDocumentPosition(attention) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('offers the setup guide first when nothing is configured', async () => {
    setup({ diskData: [], overviewData: { failedBackgroundJobs: 0, duplicateGroups: 0, filesMissingMetadata: 0 } });
    expect(await screen.findByRole('link', { name: /Start the setup guide/ })).toBeInTheDocument();
  });
});
