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

// An admin can say why they made a change; the server records it in the audit log.
function request(method, url, body, { reason } = {}) {
  const opts = { method, credentials: 'include', headers: {} };
  if (reason) opts.headers['X-Admin-Reason'] = reason;
  if (currentLibraryId) opts.headers['X-Library'] = String(currentLibraryId);
  if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }
  return fetch(url, opts).then(handleResponse);
}

const get = url => request('GET', url);
const post = (url, body, opts) => request('POST', url, body ?? {}, opts);
const put = (url, body, opts) => request('PUT', url, body ?? {}, opts);
const del = (url, opts) => request('DELETE', url, undefined, opts);
const query = params => new URLSearchParams(Object.entries(params).filter(([, v]) => v !== '' && v != null)).toString();

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
// Title matching is provider-agnostic; `providers` is a list of provider ids (empty = all enabled).
export const searchMediaProviders = (id, q = '', providers = [], type = 'all') => get(`/api/media/${id}/match?q=${encodeURIComponent(q)}${providers.length ? `&providers=${encodeURIComponent(providers.join(','))}` : ''}${type !== 'all' ? `&type=${encodeURIComponent(type)}` : ''}`);
export const matchMediaProvider = (id, provider, externalId, merge = false) => post(`/api/media/${id}/match`, { provider, externalId, merge });
export const getMediaMatchState = id => get(`/api/media/${id}/match/state`);
export const getMediaLinkedRecords = id => get(`/api/media/${id}/match/linked`);
export const unmatchMediaProvider = (id, options) => post(`/api/media/${id}/match/unmatch`, options);
export const deleteMediaItem = id => del(`/api/admin/media/${id}`);
export const updateMediaMetadata = (id, patch) => put(`/api/media/${id}/metadata`, patch);
export const deleteMediaPoster = id => del(`/api/media/${id}/poster`);
export const deleteMediaBackdrop = id => del(`/api/media/${id}/backdrop`);
export async function uploadMediaPoster(id, file) {
  const form = new FormData();
  form.append('file', file);
  const response = await fetch(`/api/media/${id}/poster`, { method: 'POST', credentials: 'include', body: form });
  return handleResponse(response);
}
export async function uploadMediaBackdrop(id, file) {
  const form = new FormData();
  form.append('file', file);
  const response = await fetch(`/api/media/${id}/backdrop`, { method: 'POST', credentials: 'include', body: form });
  return handleResponse(response);
}
export const createMediaItem = payload => post('/api/media', payload);

// Poster from the video itself: preview any position, then use it (or let the server pick a good one).
export const mediaFrameUrl = (id, seconds, width = 640) => `/api/media/${id}/frame?t=${Number(seconds).toFixed(1)}&w=${width}`;
export const setPosterFromFrame = (id, options) => post(`/api/media/${id}/poster/frame`, options);
export const setBackdropFromFrame = (id, options) => post(`/api/media/${id}/backdrop/frame`, options);

// Scene markers (private to the signed-in user).
export const getMarkers = id => get(`/api/media/${id}/markers`);
export const createMarker = (id, seconds, title) => post(`/api/media/${id}/markers`, { seconds, title });
export const updateMarker = (id, markerId, patch) => request('PATCH', `/api/media/${id}/markers/${markerId}`, patch);
export const deleteMarker = (id, markerId) => del(`/api/media/${id}/markers/${markerId}`);

// Scrub-preview sprites and hover previews are built on demand; these ask for them.
export const getSpriteInfo = id => get(`/api/media/${id}/sprite.json`);
export const requestSprite = id => post(`/api/media/${id}/sprite`);
export const previewUrl = id => `/api/media/${id}/preview`;
export const requestPreview = id => post(`/api/media/${id}/preview`);

export const addWatchLater = id => put(`/api/watch-later/${id}`);
export const removeWatchLater = id => del(`/api/watch-later/${id}`);

export const getNotifications = (limit = 20) => get(`/api/notifications?limit=${limit}`);
export const markNotificationsRead = () => post('/api/notifications/read');

