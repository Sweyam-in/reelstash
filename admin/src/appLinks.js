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
