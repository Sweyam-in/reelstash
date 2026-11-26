import test, { beforeEach } from 'node:test';
import assert from 'node:assert/strict';

// api.js touches window only to announce a locked app, so a tiny stand-in is enough.
const locked = [];
globalThis.window = { dispatchEvent: event => locked.push(event.type), location: { assign: url => { globalThis.__assigned = url; } } };
globalThis.Event ||= class { constructor(type) { this.type = type; } };

const api = await import('./api.js');

let calls;
function respond(body, { status = 200, json } = {}) {
  globalThis.fetch = async (url, opts) => {
    calls.push({ url, opts });
    return {
      ok: status >= 200 && status < 300,
      status,
      json: json || (async () => body),
    };
  };
}
const last = () => calls[calls.length - 1];

beforeEach(() => {
  calls = [];
  locked.length = 0;
  api.setCurrentLibrary(null);
  respond({ ok: true });
});

test('GET requests carry the session cookie and no body', async () => {
  await api.getMediaItem(7);
  assert.equal(last().url, '/api/media/7');
  assert.equal(last().opts.method, 'GET');
  assert.equal(last().opts.credentials, 'include');
  assert.equal(last().opts.body, undefined);
  assert.equal(last().opts.headers['Content-Type'], undefined);
});

test('POST and PUT without a payload still send an empty object', async () => {
  await api.lockApp();
  assert.equal(last().opts.body, '{}');
  await api.addWatchLater(3);
  assert.equal(last().opts.method, 'PUT');
  assert.equal(last().opts.body, '{}');
});

test('the selected library is sent on every call and can be cleared', async () => {
  api.setCurrentLibrary(12);
  await api.getCategories();
  assert.equal(last().opts.headers['X-Library'], '12');
  api.setCurrentLibrary(0);
  await api.getCategories();
  assert.equal(last().opts.headers['X-Library'], undefined);
});

test('a 423 announces the app lock and still rejects', async () => {
  respond({ error: 'locked' }, { status: 423 });
  await assert.rejects(api.getMediaItems(), err => err.status === 423);
  assert.deepEqual(locked, [api.APP_LOCKED_EVENT]);
});

test('getSession swallows failures into an unauthenticated session', async () => {
  respond({ error: 'no' }, { status: 401 });
  assert.deepEqual(await api.getSession(), { authenticated: false });
  respond({ id: 1, isAdmin: true });
  assert.deepEqual(await api.getSession(), { id: 1, isAdmin: true });
});

test('logout follows the identity provider landing page when the server names one', async () => {
  respond({ ok: true, logoutUrl: 'https://auth.example/bye' });
  await api.logout();
  assert.equal(globalThis.__assigned, 'https://auth.example/bye');
  globalThis.__assigned = undefined;
  respond({ ok: true, logoutUrl: '' });
  await api.logout();
  assert.equal(globalThis.__assigned, undefined);
});

test('an app-lock PIN removal sends the pin in the DELETE body', async () => {
  await api.removeAppLockPin('1234');
  assert.deepEqual([last().opts.method, last().opts.body], ['DELETE', '{"pin":"1234"}']);
});

test('sign-in helpers hit the built-in and plugin endpoints', async () => {
  await api.nativeLogin('alice', 'pw');
  assert.equal(last().url, '/auth/native-login');
  assert.deepEqual(JSON.parse(last().opts.body), { username: 'alice', password: 'pw' });
  await api.setupAdmin({ username: 'owner', password: 'pw', displayName: 'O' });
  assert.equal(last().url, '/auth/setup');
  assert.deepEqual(JSON.parse(last().opts.body), { username: 'owner', password: 'pw', displayName: 'O' });
  await api.pluginPasswordLogin('my plugin', 'lee', 'pw');
  assert.equal(last().url, '/auth/plugin/my%20plugin/login');
  await api.changeMyPassword('old', 'new');
  assert.equal(last().url, '/api/me/password');
  assert.equal(last().opts.method, 'PUT');
  assert.deepEqual(JSON.parse(last().opts.body), { current: 'old', password: 'new' });
  assert.equal(api.pluginLoginUrl('sso', '/a?b=1'), '/auth/plugin/sso/login?redirect_to=%2Fa%3Fb%3D1');
  assert.equal(api.pluginLoginUrl('sso'), '/auth/plugin/sso/login?redirect_to=%2F');
});