export const getWebhooks = () => get('/api/admin/webhooks');
export const createWebhook = payload => post('/api/admin/webhooks', payload);
export const updateWebhook = (id, payload) => put(`/api/admin/webhooks/${id}`, payload);
export const deleteWebhook = (id, opts) => del(`/api/admin/webhooks/${id}`, opts);
export const getWebhookDeliveries = id => get(`/api/admin/webhooks/${id}/deliveries`);
export const testWebhook = id => post(`/api/admin/webhooks/${id}/test`);
export const getPlaybackInfo = id => get(`/api/media/${id}/playback-info`);
export const streamUrl = (id, fileId) => `/api/media/${id}/stream${fileId ? `?fileId=${fileId}` : ''}`;
export const artworkUrl = (id, kind = 'thumbnail', version) => `/api/media/${id}/artwork?kind=${kind}${version ? `&v=${version}` : ''}`;
export const updatePlayback = (mediaItemId, positionSeconds, completed) =>
  put(`/api/playback/${mediaItemId}`, { positionSeconds, completed });
export const startWatchSession = mediaItemId => post(`/api/playback/${mediaItemId}/session`);
export const updateWatchSession = (sessionId, body, keepalive = false) =>
  fetch(`/api/playback/session/${sessionId}`, { method: 'PUT', credentials: 'include', keepalive, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
export const getHomeRecommendations = () => get('/api/recommendations/home');
export const getWatchActivity = (days = 30) => get(`/api/activity?days=${days}`);
export const getContinueWatching = (params = {}) => get(`/api/playback/continue?${new URLSearchParams(params)}`);
export const getNextUp = (params = {}) => get(`/api/playback/next-up?${new URLSearchParams(params)}`);
export const getNextInSeries = id => get(`/api/media/${id}/next`);

// Audio and subtitle tracks, and the current user's language defaults.
export const getTracks = id => get(`/api/media/${id}/tracks`);
export const subtitleUrl = (id, trackId) => `/api/media/${id}/subtitles/${trackId}.vtt`;
// Admin: read a title's tracks again, and find, save or remove subtitle files beside it.
export const refreshTracks = id => post(`/api/media/${id}/tracks/refresh`);
export const searchSubtitles = (id, params = {}) => get(`/api/media/${id}/subtitles/search?${new URLSearchParams(params)}`);
export const downloadSubtitle = (id, payload) => post(`/api/media/${id}/subtitles`, payload);
export const deleteSubtitle = (id, trackId) => del(`/api/media/${id}/subtitles/${trackId}`);
export const getPreferences = () => get('/api/preferences');
export const updatePreferences = payload => put('/api/preferences', payload);

export const getPerformers = (params = {}) => get(`/api/performers?${new URLSearchParams(params)}`);
export const getPerformer = id => get(`/api/performers/${id}`);
export const performerImageUrl = (id, version) => `/api/performers/${id}/image${version ? `?v=${version}` : ''}`;
export const studioImageUrl = (id, imageId) => `/api/studios/${id}/image${imageId ? `?v=${imageId}` : ''}`;
const providerQuery = providers => (providers && providers.length ? `&providers=${encodeURIComponent(providers.join(','))}` : '');
export const searchStudioProviders = (id, q, providers = []) => get(`/api/studios/${id}/match?q=${encodeURIComponent(q)}${providerQuery(providers)}`);
export const matchStudioProvider = (id, provider, externalId) => post(`/api/studios/${id}/match`, { provider, externalId });
export const getStudioMatchJob = id => get(`/api/studios/jobs/${id}`);
export const unmatchStudioProvider = (id, options) => post(`/api/studios/${id}/match/unmatch`, options);
export const startStudioSync = () => post('/api/admin/studios/sync');
export const getStudioSync = () => get('/api/admin/studios/sync');
export const searchPerformerProviders = (id, q, providers = []) => get(`/api/performers/${id}/match?q=${encodeURIComponent(q)}${providerQuery(providers)}`);
export const matchPerformerProvider = (id, provider, externalId) => post(`/api/performers/${id}/match`, { provider, externalId });
export const getPerformerMatchJob = id => get(`/api/performers/jobs/${id}`);
export const startPerformerSync = (retryUnresolved = false) => post('/api/admin/performers/sync', { retryUnresolved });
export const getEmptyPerformers = () => get('/api/admin/performers/empty');
export const getEmptyCategories = () => get('/api/admin/categories/empty');
export const cleanupCategories = () => post('/api/admin/categories/cleanup', {});
export const cleanupPerformers = () => post('/api/admin/performers/cleanup', {});
export const getPerformerSync = () => get('/api/admin/performers/sync');
export const getRelatedPerformers = (id, limit = 12) => get(`/api/performers/${id}/related?limit=${limit}`);
export const createPerformer = payload => post('/api/performers', payload);
export const updatePerformer = (id, payload) => put(`/api/performers/${id}`, payload);
export const deletePerformer = id => del(`/api/performers/${id}`);
export const getPerformerMedia = id => get(`/api/performers/${id}/media`);
export const linkPerformerMedia = (id, mediaItemId) => post(`/api/performers/${id}/media`, { mediaItemId });
export const unlinkPerformerMedia = (id, mediaItemId) => del(`/api/performers/${id}/media/${mediaItemId}`);
export const unmatchPerformerProvider = (id, options) => post(`/api/performers/${id}/match/unmatch`, options);
export const setPerformerFavorite = (id, favorite) => put(`/api/performers/${id}/favorite`, { favorite });
export const deletePerformerImage = id => del(`/api/performers/${id}/image`);
export const getPerformerFieldSources = id => get(`/api/performers/${id}/field-sources`);
export const setPerformerFieldSources = (id, sources) => put(`/api/performers/${id}/field-sources`, { sources });
export const getPerformerPhotos = id => get(`/api/performers/${id}/photos`);
export const performerPhotoUrl = (id, photoId) => `/api/performers/${id}/photos/${photoId}`;
export const choosePerformerPhoto = (id, photoId) => put(`/api/performers/${id}/photos/${photoId}/primary`, {});
export const deletePerformerPhoto = (id, photoId) => del(`/api/performers/${id}/photos/${photoId}`);
export const fetchPerformerPhotos = id => post(`/api/performers/${id}/photos/fetch`, {});
export const mergePerformer = (keepId, sourceId) => post(`/api/performers/${keepId}/merge`, { sourceId });
export const getPerformerDuplicates = (performerId) => get(`/api/performers/duplicates${performerId ? `?performerId=${performerId}` : ''}`);
export const dismissPerformerDuplicate = (a, b) => post('/api/performers/duplicates/dismiss', { a, b });
export async function uploadPerformerImage(id, file) {
  const form = new FormData();
  form.append('file', file);
  const response = await fetch(`/api/performers/${id}/image`, { method: 'POST', credentials: 'include', body: form });
  return handleResponse(response);
}

export const getStudios = (params = {}) => get(`/api/studios?${new URLSearchParams(params)}`);
export const getStudio = id => get(`/api/studios/${id}`);
export const deleteStudioImage = id => del(`/api/studios/${id}/image`);
export async function uploadStudioImage(id, file) {
  const form = new FormData();
  form.append('file', file);
  const response = await fetch(`/api/studios/${id}/image`, { method: 'POST', credentials: 'include', body: form });
  return handleResponse(response);
}
export const createStudio = payload => post('/api/studios', payload);
export const updateStudio = (id, payload) => put(`/api/studios/${id}`, payload);
export const deleteStudio = id => del(`/api/studios/${id}`);
export const mergeStudio = (keepId, sourceId) => post(`/api/studios/${keepId}/merge`, { sourceId });
export const getStudioDuplicates = id => get(`/api/studios/${id}/duplicates`);
export const linkStudioMedia = (id, mediaItemId) => post(`/api/studios/${id}/media`, { mediaItemId });
export const unlinkStudioMedia = (id, mediaItemId) => del(`/api/studios/${id}/media/${mediaItemId}`);

export const getSeries = (params = {}) => get(`/api/series?${new URLSearchParams(params)}`);
export const createSeries = payload => post('/api/series', payload);
export const updateSeries = (id, payload) => put(`/api/series/${id}`, payload);
export const deleteSeries = id => del(`/api/series/${id}`);
export const getSeriesItem = id => get(`/api/series/${id}`);
export const seriesImageUrl = (id, imageId) => `/api/series/${id}/image${imageId ? `?v=${imageId}` : ''}`;
export async function uploadSeriesImage(id, file) {
  const form = new FormData();
  form.append('file', file);
  const response = await fetch(`/api/series/${id}/image`, { method: 'POST', credentials: 'include', body: form });
  return handleResponse(response);
}
export const setSeriesProviderImage = (id, provider) => post(`/api/series/${id}/image/provider`, { provider });
export const deleteSeriesImage = id => del(`/api/series/${id}/image`);
export const seriesBackdropUrl = (id, imageId) => `/api/series/${id}/backdrop${imageId ? `?v=${imageId}` : ''}`;
export async function uploadSeriesBackdrop(id, file) {
  const form = new FormData();
  form.append('file', file);
  const response = await fetch(`/api/series/${id}/backdrop`, { method: 'POST', credentials: 'include', body: form });
  return handleResponse(response);
}
export const deleteSeriesBackdrop = id => del(`/api/series/${id}/backdrop`);
export const addSeriesMedia = (id, mediaItemIds) => post(`/api/series/${id}/media`, { mediaItemIds });
export const removeSeriesMedia = (id, mediaItemId) => del(`/api/series/${id}/media/${mediaItemId}`);
export const orderSeriesMedia = (id, mediaItemIds) => put(`/api/series/${id}/media/order`, { mediaItemIds });
export const searchSeriesProviders = (id, q = '', providers = []) => get(`/api/series/${id}/providers?q=${encodeURIComponent(q)}${providers.length ? `&providers=${encodeURIComponent(providers.join(','))}` : ''}`);
export const linkSeriesProvider = (id, provider, externalId, rename = false, parent = false) => post(`/api/series/${id}/providers`, { provider, externalId, rename, parent });
export const unlinkSeriesProvider = (id, provider) => del(`/api/series/${id}/providers/${encodeURIComponent(provider)}`);
export const getSeriesParts = id => get(`/api/series/${id}/parts`);
export const assignSeriesPart = (id, payload) => post(`/api/series/${id}/parts/assign`, payload);
export const setMediaSeries = (mediaItemId, payload) => put(`/api/media/${mediaItemId}/series`, payload);
// Reels: true always a reel, false never, null decided by the video's shape.
export const setMediaReel = (mediaItemId, override) => put(`/api/media/${mediaItemId}/reel`, { override });

export const getCategories = (params = {}) => get(`/api/categories?${new URLSearchParams(params)}`);
export const createCategory = payload => post('/api/categories', payload);
export const updateCategory = (id, payload) => put(`/api/categories/${id}`, payload);
export const deleteCategory = id => del(`/api/categories/${id}`);

export const getTags = () => get('/api/tags');
export const createTag = payload => post('/api/tags', payload);
export const updateTag = (id, payload) => put(`/api/admin/tags/${id}`, payload);
export const deleteTag = id => del(`/api/admin/tags/${id}`);

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

export const getDuplicates = (params = {}) => get(`/api/duplicates?${new URLSearchParams(params)}`);
export const dismissDuplicate = id => post(`/api/duplicates/${id}/dismiss`);
export const keepDuplicate = (id, mediaFileId, opts) => post(`/api/duplicates/${id}/keep`, mediaFileId ? { mediaFileId } : {}, opts);
export const getTrash = () => get('/api/duplicates/trash');
export const restoreTrash = id => post(`/api/duplicates/trash/${id}/restore`);
export const exportLibrary = () => get('/api/me/library-export');
export const importLibrary = body => post('/api/me/library-import', body);
export const getDuplicateScan = () => get('/api/admin/duplicates/scan');
export const startDuplicateScan = () => post('/api/admin/duplicates/scan');
export const cancelDuplicateScan = () => post('/api/admin/duplicates/scan/cancel');

// ---------------------------------------------------------------------------
// File browser (filesystem-driven; Library above is metadata-driven)
// ---------------------------------------------------------------------------
export const listFiles = (storageLocationId, path = '') =>
  get(`/api/files?${new URLSearchParams({ storageLocationId, path })}`);
export const getFileLocations = () => get('/api/files/locations');
export const fileDownloadUrl = (storageLocationId, path) =>
  `/api/files/download?${new URLSearchParams({ storageLocationId, path })}`;
export const getFileNfo = (storageLocationId, path) =>
  get(`/api/files/nfo?${new URLSearchParams({ storageLocationId, path })}`);
export const saveFileNfo = (storageLocationId, path, content) =>
  put('/api/admin/files/nfo', { storageLocationId: Number(storageLocationId), path, content });
// storageLocationId always arrives here as a string (it comes from a route
// param), but these endpoints decode a JSON body into an int64 field —
// encoding/json rejects a JSON string for a numeric field, so it must be
// coerced to a real number before it's serialized.
export const renameFileEntry = (storageLocationId, path, newName) =>
  put('/api/admin/files', { storageLocationId: Number(storageLocationId), path, newName });
export const deleteFileEntry = (storageLocationId, path, recursive = false) =>
  del(`/api/admin/files?${new URLSearchParams({ storageLocationId, path, ...(recursive ? { recursive: 'true' } : {}) })}`);
// destStorageLocationId (optional) pastes onto another storage location (disk).
export const moveFileEntry = (storageLocationId, path, destPath, destStorageLocationId = storageLocationId) =>
  post('/api/admin/files/move', { storageLocationId: Number(storageLocationId), path, destPath, destStorageLocationId: Number(destStorageLocationId) });
export const copyFileEntry = (storageLocationId, path, destPath, destStorageLocationId = storageLocationId) =>
  post('/api/admin/files/copy', { storageLocationId: Number(storageLocationId), path, destPath, destStorageLocationId: Number(destStorageLocationId) });
// Paste as a background job (FILE_TRANSFER): { mode: 'move'|'copy', storageLocationId, destStorageLocationId, dir, items: [names], destPath }.
export const startFileTransfer = ({ storageLocationId, destStorageLocationId = storageLocationId, ...rest }) =>
  post('/api/admin/files/transfer', { ...rest, storageLocationId: Number(storageLocationId), destStorageLocationId: Number(destStorageLocationId) });
export const createFileDir = (storageLocationId, path, name) =>
  post('/api/admin/files/dir', { storageLocationId: Number(storageLocationId), path, name });

// Upload uses XHR (not the fetch-based `request` helper) so we can report
// progress for large media files.
export function uploadFiles(storageLocationId, path, files, onProgress) {
  const form = new FormData();
  form.append('storageLocationId', storageLocationId);
  form.append('path', path);
  Array.from(files).forEach(file => form.append('files', file));
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/api/admin/files/upload');
    xhr.withCredentials = true;
    if (onProgress) xhr.upload.onprogress = event => { if (event.lengthComputable) onProgress(event.loaded / event.total); };
    xhr.onload = () => {
      let data = {};
      try { data = JSON.parse(xhr.responseText); } catch { /* non-JSON error body */ }
      if (xhr.status >= 200 && xhr.status < 300) resolve(data);
      else reject(new Error(data.error || 'Upload failed'));
    };
    xhr.onerror = () => reject(new Error('Upload failed'));
    xhr.send(form);
  });
}

