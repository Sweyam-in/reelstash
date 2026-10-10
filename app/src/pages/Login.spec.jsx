import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../api', () => ({
  getAuthConfig: vi.fn(),
  localLogin: vi.fn(),
  nativeLogin: vi.fn(),
  setupAdmin: vi.fn(),
  pluginPasswordLogin: vi.fn(),
  pluginLoginUrl: (id, redirect) => `/auth/plugin/${id}/login?redirect_to=${encodeURIComponent(redirect)}`,
}));
import { getAuthConfig, localLogin, nativeLogin, pluginPasswordLogin, setupAdmin } from '../api';
import Login from './Login';
import { renderApp } from '../testing/render';

function at(search = '') {
  window.history.replaceState(null, '', `/${search}`);
}

beforeEach(() => {
  vi.clearAllMocks();
  at('');
  getAuthConfig.mockResolvedValue({ localAuthEnabled: true });
});

describe('Login', () => {
  it('shows the password form when only local sign-in is enabled', async () => {
    renderApp(<Login onLoginSuccess={() => {}} />);
    expect(await screen.findByLabelText('Password')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Welcome back' })).toBeInTheDocument();
    expect(screen.queryByText('Continue with SSO')).not.toBeInTheDocument();
  });

  it('offers the sign-in plugin that returns to the current page', async () => {
    window.history.replaceState(null, '', '/library?tab=2');
    getAuthConfig.mockResolvedValue({ localAuthEnabled: false, plugins: [{ id: 'oidc-auth', name: 'OpenID Connect', type: 'redirect', label: 'Continue with SSO' }] });
    renderApp(<Login onLoginSuccess={() => {}} />);
    const link = await screen.findByRole('link', { name: 'Continue with SSO' });
    expect(link).toHaveAttribute('href', '/auth/plugin/oidc-auth/login?redirect_to=%2Flibrary%3Ftab%3D2');
    expect(screen.queryByLabelText('Password')).not.toBeInTheDocument();
  });

  it('labels the local form as the emergency route when SSO is also on', async () => {
    getAuthConfig.mockResolvedValue({ localAuthEnabled: true, plugins: [{ id: 'oidc-auth', name: 'OpenID Connect', type: 'redirect', label: 'Continue with SSO' }] });
    renderApp(<Login onLoginSuccess={() => {}} />);
    expect(await screen.findByRole('heading', { name: 'Emergency local sign-in' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Continue with SSO' })).toBeInTheDocument();
  });

  it('says sign-in is unavailable when no method is enabled', async () => {
    getAuthConfig.mockResolvedValue({ localAuthEnabled: false });
    renderApp(<Login onLoginSuccess={() => {}} />);
    expect(await screen.findByText(/Sign-in is unavailable/)).toBeInTheDocument();
  });

  it('signs in with the typed password and reports success', async () => {
    localLogin.mockResolvedValue({ ok: true });
    const onLoginSuccess = vi.fn();
    renderApp(<Login onLoginSuccess={onLoginSuccess} />);
    await userEvent.type(await screen.findByLabelText('Password'), 's3cret');
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    await waitFor(() => expect(onLoginSuccess).toHaveBeenCalledTimes(1));
    expect(localLogin).toHaveBeenCalledWith('s3cret');
  });

  it('shows the server error and stays put on a wrong password', async () => {
    localLogin.mockRejectedValue(new Error('invalid password'));
    const onLoginSuccess = vi.fn();
    renderApp(<Login onLoginSuccess={onLoginSuccess} />);
    await userEvent.type(await screen.findByLabelText('Password'), 'nope');
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(await screen.findByText('invalid password')).toBeInTheDocument();
    expect(onLoginSuccess).not.toHaveBeenCalled();
  });

  it('requires a password before submitting', async () => {
    renderApp(<Login onLoginSuccess={() => {}} />);
    expect(await screen.findByLabelText('Password')).toBeRequired();
  });

  it('offers a retry when the config cannot be loaded', async () => {
    getAuthConfig.mockRejectedValueOnce(new Error('offline')).mockResolvedValue({ localAuthEnabled: true });
    renderApp(<Login onLoginSuccess={() => {}} />);
    expect(await screen.findByText("Can't connect right now")).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByLabelText('Password')).toBeInTheDocument();
    expect(getAuthConfig).toHaveBeenCalledTimes(2);
  });

  it('reloads the config when the browser comes back online', async () => {
    renderApp(<Login onLoginSuccess={() => {}} />);
    await screen.findByLabelText('Password');
    window.dispatchEvent(new Event('online'));
    await waitFor(() => expect(getAuthConfig).toHaveBeenCalledTimes(2));
  });

  it.each([
    ['not_authorized', "You don't have access"],
    ['account_disabled', 'This account is disabled'],
    ['cancelled', 'Sign-in was cancelled'],
    ['login_failed', "We couldn't sign you in"],
    ['server_error', 'Something went wrong'],
  ])('explains the %s sign-in outcome', async (code, title) => {
    at(`?auth_error=${code}`);
    renderApp(<Login onLoginSuccess={() => {}} />);
    expect(await screen.findByRole('heading', { name: title })).toBeInTheDocument();
    expect(screen.getByRole('alert')).toBeInTheDocument();
  });

  it('treats an unknown error code as a generic failure', async () => {
    at('?auth_error=whatever');
    renderApp(<Login onLoginSuccess={() => {}} />);
    expect(await screen.findByRole('heading', { name: "We couldn't sign you in" })).toBeInTheDocument();
  });

  it('offers "Try again" via the sign-in plugin on an error screen, and clears the error to go back', async () => {
    at('?auth_error=cancelled&keep=1');
    getAuthConfig.mockResolvedValue({ localAuthEnabled: false, plugins: [{ id: 'oidc-auth', name: 'OpenID Connect', type: 'redirect', label: 'Continue with SSO' }] });
    renderApp(<Login onLoginSuccess={() => {}} />);
    expect(await screen.findByRole('link', { name: 'Try again' })).toHaveAttribute('href', '/auth/plugin/oidc-auth/login?redirect_to=%2F');
    await userEvent.click(screen.getByRole('button', { name: 'Back to sign in' }));
    expect(await screen.findByRole('link', { name: 'Continue with SSO' })).toBeInTheDocument();
    expect(window.location.search).toBe('?keep=1');
  });

  it('uses the supplied app name', async () => {
    renderApp(<Login onLoginSuccess={() => {}} appName="ReelStash Admin" />);
    expect(await screen.findByText('ReelStash Admin')).toBeInTheDocument();
  });
});

describe('Login — built-in accounts and plugins', () => {
  it('offers to create the first admin on a fresh install and signs them in', async () => {
    getAuthConfig.mockResolvedValue({ needsSetup: true, nativeEnabled: false, plugins: [] });
    setupAdmin.mockResolvedValue({ ok: true });
    const onLoginSuccess = vi.fn();
    renderApp(<Login onLoginSuccess={onLoginSuccess} />);
    expect(await screen.findByRole('heading', { name: 'Create your admin account' })).toBeInTheDocument();
    expect(screen.queryByText(/Sign-in is unavailable/)).not.toBeInTheDocument();
    await userEvent.type(screen.getByLabelText('Username'), 'owner');
    await userEvent.type(screen.getByLabelText('Password'), 'long-enough-pw');
    await userEvent.type(screen.getByLabelText('Confirm password'), 'long-enough-pw');
    await userEvent.click(screen.getByRole('button', { name: 'Create account' }));
    await waitFor(() => expect(onLoginSuccess).toHaveBeenCalledTimes(1));
    expect(setupAdmin).toHaveBeenCalledWith({ username: 'owner', password: 'long-enough-pw', displayName: '' });
  });

  it('refuses mismatched passwords before asking the server', async () => {
    getAuthConfig.mockResolvedValue({ needsSetup: true, plugins: [] });
    renderApp(<Login onLoginSuccess={() => {}} />);
    await userEvent.type(await screen.findByLabelText('Username'), 'owner');
    await userEvent.type(screen.getByLabelText('Password'), 'long-enough-pw');
    await userEvent.type(screen.getByLabelText('Confirm password'), 'something-else');
    await userEvent.click(screen.getByRole('button', { name: 'Create account' }));
    expect(await screen.findByText('The two passwords do not match.')).toBeInTheDocument();
    expect(setupAdmin).not.toHaveBeenCalled();
  });

  it('signs in with a username and password', async () => {
    getAuthConfig.mockResolvedValue({ nativeEnabled: true, plugins: [] });
    nativeLogin.mockResolvedValue({ ok: true });
    const onLoginSuccess = vi.fn();
    renderApp(<Login onLoginSuccess={onLoginSuccess} />);
    expect(await screen.findByRole('heading', { name: 'Welcome back' })).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText('Username'), ' alice ');
    await userEvent.type(screen.getByLabelText('Password'), 'pw-123456');
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    await waitFor(() => expect(onLoginSuccess).toHaveBeenCalledTimes(1));
    expect(nativeLogin).toHaveBeenCalledWith('alice', 'pw-123456');
  });

  it('shows the server message on a wrong password', async () => {
    getAuthConfig.mockResolvedValue({ nativeEnabled: true, plugins: [] });
    nativeLogin.mockRejectedValue(new Error('invalid username or password'));
    const onLoginSuccess = vi.fn();
    renderApp(<Login onLoginSuccess={onLoginSuccess} />);
    await userEvent.type(await screen.findByLabelText('Username'), 'alice');
    await userEvent.type(screen.getByLabelText('Password'), 'nope');
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(await screen.findByText('invalid username or password')).toBeInTheDocument();
    expect(onLoginSuccess).not.toHaveBeenCalled();
  });

  it('lists a button for each sign-in plugin that returns to the current page', async () => {
    window.history.replaceState(null, '', '/library?tab=2');
    getAuthConfig.mockResolvedValue({ nativeEnabled: true, plugins: [{ id: 'sso', name: 'Company SSO', type: 'redirect', label: 'Continue with Company SSO' }] });
    renderApp(<Login onLoginSuccess={() => {}} />);
    const link = await screen.findByRole('link', { name: 'Continue with Company SSO' });
    expect(link).toHaveAttribute('href', '/auth/plugin/sso/login?redirect_to=%2Flibrary%3Ftab%3D2');
    expect(screen.getAllByRole('heading', { name: 'Welcome back' })).toHaveLength(1); // one heading, however many methods
    expect(screen.getByLabelText('Username')).toBeInTheDocument(); // the password form is still offered
  });

  it('shows a form for a plugin that checks passwords', async () => {
    getAuthConfig.mockResolvedValue({ plugins: [{ id: 'ldap', name: 'LDAP', type: 'password', label: 'Continue with LDAP' }] });
    pluginPasswordLogin.mockResolvedValue({ ok: true });
    const onLoginSuccess = vi.fn();
    renderApp(<Login onLoginSuccess={onLoginSuccess} />);
    await userEvent.type(await screen.findByLabelText('Username'), 'lee');
    await userEvent.type(screen.getByLabelText('Password'), 'pw-123456');
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    await waitFor(() => expect(onLoginSuccess).toHaveBeenCalledTimes(1));
    expect(pluginPasswordLogin).toHaveBeenCalledWith('ldap', 'lee', 'pw-123456');
  });

  it('offers a retry through the plugin after a failed sign-in', async () => {
    window.history.replaceState(null, '', '/?auth_error=not_authorized');
    getAuthConfig.mockResolvedValue({ plugins: [{ id: 'sso', name: 'SSO', type: 'redirect', label: 'Continue with SSO' }] });
    renderApp(<Login onLoginSuccess={() => {}} />);
    const retry = await screen.findByRole('link', { name: 'Try again' });
    expect(retry).toHaveAttribute('href', '/auth/plugin/sso/login?redirect_to=%2F');
  });
});
