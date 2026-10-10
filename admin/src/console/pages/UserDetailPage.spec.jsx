import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Route, Routes } from 'react-router-dom';

vi.mock('../../api', () => ({
  getUser: vi.fn(), getUsers: vi.fn(), getLibraries: vi.fn(), updateUser: vi.fn(), setUserLibraries: vi.fn(), resetUserPassword: vi.fn(), deleteUser: vi.fn(),
  getUserSessions: vi.fn(), revokeUserSession: vi.fn(), revokeUserSessions: vi.fn(), getAuditEvents: vi.fn(),
}));
import { deleteUser, getAuditEvents, getLibraries, getUser, getUserSessions, getUsers, resetUserPassword, revokeUserSession, revokeUserSessions, setUserLibraries, updateUser } from '../../api';
import { renderConsole } from '../testing';
import { UserDetailPage } from './UserDetailPage';

const users = [
  { id: 1, username: 'root', displayName: 'Root Admin', role: 'ADMIN', disabled: false, lastLoginAt: '2026-10-01T10:00:00Z', libraryIds: [], passwordLogin: true },
  { id: 2, username: 'asha', displayName: 'Asha Nair', email: 'asha@example.com', role: 'USER', disabled: false, lastLoginAt: '2026-10-02T10:00:00Z', libraryIds: [10], passwordLogin: true },
  { id: 3, username: 'sso', displayName: 'Sso Person', role: 'ADMIN', disabled: false, lastLoginAt: null, libraryIds: [], passwordLogin: false },
];
const libraries = [{ id: 10, name: 'Movies', mode: 'entertainment', mediaCount: 5, defaultAccess: true }, { id: 11, name: 'Private', mode: 'private', mediaCount: 7, defaultAccess: false }];

const show = (id, search = '') => renderConsole(<Routes><Route path="/admin/users/:id" element={<UserDetailPage />} /></Routes>, { route: `/admin/users/${id}${search}`, session: { id: 1, isAdmin: true } });

beforeEach(() => {
  vi.clearAllMocks();
  getUser.mockImplementation(id => { const u = users.find(x => String(x.id) === String(id)); return u ? Promise.resolve(u) : Promise.reject(Object.assign(new Error('user not found'), { status: 404 })); });
  getUsers.mockResolvedValue({ items: [], total: 2 });
  getUserSessions.mockResolvedValue({ items: [{ id: 5, kind: 'device', device: 'Pixel 9', platform: 'android', createdAt: '2026-10-01T10:00:00Z', expiresAt: '2026-11-01T10:00:00Z' }, { id: 6, kind: 'browser', createdAt: '2026-10-02T10:00:00Z', expiresAt: '2026-11-01T10:00:00Z' }] });
  getAuditEvents.mockResolvedValue({ items: [{ id: 1, at: '2026-10-02T10:00:00Z', actorName: 'Root Admin', action: 'users.update', method: 'PUT', route: '/api/admin/users/{id}', targetKind: 'users', targetId: 2, targetLabel: 'Asha Nair', reason: 'left the team' }], total: 1 });
  deleteUser.mockResolvedValue({}); revokeUserSession.mockResolvedValue({}); revokeUserSessions.mockResolvedValue({ revoked: 2 });
  getLibraries.mockResolvedValue({ items: libraries });
  updateUser.mockResolvedValue({});
  setUserLibraries.mockResolvedValue({});
  resetUserPassword.mockResolvedValue({});
});