// ---------------------------------------------------------------------------
// Admin: overview / storage / scans / jobs
// ---------------------------------------------------------------------------
export const getOverview = () => get('/api/admin/overview');
export const getAnalytics = () => get('/api/admin/analytics');
export const getUsageAnalytics = (days = 30, tz = '') => get(`/api/admin/analytics/usage?${new URLSearchParams({ days, tz })}`);
export const getHealth = () => get('/api/admin/health');
// Metadata providers: enabled list for the match modals, full admin management.
export const getProviders = () => get('/api/providers');
export const getAdminProviders = () => get('/api/admin/providers');
export const saveProviderConfig = (id, fields) => put(`/api/admin/providers/${id}/config`, fields);
export const clearProviderConfig = id => del(`/api/admin/providers/${id}/config`);
export const testProvider = id => post(`/api/admin/providers/${id}/test`);
export const getSubtitleProvider = () => get('/api/admin/subtitle-provider');
export const saveSubtitleProvider = fields => put('/api/admin/subtitle-provider', fields);
export const clearSubtitleProvider = () => del('/api/admin/subtitle-provider');
export const testSubtitleProvider = () => post('/api/admin/subtitle-provider/test');
export const addExternalProvider = (baseUrl, token = '') => post('/api/admin/providers/external', { baseUrl, token });
export const refreshExternalProvider = id => post(`/api/admin/providers/external/${id}/refresh`);
export const removeExternalProvider = id => del(`/api/admin/providers/external/${id}`);
export const setExternalProviderEnabled = (id, enabled) => put(`/api/admin/providers/external/${id}/enabled`, { enabled });
export const getExternalProviderLog = id => get(`/api/admin/providers/external/${id}/log`);
export const clearExternalProviderLog = id => del(`/api/admin/providers/external/${id}/log`);
export const saveProviderOrder = ({ order, mode, autoMatch }) => put('/api/admin/providers/order', { order, mode, autoMatch });
// Without params this returns every user; with limit/offset the server pages, searches and filters.
export const getUsers = (params = {}) => get(`/api/admin/users?${query(params)}`);
export const getUser = id => get(`/api/admin/users/${id}`);
export const deleteUser = (id, opts) => del(`/api/admin/users/${id}`, opts);
export const getUserSessions = id => get(`/api/admin/users/${id}/sessions`);
export const revokeUserSessions = (id, opts) => del(`/api/admin/users/${id}/sessions`, opts);
export const revokeUserSession = (id, sessionId, opts) => del(`/api/admin/users/${id}/sessions/${sessionId}`, opts);
export const getAuditEvents = (params = {}) => get(`/api/admin/audit?${query(params)}`);
export const updateUser = (id, payload, opts) => put(`/api/admin/users/${id}`, payload, opts);
export const createUser = payload => post('/api/admin/users', payload);
export const resetUserPassword = (id, password, opts) => put(`/api/admin/users/${id}/password`, { password }, opts);
export const getAuthSettings = () => get('/api/admin/auth');
export const saveAuthSettings = (nativeEnabled, opts) => put('/api/admin/auth', { nativeEnabled }, opts);
export const setUserLibraries = (id, libraryIds, opts) => put(`/api/admin/users/${id}/libraries`, { libraryIds }, opts);

