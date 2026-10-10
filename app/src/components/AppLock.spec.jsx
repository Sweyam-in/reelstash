import React from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../api', () => ({
  APP_LOCKED_EVENT: 'reelstash:app-locked',
  getAppLockStatus: vi.fn(), lockApp: vi.fn(), logout: vi.fn(), removeAppLockPin: vi.fn(),
  setAppLockIdleMinutes: vi.fn(), setAppLockPin: vi.fn(), verifyAppLockPin: vi.fn(),
}));
import { getAppLockStatus, lockApp, logout, removeAppLockPin, setAppLockIdleMinutes, setAppLockPin, verifyAppLockPin } from '../api';
import { AppLockProvider, LEAVE_LOCKED_LIBRARY_EVENT, useAppLock } from './AppLock';
import { renderApp } from '../testing/render';

const status = (over = {}) => ({ hasPin: true, locked: false, idleMinutes: 30, scope: 'app', libraryIds: [], librariesLocked: false, ...over });

function Controls() {
  const lock = useAppLock();
  return <div>
    <span data-testid="locked">{String(lock.locked)}</span>
    <span data-testid="has-pin">{String(lock.hasPin)}</span>
    <button onClick={lock.lockNow}>lock now</button>
    <button onClick={lock.openSettings}>settings</button>
  </div>;
}
const mount = () => renderApp(<AppLockProvider><Controls /></AppLockProvider>);
const pin = async digits => { for (const d of digits) await userEvent.keyboard(d); };

beforeEach(() => {
  vi.clearAllMocks();
  getAppLockStatus.mockResolvedValue(status());
  for (const fn of [lockApp, verifyAppLockPin, removeAppLockPin, setAppLockIdleMinutes, setAppLockPin]) fn.mockResolvedValue({});
});
afterEach(() => vi.useRealTimers());

describe('AppLockProvider', () => {
  it('shows the app normally when there is no lock', async () => {
    getAppLockStatus.mockResolvedValue(status({ hasPin: false }));
    mount();
    await waitFor(() => expect(getAppLockStatus).toHaveBeenCalled());
    expect(screen.getByTestId('locked')).toHaveTextContent('false');
    expect(screen.queryByRole('dialog', { name: 'ReelStash is locked' })).not.toBeInTheDocument();
  });

  it('raises the lock screen when the server says it is locked', async () => {
    getAppLockStatus.mockResolvedValue(status({ locked: true }));
    mount();
    expect(await screen.findByRole('dialog', { name: 'ReelStash is locked' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Locked' })).toBeInTheDocument();
  });

  it('never shows a lock screen if there is no PIN, even when marked locked', async () => {
    getAppLockStatus.mockResolvedValue(status({ hasPin: false, locked: true }));
    mount();
    await waitFor(() => expect(getAppLockStatus).toHaveBeenCalled());
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('locks as soon as any request answers 423', async () => {
    mount();
    await waitFor(() => expect(getAppLockStatus).toHaveBeenCalled());
    act(() => { window.dispatchEvent(new Event('reelstash:app-locked')); });
    expect(await screen.findByRole('dialog', { name: 'ReelStash is locked' })).toBeInTheDocument();
  });

  it('lock now asks the server and raises the screen', async () => {
    mount();
    await waitFor(() => expect(screen.getByTestId('has-pin')).toHaveTextContent('true'));
    await userEvent.click(screen.getByRole('button', { name: 'lock now' }));
    expect(lockApp).toHaveBeenCalled();
    expect(await screen.findByRole('dialog', { name: 'ReelStash is locked' })).toBeInTheDocument();
  });

  it('unlocks once six digits are typed and the server accepts them', async () => {
    getAppLockStatus.mockResolvedValue(status({ locked: true }));
    mount();
    await screen.findByRole('dialog', { name: 'ReelStash is locked' });
    await pin('123456');
    await waitFor(() => expect(verifyAppLockPin).toHaveBeenCalledWith('123456'));
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'ReelStash is locked' })).not.toBeInTheDocument());
  });

  it('also accepts taps on the on-screen pad, and Delete removes the last digit', async () => {
    getAppLockStatus.mockResolvedValue(status({ locked: true }));
    mount();
    await screen.findByRole('dialog');
    for (const d of ['1', '2', '9']) await userEvent.click(screen.getByRole('button', { name: d }));
    expect(screen.getByLabelText('3 of 6 digits entered')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }));
    expect(screen.getByLabelText('2 of 6 digits entered')).toBeInTheDocument();
    for (const d of ['3', '4', '5', '6']) await userEvent.click(screen.getByRole('button', { name: d }));
    await waitFor(() => expect(verifyAppLockPin).toHaveBeenCalledWith('123456'));
  });

  it('shows the error and stays locked on a wrong PIN, then clears the dots', async () => {
    getAppLockStatus.mockResolvedValue(status({ locked: true }));
    verifyAppLockPin.mockRejectedValue(new Error('incorrect PIN'));
    mount();
    await screen.findByRole('dialog');
    await pin('000000');
    expect(await screen.findByText('incorrect PIN')).toBeInTheDocument();
    expect(screen.getByRole('dialog', { name: 'ReelStash is locked' })).toBeInTheDocument();
    await waitFor(() => expect(screen.getByLabelText('0 of 6 digits entered')).toBeInTheDocument());
  });

  it('ignores letters and stops at six digits', async () => {
    getAppLockStatus.mockResolvedValue(status({ locked: true }));
    mount();
    await screen.findByRole('dialog');
    await userEvent.keyboard('ab');
    expect(screen.getByLabelText('0 of 6 digits entered')).toBeInTheDocument();
  });

  it('offers sign out on the full lock screen', async () => {
    getAppLockStatus.mockResolvedValue(status({ locked: true }));
    logout.mockResolvedValue({});
    mount();
    await userEvent.click(await screen.findByRole('button', { name: /Sign out/ }));
    expect(logout).toHaveBeenCalled();
  });

  it('a library lock offers "Go to another library" instead of signing out', async () => {
    getAppLockStatus.mockResolvedValue(status({ locked: true, scope: 'libraries', librariesLocked: true, libraryIds: [2] }));
    const left = vi.fn();
    window.addEventListener(LEAVE_LOCKED_LIBRARY_EVENT, left);
    mount();
    expect(await screen.findByRole('heading', { name: 'Library locked' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Sign out/ })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: /Go to another library/ }));
    expect(left).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'ReelStash is locked' })).not.toBeInTheDocument());
    window.removeEventListener(LEAVE_LOCKED_LIBRARY_EVENT, left);
  });

  it('polls for idle expiry while a PIN is set and the app is unlocked', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mount();
    await waitFor(() => expect(getAppLockStatus).toHaveBeenCalledTimes(1));
    await act(async () => { await vi.advanceTimersByTimeAsync(30_000); });
    expect(getAppLockStatus.mock.calls.length).toBeGreaterThanOrEqual(2);
  });

  it('does not poll when there is no PIN', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    getAppLockStatus.mockResolvedValue(status({ hasPin: false }));
    mount();
    await waitFor(() => expect(getAppLockStatus).toHaveBeenCalledTimes(1));
    await act(async () => { await vi.advanceTimersByTimeAsync(90_000); });
    expect(getAppLockStatus).toHaveBeenCalledTimes(1);
  });

  it('carries on when the status cannot be fetched (signed out or offline)', async () => {
    getAppLockStatus.mockRejectedValue(new Error('offline'));
    mount();
    await waitFor(() => expect(getAppLockStatus).toHaveBeenCalled());
    expect(screen.getByTestId('locked')).toHaveTextContent('false');
  });
});

