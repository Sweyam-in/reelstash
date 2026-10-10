import React, { useEffect, useRef, useState } from 'react';
import { useLocation, useNavigationType } from 'react-router-dom';
import { Bell, Film, Lock, LogOut, Moon, Palette, Sun } from 'lucide-react';
import { Button, SpatikaThemeProvider } from '@spatika/react';
import { getMediaItems, getSession, logout } from './api';
import { loadAppUrls } from './appLinks';
import Login from './pages/Login';
import { useAppLock } from './components/AppLock';
import { useNotifications } from './components/Notifications';
import { timeAgo } from './pages/timeUtil';

// Pieces shared by the two ReelStash apps: the media library (App.jsx) and the admin + files
// console (AdminApp.jsx).

export const APP_THEMES = ['neelam', 'sandhya', 'mukta', 'usha'];

// The media app has just one dark and one light theme (its colour comes from discover.css, not a theme
// accent). Its own key, seeded from the shared one: Midnight/Dusk -> dark, Light/Warm light -> light.
export const MEDIA_DARK = 'neelam';
export const MEDIA_LIGHT = 'mukta';

export function useRecentMedia() {
  const [recentItems, setRecentItems] = useState([]);
  const [seenItemId, setSeenItemId] = useState(() => Number(localStorage.getItem('reelstash-seen-media-id') || 0));
  useEffect(() => { getMediaItems({ limit: 5 }).then(result => setRecentItems(result.items || [])).catch(() => {}); }, []);
  const markSeen = () => {
    if (!recentItems.length) return;
    const latest = Math.max(...recentItems.map(item => Number(item.id)));
    setSeenItemId(latest);
    localStorage.setItem('reelstash-seen-media-id', String(latest));
  };
  return { recentItems, unseen: recentItems.some(item => Number(item.id) > seenItemId), markSeen };
}

// openLink takes an app path (a notification link, a recently added title) and routes it to whichever
// app owns it; accountItems adds entries above "Sign out", e.g. a link across to the other app.
export function HeaderActions({ theme, onThemeChange, themeToggle = false, recentItems, unseen, onMarkSeen, session, onSignOut, openLink, leading, hideNotifications = false, accountItems = null }) {
  const location = useLocation();
  const appLock = useAppLock();
  const notes = useNotifications();
  const [openMenu, setOpenMenu] = useState('');
  const menuRef = useRef(null);
  useEffect(() => { setOpenMenu(''); }, [location.pathname]);
  useEffect(() => {
    const closeOnOutside = event => { if (!menuRef.current?.contains(event.target)) setOpenMenu(''); };
    document.addEventListener('pointerdown', closeOnOutside);
    return () => document.removeEventListener('pointerdown', closeOnOutside);
  }, []);
  return <div className="rs-header-actions" ref={menuRef}>
    {leading}
    {themeToggle
      ? <Button variant="ghost" size="icon-sm" aria-label={theme === MEDIA_LIGHT ? 'Switch to dark theme' : 'Switch to light theme'} title={theme === MEDIA_LIGHT ? 'Dark theme' : 'Light theme'} onClick={() => onThemeChange(theme === MEDIA_LIGHT ? MEDIA_DARK : MEDIA_LIGHT)}>{theme === MEDIA_LIGHT ? <Moon size={18} /> : <Sun size={18} />}</Button>
      : <Button variant="ghost" size="icon-sm" aria-label="Choose theme" aria-expanded={openMenu === 'theme'} onClick={() => setOpenMenu(openMenu === 'theme' ? '' : 'theme')}><Palette size={18} /></Button>}
    {!hideNotifications && <Button variant="ghost" size="icon-sm" aria-label="Notifications" aria-expanded={openMenu === 'notifications'} className="rs-header-notification" onClick={() => { const opening = openMenu !== 'notifications'; setOpenMenu(opening ? 'notifications' : ''); if (opening) { onMarkSeen(); notes.markRead(); } }}><Bell size={18} />{notes.unread > 0 ? <span className="rs-notification-count">{notes.unread > 9 ? '9+' : notes.unread}</span> : unseen && <span />}</Button>}
    <Button variant="ghost" size="icon-sm" aria-label={`Account: ${session?.displayName || session?.email || 'User'}`} aria-expanded={openMenu === 'account'} className="rs-header-account" onClick={() => setOpenMenu(openMenu === 'account' ? '' : 'account')}>{(session?.displayName || session?.email || 'U').slice(0, 1).toUpperCase()}</Button>
    {openMenu === 'theme' && <div className="rs-header-popover" role="menu" aria-label="Appearance"><p className="rs-header-menu-title">Appearance</p>{[['neelam', 'Midnight'], ['sandhya', 'Dusk'], ['mukta', 'Light'], ['usha', 'Warm light']].map(([id, label]) => <button type="button" role="menuitem" key={id} onClick={() => { onThemeChange(id); setOpenMenu(''); }}>{label}{theme === id && <span className="rs-header-menu-check">✓</span>}</button>)}</div>}
    {!hideNotifications && openMenu === 'notifications' && <div className="rs-header-popover rs-notification-menu" role="menu" aria-label="Notifications">{!!notes.items.length && <><p className="rs-header-menu-title">Notifications</p>{notes.items.slice(0, 6).map(note => <button type="button" role="menuitem" key={note.id} onClick={() => { setOpenMenu(''); if (note.link) openLink(note.link); }}><Bell size={16} /><span className="rs-notification-item"><strong>{note.title}</strong><small>{[note.body, timeAgo(note.createdAt)].filter(Boolean).join(' · ')}</small></span></button>)}</>}<p className="rs-header-menu-title">Recently added</p>{recentItems.length ? recentItems.map(item => <button type="button" role="menuitem" key={item.id} onClick={() => { setOpenMenu(''); openLink(`/library/${item.id}`); }}><Film size={16} /><span className="rs-notification-item"><strong>{item.title}</strong><small>{item.studioName || 'Your library'}</small></span></button>) : <p className="rs-header-menu-empty">No recent additions.</p>}</div>}
    {openMenu === 'account' && <div className="rs-header-popover" role="menu" aria-label="Account">{accountItems}{appLock?.hasPin && <button type="button" role="menuitem" onClick={() => { setOpenMenu(''); appLock.lockNow(); }}><Lock size={16} /> Lock now</button>}<button type="button" role="menuitem" onClick={() => { setOpenMenu(''); appLock?.openSettings(); }}><Lock size={16} /> PIN lock…</button><button type="button" role="menuitem" onClick={onSignOut}><LogOut size={16} /> Sign out</button></div>}
  </div>;
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
  if (!session?.id) return <SpatikaThemeProvider theme="neelam" syncDocument={false} className="rs-media-theme"><Login appName={appName} logo={logo} onLoginSuccess={() => getSession().then(setSession)} /></SpatikaThemeProvider>;
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
      if (target && target !== document && !target.classList?.contains('spk-app-main')) return; // shelves, panels, etc.
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
    document.querySelectorAll('main, .rs-media-surface, [class*="spk-"]').forEach(el => {
      if (el.scrollTop > 0) el.scrollTop = 0;
    });
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pathname]);
  return null;
}