// Libraries: the media app's switcher lists the caller's; admins manage all.
export const getMyLibraries = () => get('/api/libraries');
export const setDefaultLibrary = libraryId => put('/api/libraries/default', { libraryId });
export const getLibraries = () => get('/api/admin/libraries');
export const startLibraryScan = id => post(`/api/admin/libraries/${id}/scan`);
export const cancelLibraryScan = id => post(`/api/admin/libraries/${id}/scan/cancel`);
export const createLibrary = payload => post('/api/admin/libraries', payload);
export const updateLibrary = (id, payload) => put(`/api/admin/libraries/${id}`, payload);
export const deleteLibrary = (id, opts) => del(`/api/admin/libraries/${id}`, opts);
export const reorderLibraries = ids => put('/api/admin/libraries/order', { ids });
export const getStorageLocations = () => get('/api/admin/storage');
export const createStorageLocation = payload => post('/api/admin/storage', payload);
export const updateStorageLocation = (id, payload) => put(`/api/admin/storage/${id}`, payload);
export const deleteStorageLocation = (id, opts) => del(`/api/admin/storage/${id}`, opts);
export const getStorageInsights = (id, days = 30) => get(`/api/admin/storage/${id}/insights?days=${days}`);
export const getScanRules = () => get('/api/admin/scan-rules');
export const createScanRule = rule => post('/api/admin/scan-rules', rule);
export const testScanRule = rule => post('/api/admin/scan-rules/test', rule);
export const deleteScanRule = id => del(`/api/admin/scan-rules/${id}`);
export const startScan = storageLocationId => post(`/api/admin/scans/${storageLocationId}`);
export const cancelScan = storageLocationId => post(`/api/admin/scans/${storageLocationId}/cancel`);
export const getScanJobs = () => get('/api/admin/scans');
export const getBackgroundJobs = ({ status = '', kind = '', limit = 100, offset = 0 } = {}) =>
  get(`/api/admin/jobs?${new URLSearchParams({ status, kind, limit, offset })}`);
