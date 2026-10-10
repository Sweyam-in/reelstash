import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../../api', () => ({ getUsers: vi.fn(), getLibraries: vi.fn(), getAuthSettings: vi.fn(), saveAuthSettings: vi.fn(), createUser: vi.fn() }));
import { getLibraries, getUsers } from '../../api';
import { renderConsole } from '../testing';
import { UsersPage } from './UsersPage';

const user = (id, over = {}) => ({ id, username: `u${id}`, displayName: `Person ${id}`, role: 'USER', disabled: false, lastLoginAt: '2026-10-01T10:00:00Z', libraryIds: [], passwordLogin: true, ...over });
const reply = (items, total = items.length) => ({ items, total, counts: { total: 120, admins: 3, disabled: 2, signedInRecently: 80 } });

beforeEach(() => {
  vi.clearAllMocks();
  getLibraries.mockResolvedValue({ items: [] });
  getUsers.mockResolvedValue(reply([user(1), user(2)], 120));
});

describe('UsersPage', () => {
  it('lets the server page, and shows summary counts that do not depend on the page', async () => {
    renderConsole(<UsersPage />, { route: '/admin/users' });
    expect(await screen.findByText('Person 1')).toBeInTheDocument();
    expect(getUsers).toHaveBeenCalledWith(expect.objectContaining({ limit: 25, offset: 0, sort: 'login', dir: 'desc' }));
    expect(screen.getByText('1–25 of 120 users')).toBeInTheDocument();
    expect(screen.getAllByText('120').length).toBeGreaterThan(0);
    expect(screen.getByText('80')).toBeInTheDocument();
  });

  it('sends filters, sort and the page from the URL to the server', async () => {
    renderConsole(<UsersPage />, { route: '/admin/users?role=ADMIN&status=disabled&method=sso&sort=name&dir=asc&page=2&size=50' });
    await screen.findByText('Person 1');
    expect(getUsers).toHaveBeenCalledWith({ limit: 50, offset: 100, q: '', role: 'ADMIN', status: 'disabled', method: 'sso', sort: 'name', dir: 'asc' });
  });

  it('searches on the server after a pause', async () => {
    renderConsole(<UsersPage />, { route: '/admin/users' });
    await screen.findByText('Person 1');
    await userEvent.type(screen.getByRole('searchbox', { name: 'Search users' }), 'asha');
    await waitFor(() => expect(getUsers).toHaveBeenCalledWith(expect.objectContaining({ q: 'asha', offset: 0 })));
  });

  it('opens a user\'s page from a row', async () => {
    renderConsole(<UsersPage />, { route: '/admin/users' });
    expect(await screen.findByRole('row', { name: 'Person 2, open details' })).toBeInTheDocument();
  });
});
