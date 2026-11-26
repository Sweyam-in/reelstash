/**
 * Centralized API client for ReelStash's Go backend.
 * Every request carries the session cookie; the server decides
 * authorization, this layer never does.
 */

// The server answers 423 when the PIN app lock is engaged; the lock screen listens for this.
export const APP_LOCKED_EVENT = 'reelstash:app-locked';

async function handleResponse(response) {
  if (response.status === 423) window.dispatchEvent(new Event(APP_LOCKED_EVENT));
  if (!response.ok) {
    const errorData = await response.json().catch(() => ({ error: 'An unknown network error occurred' }));
    const error = new Error(errorData.error || 'Request failed');
    error.status = response.status;
    throw error;
  }
  if (response.status === 204) return null;
  return response.json();
}

// The library the media app is browsing. Sent on every call so the server
// narrows listings to it; the admin console never sets it and sees everything.
let currentLibraryId = null;
export function setCurrentLibrary(id) { currentLibraryId = id || null; }

function request(method, url, body) {
  const opts = { method, credentials: 'include', headers: {} };
  if (currentLibraryId) opts.headers['X-Library'] = String(currentLibraryId);
  if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }
  return fetch(url, opts).then(handleResponse);
}

const get = url => request('GET', url);
const post = (url, body) => request('POST', url, body ?? {});
const put = (url, body) => request('PUT', url, body ?? {});
const del = url => request('DELETE', url);

// ---------------------------------------------------------------------------
// Auth / session
// ---------------------------------------------------------------------------
export async function getSession() {
  try {
    return await get('/api/me');
  } catch {
    return { authenticated: false };
  }
}

export const getAuthConfig = () => get('/auth/config');
export const localLogin = password => post('/auth/local-login', { password });
// Built-in username/password accounts, the first-run admin, and sign-in plugins.
export const nativeLogin = (username, password) => post('/auth/native-login', { username, password });
export const setupAdmin = ({ username, password, displayName }) => post('/auth/setup', { username, password, displayName });
export const pluginLoginUrl = (id, redirectTo = '/') => `/auth/plugin/${encodeURIComponent(id)}/login?redirect_to=${encodeURIComponent(redirectTo)}`;
export const pluginPasswordLogin = (id, username, password) => post(`/auth/plugin/${encodeURIComponent(id)}/login`, { username, password });
export const changeMyPassword = (current, password) => put('/api/me/password', { current, password });
export async function logout() {
  const result = await post('/auth/logout');
  if (result?.logoutUrl) window.location.assign(result.logoutUrl);
  return result;
}

// ---------------------------------------------------------------------------
// App lock (optional PIN)
// ---------------------------------------------------------------------------
// Apps signed in to this account (bearer-token device sessions), and pairing a new one by code.
export const getDevices = () => get('/api/devices');
export const revokeDevice = id => del(`/api/devices/${id}`);
export const lookupDevicePairing = code => get(`/api/devices/pair?code=${encodeURIComponent(code)}`);
export const approveDevicePairing = code => post('/api/devices/pair', { code });
export const getAppLockStatus = () => get('/api/applock');
export const verifyAppLockPin = pin => post('/api/applock/verify', { pin });
export const lockApp = () => post('/api/applock/lock');
export const setAppLockPin = (pin, currentPin) => put('/api/applock/pin', { pin, currentPin });
export const removeAppLockPin = pin => request('DELETE', '/api/applock/pin', { pin });
export const setAppLockIdleMinutes = idleMinutes => put('/api/applock/settings', { idleMinutes });
// scope 'app' locks everything; 'libraries' locks only libraryIds.
export const setAppLockSettings = settings => put('/api/applock/settings', settings);

// ---------------------------------------------------------------------------
// Media library
// ---------------------------------------------------------------------------
export const getMediaItems = (params = {}) => get(`/api/media?${new URLSearchParams(params)}`);
export const getMediaItem = id => get(`/api/media/${id}`);
export const getRelatedMedia = id => get(`/api/media/${id}/related`);
export const updateMediaItem = (id, patch) => request('PATCH', `/api/media/${id}`, patch);
// Scene markers (private to the signed-in user).
export const getMarkers = id => get(`/api/media/${id}/markers`);
export const createMarker = (id, seconds, title) => post(`/api/media/${id}/markers`, { seconds, title });
export const updateMarker = (id, markerId, patch) => request('PATCH', `/api/media/${id}/markers/${markerId}`, patch);
export const deleteMarker = (id, markerId) => del(`/api/media/${id}/markers/${markerId}`);

// Scrub-preview sprites and hover previews are built on demand; these ask for them.
export const getSpriteInfo = id => get(`/api/media/${id}/sprite.json`);
export const requestSprite = id => post(`/api/media/${id}/sprite`);
export const previewUrl = id => `/api/media/${id}/preview`;
export const addWatchLater = id => put(`/api/watch-later/${id}`);
export const removeWatchLater = id => del(`/api/watch-later/${id}`);

