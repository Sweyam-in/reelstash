import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../../api', () => ({ getAuditEvents: vi.fn() }));
import { getAuditEvents } from '../../api';
import { renderConsole } from '../testing';
import { AuditPage } from './AuditPage';

const event = (over = {}) => ({ id: 1, at: '2026-10-02T10:00:00Z', actorId: 1, actorName: 'Root Admin', action: 'libraries.delete', method: 'DELETE', route: '/api/admin/libraries/{id}', targetKind: 'libraries', targetId: 7, targetLabel: 'Movies', reason: 'merged into Archive', detail: { revoked: 2 }, ...over });

beforeEach(() => { vi.clearAllMocks(); getAuditEvents.mockResolvedValue({ items: [event(), event({ id: 2, action: 'users.update', targetKind: 'users', targetLabel: 'Asha', reason: '' })], total: 80 }); });

describe('AuditPage', () => {
  it('lists who did what to which target and why, in plain language', async () => {
    renderConsole(<AuditPage />, { route: '/admin/audit' });
    expect(await screen.findByText('Deleted a library')).toBeInTheDocument();
    expect(screen.getByText('merged into Archive')).toBeInTheDocument();
    expect(screen.getAllByText('Root Admin').length).toBe(2);
    expect(screen.getByText('1–50 of 80 events')).toBeInTheDocument();
  });

  it('asks the server to filter by area and pages with offsets', async () => {
    renderConsole(<AuditPage />, { route: '/admin/audit?action=users.&size=25&page=1' });
    await screen.findByText('Deleted a library');
    expect(getAuditEvents).toHaveBeenCalledWith({ q: '', action: 'users.', limit: 25, offset: 25 });
  });

  it('searches with a debounce and sends the search to the server', async () => {
    renderConsole(<AuditPage />, { route: '/admin/audit' });
    await screen.findByText('Deleted a library');
    await userEvent.type(screen.getByRole('searchbox', { name: 'Search the audit log' }), 'asha');
    await waitFor(() => expect(getAuditEvents).toHaveBeenCalledWith(expect.objectContaining({ q: 'asha' })));
  });

  it('opens an event in a drawer with its raw details and a link to the target', async () => {
    renderConsole(<AuditPage />, { route: '/admin/audit' });
    await userEvent.click(await screen.findByRole('row', { name: /Deleted a library/ }));
    const drawer = await screen.findByRole('dialog');
    expect(within(drawer).getByRole('link', { name: 'Movies' })).toHaveAttribute('href', '/admin/libraries/7');
    expect(within(drawer).getByText(/"revoked": 2/)).toBeInTheDocument();
    expect(within(drawer).getByText('DELETE /api/admin/libraries/{id}')).toBeInTheDocument();
  });

  it('says so when nothing is recorded, and when a filter matches nothing', async () => {
    getAuditEvents.mockResolvedValue({ items: [], total: 0 });
    renderConsole(<AuditPage />, { route: '/admin/audit' });
    expect(await screen.findByText('Nothing recorded yet')).toBeInTheDocument();
  });

  it('shows a retry when the log cannot load', async () => {
    getAuditEvents.mockRejectedValue(new Error('boom'));
    renderConsole(<AuditPage />, { route: '/admin/audit' });
    expect(await screen.findByText("Couldn't load the audit log")).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument();
  });
});
