import React, { useEffect, useState } from 'react';
import { useLocation, useNavigationType } from 'react-router-dom';
import { ThemeScope } from './ui';
import { getMediaItems, getSession, logout } from './api';
import { loadAppUrls } from './appLinks';
import Login from './pages/Login';
import { useAppLock } from './components/AppLock';

// Pieces shared by the two ReelStash apps: the media library (App.jsx) and the admin + files
// console (AdminApp.jsx).

// The media app has one dark and one light theme (ui/tokens.css); the accent comes from the admin's
// colour theme (colorThemes.js).
export const MEDIA_DARK = 'dark';
export const MEDIA_LIGHT = 'light';

export function useRecentMedia() {
  const [recentItems, setRecentItems] = useState([]);
  const [seenItemId, setSeenItemId] = useState(() => Number(localStorage.getItem('reelstash-seen-media-id') || 0));
  useEffect(() => {
    // The newest titles, plus the newest with anyone the person asked to hear about.
    Promise.all([getMediaItems({ limit: 5 }), getMediaItems({ limit: 5, notifyOnly: 'true' }).catch(() => ({ items: [] }))]).then(([latest, followed]) => {
      const seen = new Set();
      const merged = [...(followed.items || []).map(item => ({ ...item, followed: true })), ...(latest.items || [])].filter(item => !seen.has(item.id) && seen.add(item.id));
      setRecentItems(merged.sort((a, b) => String(b.dateAdded || '').localeCompare(String(a.dateAdded || ''))).slice(0, 8));
    }).catch(() => {});
  }, []);
  const markSeen = () => {
    if (!recentItems.length) return;
    const latest = Math.max(...recentItems.map(item => Number(item.id)));
    setSeenItemId(latest);
    localStorage.setItem('reelstash-seen-media-id', String(latest));
  };
  return { recentItems, unseen: recentItems.some(item => Number(item.id) > seenItemId), markSeen };
}

// Checks the session (and learns where the partner app lives) before rendering the app;
// shows the sign-in screen when there's no session. children is called with (session, signOut).
export function AuthGate({ appName, logo, children }) {
  const [session, setSession] = useState(null);
  const [authChecked, setAuthChecked] = useState(false);
  const appLock = useAppLock();
  const refreshAppLock = appLock.refresh;
  useEffect(() => {
    let active = true;
    Promise.all([getSession().then(result => { if (active) setSession(result); }), loadAppUrls()])
      .finally(() => { if (active) setAuthChecked(true); });
    return () => { active = false; };
  }, []);
  useEffect(() => { if (session?.id) refreshAppLock(); }, [session?.id, refreshAppLock]);

  if (!authChecked) return <div className="auth-loading">Loading {appName}…</div>;
  if (!session?.id) return <ThemeScope theme={MEDIA_DARK}><Login appName={appName} logo={logo} onLoginSuccess={() => getSession().then(setSession)} /></ThemeScope>;
  return children(session, () => logout().finally(() => setSession(null)));
}

// List pages that remember and restore their own scroll position (see useScrollRestoration) —
// this reset must leave them alone, on the way in and on the way back.
const RESTORED_PATHS = ['/library', '/favorites', '/watch-later', '/search', '/performers', '/studios', '/series', '/categories'];

// A performer/studio/series/category page restores too, but only when coming back to it.
const RESTORED_DETAIL = /^\/(performers|studios|series|categories)\/[^/]+$/;

// The media app header floats clear over the page until it scrolls, then becomes the frosted bar
// (styled off html[data-scrolled]). Depending on the layout the window or the shell's main pane scrolls.
export function HeaderScrollState() {
  useEffect(() => {
    const root = document.documentElement;
    const sync = event => {
      const target = event?.target;
      if (target && target !== document && target !== document.scrollingElement) return; // shelves, panels, etc.
      const y = target && target !== document ? target.scrollTop : window.scrollY;
      if (y > 8) root.dataset.scrolled = ''; else delete root.dataset.scrolled;
    };
    sync();
    document.addEventListener('scroll', sync, { capture: true, passive: true });
    return () => { document.removeEventListener('scroll', sync, { capture: true }); delete root.dataset.scrolled; };
  }, []);
  return null;
}

export function ScrollReset() {
  const { pathname, state } = useLocation();
  const navType = useNavigationType();
  const returning = navType === 'POP' || Boolean(state?.restoreScroll);
  useEffect(() => {
    if (RESTORED_PATHS.includes(pathname)) return;
    if (returning && RESTORED_DETAIL.test(pathname)) return;
    window.scrollTo(0, 0);
    document.querySelectorAll('main, .rs-main').forEach(el => {
      if (el.scrollTop > 0) el.scrollTop = 0;
    });
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pathname]);
  return null;
}
