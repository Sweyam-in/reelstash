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

test('JSON bodies are serialised with a content type', async () => {
  await api.createCategory({ name: 'Drama' });
  assert.equal(last().opts.method, 'POST');
  assert.equal(last().opts.headers['Content-Type'], 'application/json');
  assert.equal(last().opts.body, '{"name":"Drama"}');
});

test('POST and PUT without a payload still send an empty object', async () => {
  await api.lockApp();
  assert.equal(last().opts.body, '{}');
  await api.addWatchLater(3);
  assert.equal(last().opts.method, 'PUT');
  assert.equal(last().opts.body, '{}');
});

test('DELETE and PATCH use the right verbs', async () => {
  await api.deleteTag(4);
  assert.deepEqual([last().url, last().opts.method], ['/api/admin/tags/4', 'DELETE']);
  await api.updateMediaItem(9, { watched: true });
  assert.deepEqual([last().url, last().opts.method, last().opts.body], ['/api/media/9', 'PATCH', '{"watched":true}']);
  await api.setCollectionPinned(2, true);
  assert.deepEqual([last().url, last().opts.body], ['/api/collections/2', '{"pinned":true}']);
});

test('the selected library is sent on every call and can be cleared', async () => {
  api.setCurrentLibrary(12);
  await api.getCategories();
  assert.equal(last().opts.headers['X-Library'], '12');
  api.setCurrentLibrary(0);
  await api.getCategories();
  assert.equal(last().opts.headers['X-Library'], undefined);
});

test('204 answers resolve to null without parsing a body', async () => {
  respond(null, { status: 204, json: async () => { throw new Error('no body'); } });
  assert.equal(await api.deleteWebhook(1), null);
});

test('error responses reject with the server message and status', async () => {
  respond({ error: 'name is required' }, { status: 400 });
  await assert.rejects(api.createTag({}), err => err.message === 'name is required' && err.status === 400);
});

