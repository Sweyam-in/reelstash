import React, { useEffect, useState } from 'react';
import { Alert, Button, Card, FormField, Input } from '@spatika/react';
import { ShieldX } from 'lucide-react';
import { getAuthConfig, localLogin, nativeLogin, pluginLoginUrl, pluginPasswordLogin, setupAdmin } from '../api';
import { paintColorTheme } from '../colorThemes';
import defaultLogo from '../assets/logo.svg';

const AUTH_ERRORS = {
  not_authorized: {
    eyebrow: 'ACCESS RESTRICTED',
    title: "You don't have access",
    message: 'Your account signed in fine, but it has not been granted access to this ReelStash library. Ask the owner to add you, or sign in with a different account.',
  },
  account_disabled: {
    eyebrow: 'ACCOUNT DISABLED',
    title: 'This account is disabled',
    message: 'Your ReelStash account has been disabled. Contact the library owner if you think this is a mistake.',
  },
  cancelled: {
    eyebrow: 'SIGN-IN CANCELLED',
    title: 'Sign-in was cancelled',
    message: 'The sign-in was cancelled before it finished. You can try again whenever you are ready.',
  },
  login_failed: {
    eyebrow: 'SIGN-IN FAILED',
    title: "We couldn't sign you in",
    message: 'Something went wrong while completing sign-in, or the sign-in link expired. Please try again.',
  },
  server_error: {
    eyebrow: 'SIGN-IN FAILED',
    title: 'Something went wrong',
    message: 'ReelStash could not finish setting up your session. Please try again in a moment.',
  },
};

function readAuthError() {
  const code = new URLSearchParams(window.location.search).get('auth_error');
  return code ? (AUTH_ERRORS[code] || AUTH_ERRORS.login_failed) : null;
}

function clearAuthError() {
  const url = new URL(window.location.href);
  url.searchParams.delete('auth_error');
  window.history.replaceState(null, '', url.pathname + url.search + url.hash);
}

// A username + password form. `submit(username, password)` resolves when signed in.
function CredentialsForm({ idPrefix, submit, onSuccess }) {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  async function handleSubmit(event) {
    event.preventDefault();
    setLoading(true);
    setError('');
    try {
      await submit(username.trim(), password);
      onSuccess();
    } catch (err) {
      setError(err.message || 'Sign in failed');
    } finally {
      setLoading(false);
    }
  }

  return <form onSubmit={handleSubmit} className="login-form login-form-inline">
    {error && <Alert variant="danger">{error}</Alert>}
    <FormField label="Username" htmlFor={`${idPrefix}-username`}>
      <Input id={`${idPrefix}-username`} name="username" autoComplete="username" value={username} onChange={e => setUsername(e.target.value)} required />
    </FormField>
    <FormField label="Password" htmlFor={`${idPrefix}-password`}>
      <Input id={`${idPrefix}-password`} name="password" type="password" autoComplete="current-password" value={password} onChange={e => setPassword(e.target.value)} required />
    </FormField>
    <Button type="submit" loading={loading} block>Sign in</Button>
  </form>;
}

// First run: nobody can sign in yet, so the first visitor creates the admin account.
function SetupForm({ onSuccess }) {
  const [form, setForm] = useState({ username: '', displayName: '', password: '', confirm: '' });
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const set = patch => setForm(current => ({ ...current, ...patch }));

  async function handleSubmit(event) {
    event.preventDefault();
    if (form.password !== form.confirm) { setError('The two passwords do not match.'); return; }
    setLoading(true);
    setError('');
    try {
      await setupAdmin({ username: form.username.trim(), password: form.password, displayName: form.displayName.trim() });
      onSuccess();
    } catch (err) {
      setError(err.message || 'Could not create the account');
    } finally {
      setLoading(false);
    }
  }

  return <form onSubmit={handleSubmit} className="login-form">
    <p className="login-eyebrow">FIRST-TIME SETUP</p>
    <h1>Create your admin account</h1>
    <p className="login-message">This account manages ReelStash: libraries, metadata and other people's access.</p>
    {error && <Alert variant="danger">{error}</Alert>}
    <FormField label="Username" htmlFor="setup-username" description="Letters, digits and . _ @ -">
      <Input id="setup-username" name="username" autoComplete="username" value={form.username} onChange={e => set({ username: e.target.value })} required autoFocus />
    </FormField>
    <FormField label="Display name" htmlFor="setup-name">
      <Input id="setup-name" name="displayName" autoComplete="name" value={form.displayName} onChange={e => set({ displayName: e.target.value })} />
    </FormField>
    <FormField label="Password" htmlFor="setup-password" description="At least 8 characters">
      <Input id="setup-password" name="password" type="password" autoComplete="new-password" minLength={8} value={form.password} onChange={e => set({ password: e.target.value })} required />
    </FormField>
    <FormField label="Confirm password" htmlFor="setup-confirm">
      <Input id="setup-confirm" name="confirm" type="password" autoComplete="new-password" value={form.confirm} onChange={e => set({ confirm: e.target.value })} required />
    </FormField>
    <Button type="submit" loading={loading} block>Create account</Button>
  </form>;
}