// Library kinds (SCAN, ORGANIZE, METADATA_MATCH) take a library id; the rest take nothing.
export const runJob = (kind, libraryId, opts) => post('/api/admin/jobs/run', { kind, libraryId: libraryId || 0 }, opts);
export const cancelJob = id => post(`/api/admin/jobs/${id}/cancel`);
export const retryJob = id => post(`/api/admin/jobs/${id}/retry`);
export const clearFinishedJobs = () => del('/api/admin/jobs/finished');
export const getBackgroundJob = id => get(`/api/admin/jobs/${id}`);

// ---------------------------------------------------------------------------
// Admin: organizer
// ---------------------------------------------------------------------------
// Rules, layout settings, previews, runs and cleanup are all per library.
export const getOrganizerRules = libraryId => get(`/api/admin/organizer/${libraryId}/rules`);
export const createOrganizerRule = (libraryId, payload) => post(`/api/admin/organizer/${libraryId}/rules`, payload);
export const updateOrganizerRule = (libraryId, id, payload) => put(`/api/admin/organizer/${libraryId}/rules/${id}`, payload);
export const deleteOrganizerRule = (libraryId, id) => del(`/api/admin/organizer/${libraryId}/rules/${id}`);
export const reorderOrganizerRules = (libraryId, ids) => put(`/api/admin/organizer/${libraryId}/rules/order`, { ids });
export const getOrganizerSettings = libraryId => get(`/api/admin/organizer/${libraryId}/settings`);
export const updateOrganizerSettings = (libraryId, payload) => put(`/api/admin/organizer/${libraryId}/settings`, payload);
// The organizer works on a whole library, across every disk it has folders on.
export const previewOrganizer = (libraryId, params = {}) =>
  get(`/api/admin/organizer/${libraryId}/preview?${new URLSearchParams(params)}`);
