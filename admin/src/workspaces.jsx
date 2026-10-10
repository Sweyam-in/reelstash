import React, { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Tv } from 'lucide-react';
import { WorkspaceSwitcher } from '@spatika/react';
import { APP_THEMES } from './shell';
import { mediaHref } from './appLinks';
import adminLogo from './assets/logo-admin.svg';
import filesLogo from './assets/logo-files.svg';
import downloadsLogo from './assets/logo-downloads.svg';

// ReelStash Admin (AdminApp.jsx, admin.html), ReelStash Files (FilesApp.jsx, files.html) and
// ReelStash Downloads (DownloadsApp.jsx, downloads.html, admins only) are separate React apps
// deployed together as the reelstash-admin service on their own origin
// (admin.reelstash.example, media.local:8001). Switching between them is a full page load; the media
// library (App.jsx) lives on the media origin and is reached via mediaHref.

const WORKSPACES = {
  admin: { id: 'admin', name: 'ReelStash Admin', path: '/admin', logo: <img src={adminLogo} alt="" style={{ width: '100%', height: '100%' }} /> },
  files: { id: 'files', name: 'ReelStash Files', path: '/files', logo: <img src={filesLogo} alt="" style={{ width: '100%', height: '100%' }} /> },
  downloads: { id: 'downloads', name: 'ReelStash Downloads', path: '/downloads', logo: <img src={downloadsLogo} alt="" style={{ width: '100%', height: '100%' }} /> },
};

export function AppWorkspaceSwitcher({ current, canAdmin }) {
  const workspaces = canAdmin ? [WORKSPACES.admin, WORKSPACES.files, WORKSPACES.downloads] : [WORKSPACES.files];
  return <WorkspaceSwitcher workspaces={workspaces} value={current} onValueChange={id => { if (id !== current) window.location.assign(WORKSPACES[id].path); }} />;
}

// Account-menu entry back to the media library.
export const OPEN_LIBRARY = <button type="button" role="menuitem" onClick={() => window.location.assign(mediaHref('/'))}><Tv size={16} /> Open ReelStash</button>;

export function useStoredTheme(key, fallback) {
  const [theme, setTheme] = useState(() => {
    const saved = localStorage.getItem(key);
    return APP_THEMES.includes(saved) ? saved : fallback;
  });
  useEffect(() => { localStorage.setItem(key, theme); }, [key, theme]);
  return [theme, setTheme];
}

// Opens an app path from a notification or the recently added list: routes in place when this app
// owns it (prefix '/admin', '/files' or '/downloads'), loads the sibling app for another prefix, else the media app.
export function useOpenLink(ownPrefix) {
  const navigate = useNavigate();
  return useCallback(path => {
    const prefix = path.match(/^\/(admin|files|downloads)(?=\/|$|\?)/)?.[0];
    if (prefix === ownPrefix) navigate(path);
    else window.location.assign(prefix ? path : mediaHref(path));
  }, [navigate, ownPrefix]);
}