describe('UserDetailPage', () => {
  it('shows an identity header with status and a copyable ID', async () => {
    show(2);
    expect(await screen.findByRole('heading', { name: /Asha Nair/ })).toBeInTheDocument();
    expect(screen.getByText('Active')).toBeInTheDocument();
    expect(screen.getAllByRole('button', { name: 'Copy User ID' }).length).toBeGreaterThan(0);
  });

  it('says so when the user does not exist', async () => {
    show(99);
    expect(await screen.findByText('There is no user with this ID')).toBeInTheDocument();
  });

  it('disables an account only after a confirmation that says who it affects and keeps the role', async () => {
    show(2);
    await userEvent.click(await screen.findByRole('button', { name: 'Disable account' }));
    const dialog = await screen.findByRole('alertdialog');
    expect(within(dialog).getByText(/signed out and can.t sign in again/)).toBeInTheDocument();
    expect(updateUser).not.toHaveBeenCalled();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Disable account' }));
    await waitFor(() => expect(updateUser).toHaveBeenCalledWith(2, { role: 'USER', disabled: true }, { reason: '' }));
  });

  it('will not let you disable the account you are signed in with', async () => {
    show(1);
    await userEvent.click(await screen.findByRole('button', { name: 'Disable account' }));
    expect(await screen.findByText(/can.t disable the account you are signed in with/)).toBeInTheDocument();
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(updateUser).not.toHaveBeenCalled();
  });

  it('asks for typed confirmation before making someone an administrator', async () => {
    show(2, '?tab=danger');
    await userEvent.click(await screen.findByRole('button', { name: 'Make administrator…' }));
    const dialog = await screen.findByRole('alertdialog');
    const confirm = within(dialog).getByRole('button', { name: 'Make administrator' });
    expect(confirm).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText(/Type/), 'asha');
    await userEvent.click(confirm);
    await waitFor(() => expect(updateUser).toHaveBeenCalledWith(2, { role: 'ADMIN', disabled: false }, { reason: '' }));
  });

  it('saves library access once, from a Save button, not on every tick', async () => {
    show(2, '?tab=libraries');
    const save = await screen.findByRole('button', { name: 'Save access' });
    expect(save).toBeDisabled();
    await userEvent.click(screen.getByRole('checkbox', { name: 'Can open Private' }));
    expect(setUserLibraries).not.toHaveBeenCalled();
    await userEvent.click(save);
    await waitFor(() => expect(setUserLibraries).toHaveBeenCalledWith(2, [10, 11]));
  });

  it('asks for the new password twice and signs the person out', async () => {
    show(2, '?tab=danger');
    await userEvent.click((await screen.findAllByRole('button', { name: 'Reset password…' }))[0]);
    const dialog = await screen.findByRole('dialog');
    await userEvent.type(within(dialog).getByLabelText('New password'), 'correct horse');
    await userEvent.type(within(dialog).getByLabelText('Repeat the password'), 'correct hors');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Change password' }));
    expect(await within(dialog).findByText('The two passwords don’t match.')).toBeInTheDocument();
    expect(resetUserPassword).not.toHaveBeenCalled();
    await userEvent.type(within(dialog).getByLabelText('Repeat the password'), 'e');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Change password' }));
    await waitFor(() => expect(resetUserPassword).toHaveBeenCalledWith(2, 'correct horse', { reason: '' }));
  });

  it('records the reason an administrator gives', async () => {
    show(2);
    await userEvent.click(await screen.findByRole('button', { name: 'Disable account' }));
    const dialog = await screen.findByRole('alertdialog');
    await userEvent.type(within(dialog).getByLabelText(/Reason/), 'left the team');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Disable account' }));
    await waitFor(() => expect(updateUser).toHaveBeenCalledWith(2, { role: 'USER', disabled: true }, { reason: 'left the team' }));
  });

  it('deletes an account only with a reason and the typed username', async () => {
    show(2, '?tab=danger');
    await userEvent.click(await screen.findByRole('button', { name: 'Delete account…' }));
    const dialog = await screen.findByRole('alertdialog');
    expect(within(dialog).getByText('No. This is permanent and cannot be undone.')).toBeInTheDocument();
    const confirm = within(dialog).getByRole('button', { name: 'Delete account' });
    await userEvent.type(within(dialog).getByLabelText(/Type/), 'asha');
    expect(confirm).toBeDisabled();
    await userEvent.type(within(dialog).getByLabelText(/Reason/), 'duplicate account');
    await userEvent.click(confirm);
    await waitFor(() => expect(deleteUser).toHaveBeenCalledWith(2, { reason: 'duplicate account' }));
  });

  it('will not delete your own account or the only enabled administrator', async () => {
    getUsers.mockResolvedValue({ items: [], total: 1 });
    show(1, '?tab=danger');
    const button = await screen.findByRole('button', { name: 'Delete account…' });
    expect(button).toBeDisabled();
    expect(screen.getByText('This is your own account')).toBeInTheDocument();
    expect(deleteUser).not.toHaveBeenCalled();
  });

  it('lists live sessions without secrets and signs one out after confirming', async () => {
    show(2, '?tab=sessions');
    expect(await screen.findByText('Pixel 9')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Sign out Pixel 9' }));
    const dialog = await screen.findByRole('alertdialog');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Sign out' }));
    await waitFor(() => expect(revokeUserSession).toHaveBeenCalledWith(2, 5, { reason: '' }));
  });

  it('signs a user out everywhere', async () => {
    show(2, '?tab=sessions');
    await userEvent.click(await screen.findByRole('button', { name: /Sign out everywhere/ }));
    const dialog = await screen.findByRole('alertdialog');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Sign out everywhere' }));
    await waitFor(() => expect(revokeUserSessions).toHaveBeenCalledWith(2, { reason: '' }));
  });

  it('shows the audit trail of changes made to this account, with reasons', async () => {
    show(2, '?tab=activity');
    expect(await screen.findByText('Changed a user’s role or status')).toBeInTheDocument();
    expect(screen.getByText('left the team')).toBeInTheDocument();
    expect(getAuditEvents).toHaveBeenCalledWith({ targetKind: 'users', targetId: 2, limit: 25 });
  });

  it('only offers a password reset to accounts that have a password', async () => {
    show(3, '?tab=danger');
    await screen.findByRole('heading', { name: /Sso Person/ });
    expect(screen.queryByRole('button', { name: 'Reset password…' })).not.toBeInTheDocument();
  });
});