export const runOrganizer = libraryId => post(`/api/admin/organizer/${libraryId}/run`);
export const previewCleanup = libraryId => get(`/api/admin/organizer/${libraryId}/cleanup`);
export const runCleanup = libraryId => post(`/api/admin/organizer/${libraryId}/cleanup`);

// ---------------------------------------------------------------------------
// Admin: rename pipeline / review queue / namer (metadata provider matching)
// ---------------------------------------------------------------------------
export const getRenameQueue = (params = {}) => get(`/api/admin/rename?${new URLSearchParams(params)}`);
export const previewRename = mediaFileId => get(`/api/admin/rename/preview/${mediaFileId}`);
export const applyRename = mediaFileId => post(`/api/admin/rename/${mediaFileId}`);

export const getReviewQueue = (params = {}) => get(`/api/admin/review?${new URLSearchParams(params)}`);
export const getAdminVideos = (params = {}) => get(`/api/admin/videos?${new URLSearchParams(params)}`);
export const matchAdminVideos = ids => post('/api/admin/videos/match', { ids });
export const confirmAdminVideos = ids => post('/api/admin/videos/confirm', { ids });
export const getMatchSuggestions = (params = {}) => get(`/api/admin/review/matches?${new URLSearchParams(params)}`);
export const dismissMatchSuggestion = mediaItemId => post(`/api/admin/review/matches/${mediaItemId}/dismiss`, {});
export const confirmReview = mediaItemId => post(`/api/admin/review/${mediaItemId}/confirm`);

