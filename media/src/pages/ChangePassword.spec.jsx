import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../api', async importOriginal => ({ ...(await importOriginal()), getSession: vi.fn(), changeMyPassword: vi.fn() }));
import { changeMyPassword, getSession } from '../api';
import { ChangePassword } from './UserSettings';
import { renderApp } from '../testing/render';

beforeEach(() => {
  vi.clearAllMocks();
  getSession.mockResolvedValue({ id: 1, canChangePassword: true });
});

async function fill(current, password, confirm) {
  await userEvent.type(await screen.findByLabelText('Current password'), current);
  await userEvent.type(screen.getByLabelText('New password'), password);
  await userEvent.type(screen.getByLabelText('Confirm new password'), confirm);
  await userEvent.click(screen.getByRole('button', { name: 'Change password' }));
}

describe('ChangePassword', () => {
  it('is hidden for accounts that sign in elsewhere', async () => {
    getSession.mockResolvedValue({ id: 1, canChangePassword: false });
    renderApp(<ChangePassword />);
    await waitFor(() => expect(getSession).toHaveBeenCalled());
    expect(screen.queryByText('Password')).not.toBeInTheDocument();
  });

  it('changes the password', async () => {
    changeMyPassword.mockResolvedValue({ ok: true });
    renderApp(<ChangePassword />);
    await fill('old-password', 'new-password-1', 'new-password-1');
    await waitFor(() => expect(changeMyPassword).toHaveBeenCalledWith('old-password', 'new-password-1'));
    await waitFor(() => expect(screen.getByLabelText('Current password')).toHaveValue(''));
  });

  it('checks the confirmation before asking the server', async () => {
    renderApp(<ChangePassword />);
    await fill('old-password', 'new-password-1', 'different');
    expect(await screen.findByRole('alert')).toHaveTextContent('do not match');
    expect(changeMyPassword).not.toHaveBeenCalled();
  });

  it('shows the server message when the current password is wrong', async () => {
    changeMyPassword.mockRejectedValue(new Error('the current password is not right'));
    renderApp(<ChangePassword />);
    await fill('wrong', 'new-password-1', 'new-password-1');
    expect(await screen.findByRole('alert')).toHaveTextContent('the current password is not right');
  });
});