// surface=media keeps scan, job, and download alerts in the admin console.
export const getNotifications = (limit = 20) => get(`/api/notifications?limit=${limit}&surface=media`);
export const markNotificationsRead = () => post('/api/notifications/read?surface=media');

export const getPlaybackInfo = id => get(`/api/media/${id}/playback-info`);
export const streamUrl = (id, fileId) => `/api/media/${id}/stream${fileId ? `?fileId=${fileId}` : ''}`;
export const artworkUrl = (id, kind = 'thumbnail', version) => `/api/media/${id}/artwork?kind=${kind}${version ? `&v=${version}` : ''}`;
export const updatePlayback = (mediaItemId, positionSeconds, completed) =>
  put(`/api/playback/${mediaItemId}`, { positionSeconds, completed });
export const startWatchSession = mediaItemId => post(`/api/playback/${mediaItemId}/session`);
export const updateWatchSession = (sessionId, body, keepalive = false) =>
  fetch(`/api/playback/session/${sessionId}`, { method: 'PUT', credentials: 'include', keepalive, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
export const getHomeRecommendations = () => get('/api/recommendations/home');
export const getHomeSummary = () => get('/api/home/summary');
export const getHomeSeries = (limit = 6) => get(`/api/home/series?limit=${limit}`);
export const getWatchActivity = (days = 30) => get(`/api/activity?days=${days}`);
export const getContinueWatching = (params = {}) => get(`/api/playback/continue?${new URLSearchParams(params)}`);
export const getNextUp = (params = {}) => get(`/api/playback/next-up?${new URLSearchParams(params)}`);
export const getNextInSeries = id => get(`/api/media/${id}/next`);

// Audio and subtitle tracks, and the current user's language defaults.
export const getTracks = id => get(`/api/media/${id}/tracks`);
export const subtitleUrl = (id, trackId) => `/api/media/${id}/subtitles/${trackId}.vtt`;
export const getPreferences = () => get('/api/preferences');
export const updatePreferences = payload => put('/api/preferences', payload);

export const getPerformers = (params = {}) => get(`/api/performers?${new URLSearchParams(params)}`);
export const getPerformer = id => get(`/api/performers/${id}`);
export const performerImageUrl = (id, version) => `/api/performers/${id}/image${version ? `?v=${version}` : ''}`;
export const studioImageUrl = (id, imageId) => `/api/studios/${id}/image${imageId ? `?v=${imageId}` : ''}`;
export const getRelatedPerformers = (id, limit = 12) => get(`/api/performers/${id}/related?limit=${limit}`);
export const setPerformerFavorite = (id, favorite) => put(`/api/performers/${id}/favorite`, { favorite });
export const setPerformerNotify = (id, notify) => put(`/api/performers/${id}/notify`, { notify });
export const getStudios = (params = {}) => get(`/api/studios?${new URLSearchParams(params)}`);
export const getStudio = id => get(`/api/studios/${id}`);
export const getSeries = (params = {}) => get(`/api/series?${new URLSearchParams(params)}`);
export const getSeriesItem = id => get(`/api/series/${id}`);
export const seriesImageUrl = (id, imageId) => `/api/series/${id}/image${imageId ? `?v=${imageId}` : ''}`;
export const seriesBackdropUrl = (id, imageId) => `/api/series/${id}/backdrop${imageId ? `?v=${imageId}` : ''}`;
export const getCategories = (params = {}) => get(`/api/categories?${new URLSearchParams(params)}`);
export const getTags = () => get('/api/tags');
export const getCollections = () => get('/api/collections');
export const createCollection = payload => post('/api/collections', payload);
export const getCollection = (id, params = {}) => {
  const query = new URLSearchParams(params).toString();
  return get(`/api/collections/${id}${query ? `?${query}` : ''}`);
};
export const deleteCollection = id => del(`/api/collections/${id}`);
export const setCollectionPinned = (id, pinned) => request('PATCH', `/api/collections/${id}`, { pinned });
export const updateCollection = (id, payload) => request('PATCH', `/api/collections/${id}`, payload);
export const addCollectionItem = (id, mediaItemId) => post(`/api/collections/${id}/items`, { mediaItemId });
export const moveCollectionItem = (id, mediaItemId, position) => post(`/api/collections/${id}/items/${mediaItemId}/move`, { position });
export const removeCollectionItem = (id, mediaItemId) => del(`/api/collections/${id}/items/${mediaItemId}`);

export const search = q => get(`/api/search?q=${encodeURIComponent(q)}`);

export const exportLibrary = () => get('/api/me/library-export');
export const importLibrary = body => post('/api/me/library-import', body);
// Libraries: the media app's switcher lists the caller's; admins manage all.
export const getMyLibraries = () => get('/api/libraries');
export const setDefaultLibrary = libraryId => put('/api/libraries/default', { libraryId });