test('error responses without a JSON body still reject sensibly', async () => {
  respond(null, { status: 502, json: async () => { throw new Error('html'); } });
  await assert.rejects(api.getOverview(), err => err.message === 'An unknown network error occurred' && err.status === 502);
  respond({}, { status: 500 });
  await assert.rejects(api.getOverview(), err => err.message === 'Request failed');
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

test('list endpoints build their query strings', async () => {
  await api.getMediaItems({ q: 'a b', limit: 5 });
  assert.equal(last().url, '/api/media?q=a+b&limit=5');
  await api.getMediaItems();
  assert.equal(last().url, '/api/media?');
  await api.getCollection(3, { limit: 200, offset: 400 });
  assert.equal(last().url, '/api/collections/3?limit=200&offset=400');
  await api.getCollection(3);
  assert.equal(last().url, '/api/collections/3');
  await api.search('rock & roll');
  assert.equal(last().url, '/api/search?q=rock%20%26%20roll');
  await api.getNotifications();
  assert.equal(last().url, '/api/notifications?limit=20');
  await api.getUsageAnalytics(7, 'Europe/Paris');
  assert.equal(last().url, '/api/admin/analytics/usage?days=7&tz=Europe%2FParis');
});

test('provider search only adds a providers filter when some are chosen', async () => {
  await api.searchMediaProviders(5, 'title');
  assert.equal(last().url, '/api/media/5/match?q=title');
  await api.searchMediaProviders(5, 'title', ['tmdb', 'plugin']);
  assert.equal(last().url, '/api/media/5/match?q=title&providers=tmdb%2Cplugin');
  await api.searchPerformerProviders(2, 'jane', ['tmdb']);
  assert.match(last().url, /^\/api\/performers\/2\/match\?q=jane/);
  assert.match(last().url, /tmdb/);
});

test('file endpoints coerce the storage id to a number for JSON bodies', async () => {
  await api.renameFileEntry('42', 'a/b.mp4', 'c.mp4');
  assert.deepEqual(JSON.parse(last().opts.body), { storageLocationId: 42, path: 'a/b.mp4', newName: 'c.mp4' });
  await api.moveFileEntry('42', 'a', 'b');
  assert.deepEqual(JSON.parse(last().opts.body), { storageLocationId: 42, path: 'a', destPath: 'b', destStorageLocationId: 42 });
  await api.copyFileEntry('42', 'a', 'b', '9');
  assert.deepEqual(JSON.parse(last().opts.body), { storageLocationId: 42, path: 'a', destPath: 'b', destStorageLocationId: 9 });
  await api.createFileDir('7', '', 'new');
  assert.equal(JSON.parse(last().opts.body).storageLocationId, 7);
  await api.saveFileNfo('7', 'x.mp4', '<movie/>');
  assert.deepEqual(JSON.parse(last().opts.body), { storageLocationId: 7, path: 'x.mp4', content: '<movie/>' });
});

test('file listing and deletion encode paths safely', async () => {
  await api.listFiles(3, 'my folder/x&y');
  assert.equal(last().url, '/api/files?storageLocationId=3&path=my+folder%2Fx%26y');
  await api.deleteFileEntry(3, 'a');
  assert.equal(last().url, '/api/admin/files?storageLocationId=3&path=a');
  await api.deleteFileEntry(3, 'a', true);
  assert.match(last().url, /recursive=true$/);
  assert.equal(api.fileDownloadUrl(3, 'a b.mp4'), '/api/files/download?storageLocationId=3&path=a+b.mp4');
});

test('URL builders for media assets', () => {
  assert.equal(api.streamUrl(5), '/api/media/5/stream');
  assert.equal(api.streamUrl(5, 9), '/api/media/5/stream?fileId=9');
  assert.equal(api.artworkUrl(5), '/api/media/5/artwork?kind=thumbnail');
  assert.equal(api.artworkUrl(5, 'poster', 3), '/api/media/5/artwork?kind=poster&v=3');
  assert.equal(api.mediaFrameUrl(5, 12.345), '/api/media/5/frame?t=12.3&w=640');
  assert.equal(api.previewUrl(5), '/api/media/5/preview');
  assert.equal(api.performerImageUrl(2), '/api/performers/2/image');
  assert.equal(api.performerImageUrl(2, 4), '/api/performers/2/image?v=4');
  assert.equal(api.studioImageUrl(2, 8), '/api/studios/2/image?v=8');
  assert.equal(api.seriesImageUrl(2), '/api/series/2/image');
  assert.equal(api.performerPhotoUrl(2, 6), '/api/performers/2/photos/6');
});

test('series provider links are URL-encoded', async () => {
  await api.unlinkSeriesProvider(4, 'my provider');
  assert.equal(last().url, '/api/series/4/providers/my%20provider');
});

test('provider settings calls hit the admin endpoints', async () => {
  await api.saveProviderConfig('tmdb', { token: 'x' });
  assert.deepEqual([last().url, last().opts.method, last().opts.body], ['/api/admin/providers/tmdb/config', 'PUT', '{"token":"x"}']);
  await api.saveProviderOrder({ order: ['b', 'a'], mode: 'best' });
  assert.equal(last().opts.body, '{"order":["b","a"],"mode":"best"}');
  await api.clearProviderConfig('tmdb');
  assert.equal(last().opts.method, 'DELETE');
  await api.testProvider('tmdb');
  assert.equal(last().url, '/api/admin/providers/tmdb/test');
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

test('user and sign-in administration', async () => {
  await api.createUser({ username: 'kid', password: 'pw', role: 'USER' });
  assert.equal(last().url, '/api/admin/users');
  assert.equal(last().opts.method, 'POST');
  await api.resetUserPassword(4, 'new-pass');
  assert.equal(last().url, '/api/admin/users/4/password');
  assert.deepEqual(JSON.parse(last().opts.body), { password: 'new-pass' });
  await api.saveAuthSettings(false);
  assert.equal(last().url, '/api/admin/auth');
  assert.deepEqual(JSON.parse(last().opts.body), { nativeEnabled: false });
});

test('plugin bundle administration', async () => {
  await api.getPlugins();
  assert.equal(last().url, '/api/admin/plugins');
  await api.installPluginFromUrl('https://x.test/p.zip');
  assert.deepEqual(JSON.parse(last().opts.body), { url: 'https://x.test/p.zip' });
  await api.setPluginEnabled('p', false);
  assert.equal(last().url, '/api/admin/plugins/p/enabled');
  assert.deepEqual(JSON.parse(last().opts.body), { enabled: false });
  await api.savePluginConfig('p', { issuer: 'x' });
  assert.equal(last().url, '/api/admin/plugins/p/config');
  await api.restartPlugin('p');
  assert.equal(last().url, '/api/admin/plugins/p/restart');
  await api.removePlugin('p');
  assert.equal(last().opts.method, 'DELETE');
  await api.setPluginUpdateSource('p', 'https://x.test/index.json');
  assert.equal(last().url, '/api/admin/plugins/p/update-source');
  assert.deepEqual(JSON.parse(last().opts.body), { url: 'https://x.test/index.json' });
  await api.checkPluginUpdate('p');
  assert.equal(last().url, '/api/admin/plugins/p/check-update');
  await api.updatePlugin('p');
  assert.equal(last().url, '/api/admin/plugins/p/update');
  assert.equal(last().opts.method, 'POST');
  await api.setExternalProviderEnabled('x-p', true);
  assert.equal(last().url, '/api/admin/providers/external/x-p/enabled');
  await api.getExternalProviderLog('x-p');
  assert.equal(last().url, '/api/admin/providers/external/x-p/log');
  await api.clearExternalProviderLog('x-p');
  assert.equal(last().opts.method, 'DELETE');
});

test('a bundle upload is a multipart form, not JSON', async () => {
  globalThis.FormData ||= class { constructor() { this.parts = []; } append(k, v) { this.parts.push([k, v]); } };
  await api.installPluginFromFile('the-file');
  assert.equal(last().url, '/api/admin/plugins/install');
  assert.equal(last().opts.method, 'POST');
  assert.equal(last().opts.credentials, 'include');
  assert.equal(last().opts.headers, undefined); // the browser sets the multipart boundary
});
