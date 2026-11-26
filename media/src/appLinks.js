import { getAuthConfig } from './api';

// ReelStash ships as two apps: the media library (index.html) and the admin + files
// console (admin.html), each on its own origin in production (reelstash.example /
// admin.reelstash.example, media.local:8000 / :8001). The server tells each host which
// origin its partner app lives on; both empty means they share this origin (dev).
let urls = { mediaUrl: '', adminUrl: '' };
let loading;

export function loadAppUrls() {
  loading ||= getAuthConfig()
    .then(config => { urls = { mediaUrl: config?.mediaUrl || '', adminUrl: config?.adminUrl || '' }; })
    .catch(() => {});
  return loading;
}

// Links across apps are always full page loads: the other app is a different bundle.
export const mediaHref = path => `${urls.mediaUrl}${path}`;
export const adminHref = path => `${urls.adminUrl}${path}`;

// Editing lives in the admin app. These open its editor for one record in a new tab,
// leaving the library where it was.
const EDITOR_PATHS = {
  media: '/admin/videos',
  performers: '/admin/metadata/performers',
  studios: '/admin/metadata/studios',
  series: '/admin/metadata/series',
};

export function adminEditorHref(kind, id, tab) {
  const base = EDITOR_PATHS[kind] || '/admin';
  const query = new URLSearchParams({ edit: String(id) });
  if (tab) query.set('tab', tab);
  return adminHref(`${base}?${query}`);
}

export function editInAdmin(kind, id, tab) {
  window.open(adminEditorHref(kind, id, tab), '_blank', 'noopener');
}