describe('PIN lock settings', () => {
  const openSettings = async () => { await waitFor(() => expect(getAppLockStatus).toHaveBeenCalled()); await userEvent.click(screen.getByRole('button', { name: 'settings' })); };

  it('offers to set a PIN when there is none, then confirms it', async () => {
    getAppLockStatus.mockResolvedValue(status({ hasPin: false }));
    mount();
    await openSettings();
    await userEvent.click(screen.getByRole('button', { name: 'Set a PIN' }));
    expect(screen.getByText('Choose a new 6-digit PIN')).toBeInTheDocument();
    await pin('111222');
    expect(await screen.findByText('Enter the PIN again to confirm')).toBeInTheDocument();
    await pin('111222');
    await waitFor(() => expect(setAppLockPin).toHaveBeenCalledWith('111222', undefined));
    expect(await screen.findByText('PIN lock turned on.')).toBeInTheDocument();
  });

  it('rejects a confirmation that does not match and starts over', async () => {
    getAppLockStatus.mockResolvedValue(status({ hasPin: false }));
    mount();
    await openSettings();
    await userEvent.click(screen.getByRole('button', { name: 'Set a PIN' }));
    await pin('111222');
    await screen.findByText('Enter the PIN again to confirm');
    await pin('999999');
    expect(await screen.findByText('PINs did not match. Try again.')).toBeInTheDocument();
    expect(screen.getByText('Choose a new 6-digit PIN')).toBeInTheDocument();
    expect(setAppLockPin).not.toHaveBeenCalled();
  });

  it('changing a PIN asks for the current one first and sends both', async () => {
    mount();
    await openSettings();
    await userEvent.click(screen.getByRole('button', { name: 'Change PIN' }));
    expect(screen.getByText('Enter your current PIN')).toBeInTheDocument();
    await pin('123456');
    await screen.findByText('Choose a new 6-digit PIN');
    await pin('654321');
    await screen.findByText('Enter the PIN again to confirm');
    await pin('654321');
    await waitFor(() => expect(setAppLockPin).toHaveBeenCalledWith('654321', '123456'));
    expect(await screen.findByText('PIN changed.')).toBeInTheDocument();
  });

  it('turning the lock off asks for the current PIN', async () => {
    mount();
    await openSettings();
    await userEvent.click(screen.getByRole('button', { name: 'Turn off' }));
    await pin('123456');
    await waitFor(() => expect(removeAppLockPin).toHaveBeenCalledWith('123456'));
    expect(await screen.findByText('PIN lock turned off.')).toBeInTheDocument();
  });

  it('reports a wrong current PIN when turning off', async () => {
    removeAppLockPin.mockRejectedValue(new Error('incorrect PIN'));
    mount();
    await openSettings();
    await userEvent.click(screen.getByRole('button', { name: 'Turn off' }));
    await pin('000000');
    expect(await screen.findByText('incorrect PIN')).toBeInTheDocument();
  });

  it('changes the idle timeout', async () => {
    mount();
    await openSettings();
    await userEvent.selectOptions(screen.getByLabelText('Auto-lock after'), '60');
    await waitFor(() => expect(setAppLockIdleMinutes).toHaveBeenCalledWith(60));
    expect(await screen.findByText('Auto-lock updated')).toBeInTheDocument();
  });

  it('cancels back to the menu and closes with the close button or the backdrop', async () => {
    mount();
    await openSettings();
    await userEvent.click(screen.getByRole('button', { name: 'Change PIN' }));
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.getByRole('button', { name: 'Change PIN' })).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(screen.queryByRole('dialog', { name: 'PIN lock' })).not.toBeInTheDocument();
  });
});