export const namerMatch = mediaFileId => post(`/api/admin/namer/match/${mediaFileId}`);
export const linkMediaProvider = (id, provider, externalId) => post(`/api/media/${id}/links`, { provider, externalId });
export const startLinkRecovery = () => post('/api/admin/providers/recover-links');
export const namerSelect = (mediaFileId, provider, externalId) => post(`/api/admin/namer/select/${mediaFileId}`, { provider, externalId });

// ---------------------------------------------------------------------------
// Admin: automation (scheduled scans, file watching)
// ---------------------------------------------------------------------------
export const getAutomation = () => get('/api/admin/automation');
export const createSchedule = payload => post('/api/admin/schedules', payload);
export const updateSchedule = (id, payload) => put(`/api/admin/schedules/${id}`, payload);
export const deleteSchedule = (id, opts) => del(`/api/admin/schedules/${id}`, opts);
export const getAppearance = () => get('/api/admin/appearance');
export const updateAppearance = colorTheme => put('/api/admin/appearance', { colorTheme });
export const getHousekeeping = () => get('/api/admin/housekeeping');
export const runHousekeeping = (tasks, dryRun, opts) => post('/api/admin/housekeeping/run', { tasks, dryRun }, opts);
export const updateHousekeepingSettings = settings => put('/api/admin/housekeeping/settings', settings);
export const setLibraryWatch = (id, enabled) => put(`/api/admin/libraries/${id}/watch`, { enabled });

// ---------------------------------------------------------------------------
// Admin: first-run setup wizard
// ---------------------------------------------------------------------------
export const getSetupStatus = () => get('/api/admin/setup');
export const checkSetupPath = path => post('/api/admin/setup/check-path', { path });
export const completeSetup = () => post('/api/admin/setup/complete');

// Plugin bundles: code ReelStash installs and runs itself (metadata providers, sign-in methods).
export const getPlugins = () => get('/api/admin/plugins');
export const installPluginFromUrl = url => post('/api/admin/plugins/install-url', { url });
export async function installPluginFromFile(file) {
  const body = new FormData();
  body.append('bundle', file);
  const response = await fetch('/api/admin/plugins/install', { method: 'POST', credentials: 'include', body });
  return handleResponse(response);
}
export const setPluginEnabled = (id, enabled) => put(`/api/admin/plugins/${id}/enabled`, { enabled });
export const savePluginConfig = (id, values) => put(`/api/admin/plugins/${id}/config`, values);
export const restartPlugin = id => post(`/api/admin/plugins/${id}/restart`);
export const getPluginLog = id => get(`/api/admin/plugins/${id}/log`);
export const removePlugin = (id, opts) => del(`/api/admin/plugins/${id}`, opts);
export const setPluginUpdateSource = (id, url) => put(`/api/admin/plugins/${id}/update-source`, { url });
export const checkPluginUpdate = id => post(`/api/admin/plugins/${id}/check-update`);
export const updatePlugin = id => post(`/api/admin/plugins/${id}/update`);

