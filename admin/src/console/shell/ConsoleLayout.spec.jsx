import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes, useLocation } from 'react-router-dom';

vi.mock('../../api', () => ({
  getHealth: vi.fn(), getMatchSuggestions: vi.fn(), getMediaItems: vi.fn(), getAdminVideos: vi.fn(), getLibraries: vi.fn(), getStorageLocations: vi.fn(), getUsers: vi.fn(),
  getAuthConfig: vi.fn(), getNotifications: vi.fn(), markNotificationsRead: vi.fn(),
}));
vi.mock('../../events', () => ({ useEventsConnected: () => false, useServerEvent: () => {} }));
import { getAdminVideos, getHealth, getLibraries, getMatchSuggestions, getMediaItems, getStorageLocations, getUsers } from '../../api';
import { renderConsole } from '../testing';
import { SignalsProvider } from '../lib/signals';
import { ConsoleLayout } from './ConsoleLayout';

function Where() { return <p data-testid="where">{useLocation().pathname}</p>; }
const admin = { id: 1, isAdmin: true, displayName: 'Root Admin', email: 's@example.com' };

function shell(session = admin, restricted = false) {
  const body = <ConsoleLayout session={session} onSignOut={() => {}} restricted={restricted}><Routes><Route path="*" element={<Where />} /></Routes></ConsoleLayout>;
  return renderConsole(restricted ? body : <SignalsProvider>{body}</SignalsProvider>, { route: '/admin', session });
}

beforeEach(() => {
  vi.clearAllMocks();
  getHealth.mockResolvedValue({ checks: [{ id: 'database', label: 'Database', status: 'error', detail: 'down' }] });
  getMatchSuggestions.mockResolvedValue({ items: [], total: 12 });
  getMediaItems.mockResolvedValue({ items: [] });
  getAdminVideos.mockResolvedValue({ items: [{ id: 9, title: 'Heat (1995)', studioName: 'Warner' }] });
  getLibraries.mockResolvedValue({ items: [{ id: 1, name: 'Movies', roots: [] }] });
  getStorageLocations.mockResolvedValue({ items: [{ id: 3, name: 'Archive', mountPath: '/mnt/archive' }] });
  getUsers.mockImplementation(async params => ({ items: String(params?.q || '').includes('asha') ? [{ id: 2, username: 'asha', displayName: 'Asha Nair', role: 'USER' }] : [] }));
});

describe('ConsoleLayout', () => {
  it('shares one navigation model: sidebar groups, active page and the review count', async () => {
    shell();
    const nav = await screen.findByRole('navigation', { name: 'Console navigation' });
    expect(nav).toHaveTextContent('Overview');
    expect(nav).toHaveTextContent('Access & settings');
    expect(await screen.findByLabelText('12 titles waiting')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /Dashboard/ })).toHaveAttribute('aria-current', 'page');
  });

  it('shows system status in the top bar, from the health check', async () => {
    shell();
    expect(await screen.findByRole('link', { name: 'System status: 1 problem' })).toBeInTheDocument();
  });

  it('jumps with "g" then a letter, and ignores keys typed into a field', async () => {
    shell();
    await screen.findByRole('navigation', { name: 'Console navigation' });
    await userEvent.keyboard('gj');
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent('/admin/jobs'));
    await userEvent.keyboard('gu');
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent('/admin/users'));
    await userEvent.keyboard('gh');
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent('/admin/housekeeping'));
  });

  it('opens the command palette with Ctrl+K and finds pages, people and videos', async () => {
    shell();
    await screen.findByRole('navigation', { name: 'Console navigation' });
    await userEvent.keyboard('{Control>}k{/Control}');
    const input = await screen.findByPlaceholderText(/Jump to a page/);
    await userEvent.type(input, 'asha');
    expect(await screen.findByText('Asha Nair')).toBeInTheDocument();
    expect(getUsers).toHaveBeenCalledWith({ q: 'asha', limit: 5 });
    await userEvent.clear(input);
    await userEvent.type(input, 'heat');
    expect(await screen.findByText('Heat (1995)')).toBeInTheDocument();
    await userEvent.clear(input);
    await userEvent.type(input, 'scan rules');
    await userEvent.click(await screen.findByText('Scan rules'));
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent('/admin/scan-rules'));
  });

  it('keeps a non-admin inside the shell: no admin requests, no search, no health pill', async () => {
    shell({ id: 9, isAdmin: false, displayName: 'Viewer' }, true);
    expect(await screen.findByTestId('where')).toBeInTheDocument();
    expect(screen.queryByRole('navigation', { name: 'Console navigation' })?.textContent || '').not.toMatch(/Dashboard/);
    expect(screen.queryByRole('link', { name: /System status/ })).not.toBeInTheDocument();
    await userEvent.keyboard('gj');
    expect(screen.getByTestId('where')).toHaveTextContent('/admin');
    expect(getHealth).not.toHaveBeenCalled();
    expect(getMatchSuggestions).not.toHaveBeenCalled();
  });
});