export default function Login({ onLoginSuccess, appName = 'ReelStash', logo = defaultLogo }) {
  const [config, setConfig] = useState(null);
  const [configUnavailable, setConfigUnavailable] = useState(false);
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [authError, setAuthError] = useState(readAuthError);

  useEffect(() => {
    loadAuthConfig();
    window.addEventListener('online', loadAuthConfig);
    return () => window.removeEventListener('online', loadAuthConfig);
  }, []);

  function loadAuthConfig() {
    setConfig(null);
    setConfigUnavailable(false);
    getAuthConfig()
      .then(value => {
        paintColorTheme(value?.colorTheme);
        setConfig(value);
        setConfigUnavailable(false);
      })
      .catch(() => setConfigUnavailable(true));
  }

  async function handleSubmit(event) {
    event.preventDefault();
    setLoading(true);
    setError('');
    try {
      await localLogin(password);
      onLoginSuccess();
    } catch (err) {
      setError(err.message || 'Sign in failed');
    } finally {
      setLoading(false);
    }
  }

  function dismissAuthError() {
    clearAuthError();
    setAuthError(null);
  }

  const plugins = config?.plugins || [];
  const redirectPlugins = plugins.filter(p => p.type === 'redirect');
  const passwordPlugins = plugins.filter(p => p.type === 'password');
  const returnTo = window.location.pathname + window.location.search;
  const retryHref = redirectPlugins[0] ? pluginLoginUrl(redirectPlugins[0].id, '/') : null;
  const anyMethod = config && (config.localAuthEnabled || config.nativeEnabled || plugins.length > 0);

  if (authError) {
    return <main className="login-screen">
      <div className="login-content">
        <div className="login-brand"><img src={logo} alt="" /><span>{appName}</span></div>
        <p className="login-subtitle">Your library, ready when you are.</p>
        <Card className="login-card">
          <div className="login-form login-denied" role="alert">
            <span className="login-denied-icon" aria-hidden="true">
              <ShieldX size={22} />
            </span>
            <p className="login-eyebrow">{authError.eyebrow}</p>
            <h1>{authError.title}</h1>
            <p className="login-message">{authError.message}</p>
            {retryHref && <Button block asChild><a href={retryHref}>Try again</a></Button>}
            <Button block variant="ghost" onClick={dismissAuthError}>Back to sign in</Button>
          </div>
        </Card>
      </div>
    </main>;
  }

  // Only the first sign-in method carries the page heading.
  const welcome = (first) => first
    ? <><p className="login-eyebrow">YOUR PRIVATE SPACE</p><h1>Welcome back</h1><p className="login-message">Sign in to pick up where you left off.</p></>
    : <p className="login-eyebrow login-or">OR</p>;
  const hasSso = redirectPlugins.length > 0;

  return <main className="login-screen">
    <div className="login-content">
      <div className="login-brand"><img src={logo} alt="" /><span>{appName}</span></div>
      <p className="login-subtitle">Your library, ready when you are.</p>
      <Card className="login-card">
        {configUnavailable && <div className="login-form" role="alert">
          <h1>Can't connect right now</h1>
          <p className="login-message">Check your connection or try again in a moment.</p>
          <Button block onClick={loadAuthConfig}>Try again</Button>
        </div>}
        {config && !anyMethod && !config.needsSetup &&
          <Alert variant="danger">Sign-in is unavailable. Please contact the library owner.</Alert>}
        {config?.needsSetup && <SetupForm onSuccess={onLoginSuccess} />}
        {hasSso && <div className="login-form">
          {welcome(true)}
          {redirectPlugins.map(p => <Button key={p.id} block asChild><a href={pluginLoginUrl(p.id, returnTo)}>{p.label}</a></Button>)}
        </div>}
        {config?.nativeEnabled && <div className="login-form">
          {welcome(!hasSso)}
          <CredentialsForm idPrefix="native" submit={nativeLogin} onSuccess={onLoginSuccess} />
        </div>}
        {passwordPlugins.map(p => <div className="login-form" key={p.id}>
          <p className="login-eyebrow">{hasSso || config?.nativeEnabled ? `OR ${p.name.toUpperCase()}` : p.name.toUpperCase()}</p>
          <CredentialsForm idPrefix={`plugin-${p.id}`} submit={(u, pw) => pluginPasswordLogin(p.id, u, pw)} onSuccess={onLoginSuccess} />
        </div>)}
        {config?.localAuthEnabled && <form onSubmit={handleSubmit} className="login-form">
          <p className="login-eyebrow">YOUR PRIVATE SPACE</p>
          <h1>{hasSso || config?.nativeEnabled || passwordPlugins.length > 0 ? 'Emergency local sign-in' : 'Welcome back'}</h1>
          {error && <Alert variant="danger">{error}</Alert>}
          <FormField label="Password" htmlFor="password">
            <Input id="password" name="password" type="password" autoComplete="current-password" value={password} onChange={e => setPassword(e.target.value)} required autoFocus />
          </FormField>
          <Button type="submit" loading={loading} block>Sign in</Button>
        </form>}
      </Card>
    </div>
  </main>;
}