// ---------------------------------------------------------------------------
// Downloads (qBittorrent), admin only
// ---------------------------------------------------------------------------
export const getDownloads = () => get('/api/downloads');
export const torrentAction = (hashes, action) => post('/api/downloads/torrents/action', { hashes, action });
export const deleteTorrents = (hashes, deleteFiles) => post('/api/downloads/torrents/delete', { hashes, deleteFiles });
export const setTorrentCategory = (hashes, category) => post('/api/downloads/torrents/category', { hashes, category });
export const setTorrentTags = (hashes, { add = [], remove = [] }) => post('/api/downloads/torrents/tags', { hashes, add, remove });
export const moveTorrents = (hashes, storageLocationId, path, scan = true) =>
  post('/api/downloads/torrents/move', { hashes, storageLocationId: Number(storageLocationId), path, scan });
export const getTorrentDetails = hash => get(`/api/downloads/torrents/${encodeURIComponent(hash)}`);
export const getTorrentPeers = hash => get(`/api/downloads/torrents/${encodeURIComponent(hash)}/peers`);
export const renameTorrent = (hash, name) => post(`/api/downloads/torrents/${encodeURIComponent(hash)}/rename`, { name });
export const setTorrentFilePriority = (hash, ids, priority) => post(`/api/downloads/torrents/${encodeURIComponent(hash)}/files`, { ids, priority });
export const setTorrentLimits = (hash, limits) => post(`/api/downloads/torrents/${encodeURIComponent(hash)}/limits`, limits);
export const applyTorrentRules = hash => post(`/api/downloads/torrents/${encodeURIComponent(hash)}/apply-rules`);
// Magnet links / URLs (one per line) and .torrent files, with a save folder and options.
export async function addTorrents({ urls = '', files = [], storageLocationId, path = '', category = '', tags = '', stopped = false, sequential = false, firstLast = false, skipChecking = false }) {
  const body = new FormData();
  body.append('urls', urls);
  Array.from(files).forEach(file => body.append('torrents', file));
  if (storageLocationId) { body.append('storageLocationId', String(storageLocationId)); body.append('path', path); }
  body.append('category', category);
  body.append('tags', tags);
  if (stopped) body.append('stopped', 'true');
  if (sequential) body.append('sequential', 'true');
  if (firstLast) body.append('firstLast', 'true');
  if (skipChecking) body.append('skipChecking', 'true');
  const response = await fetch('/api/downloads/add', { method: 'POST', credentials: 'include', body });
  return handleResponse(response);
}
export const createTorrentCategory = (name, folder) => post('/api/downloads/categories', { name, ...(folder || {}) });
export const editTorrentCategory = (name, folder) => put(`/api/downloads/categories/${encodeURIComponent(name)}`, folder || {});
export const deleteTorrentCategory = name => del(`/api/downloads/categories/${encodeURIComponent(name)}`);
export const createTorrentTag = name => post('/api/downloads/tags', { name });
export const deleteTorrentTag = name => del(`/api/downloads/tags/${encodeURIComponent(name)}`);
export const toggleAltSpeed = () => post('/api/downloads/alt-speed');
export const getTorrentPreferences = () => get('/api/downloads/preferences');
export const saveTorrentPreferences = values => put('/api/downloads/preferences', values);
export const getDownloadSettings = () => get('/api/downloads/settings');
export const saveDownloadSettings = values => put('/api/downloads/settings', values);
export const testDownloadSettings = values => post('/api/downloads/settings/test', values);
export const saveDownloadFolders = folders => put('/api/downloads/folders', folders);
export const fixDownloadFolders = fix => post('/api/downloads/folders/fix', { fix });
export const getDownloadRules = () => get('/api/downloads/rules');
export const createDownloadRule = rule => post('/api/downloads/rules', rule);
export const updateDownloadRule = (id, rule) => put(`/api/downloads/rules/${id}`, rule);
export const deleteDownloadRule = id => del(`/api/downloads/rules/${id}`);
export const reorderDownloadRules = ids => put('/api/downloads/rules/order', { ids });
export const testDownloadRules = () => get('/api/downloads/rules/test');
export const getDownloadHistory = (limit = 100) => get(`/api/downloads/history?limit=${limit}`);
export const forgetDownload = hash => del(`/api/downloads/history/${encodeURIComponent(hash)}`);
