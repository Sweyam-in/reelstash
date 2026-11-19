import React, { useEffect, useState } from 'react';
import { BrowserRouter, Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import SurpriseButton from './components/SurpriseButton';
import { Bookmark, Clapperboard, Clock3, Film, Heart, House, Library, LibraryBig, ListVideo, Smartphone, Tv, Users, Building2, Layers3 } from 'lucide-react';
import { Button, ThemeScope, Toaster } from './ui';
import AppFrame from './frame/AppFrame';
import You from './pages/You';
import logo from './assets/logo.svg';
import Duplicates from './pages/Duplicates';
import { Home, Catalog, Browse, BrowseDetail, MediaDetail, backLabel } from './pages/Catalog';
import { Playlists, PlaylistDetail } from './pages/Playlists';
import Activity from './pages/Activity';
import Reels from './pages/Reels';
import { AppLockProvider } from './components/AppLock';
import { NotificationsProvider } from './components/Notifications';
import { AuthGate, HeaderScrollState, MEDIA_DARK, ScrollReset, useRecentMedia } from './shell';
import { adminHref } from './appLinks';
import { LibraryProvider, useLibrary } from './libraryContext';
import MediaSettingsPage, { MEDIA_THEME_EVENT, resolveMediaTheme } from './pages/UserSettings';
import ShowDetail from './pages/ShowDetail';
import { LinkDevicePage } from './components/Devices';
import SeriesDetail from './pages/SeriesDetail';
import { useCardStyle } from './cardStyle';
import { useMediaColorTheme } from './useColorTheme';

// The ReelStash media library. Admin and Files are a separate app (AdminApp.jsx, admin.html)
// on their own origin; links into them are full page loads via adminHref.

// The sidebar, in the words of the library's mode (see libraryModes.js).
function mediaGroups(terms, mode) {
  if (mode === 'entertainment') return [
    { label: 'LIBRARY', links: [['Home', '/', House], [terms.movies, '/movies', Film], [terms.shows, '/shows', Tv], ['Reels', '/reels', Smartphone], ['Favorites', '/favorites', Heart], ['Watch later', '/watch-later', Bookmark], ['Playlists', '/playlists', ListVideo]] },
    { label: 'DISCOVER', links: [[terms.actors, '/performers', Users], [terms.directors, '/directors', Clapperboard], [terms.studios, '/studios', Building2], [terms.seriesPlural, '/series', Library], [terms.categories, '/categories', Layers3], ['Activity', '/activity', Clock3]] },
  ];
  return [
    { label: 'LIBRARY', links: [['Home', '/', House], [terms.library, '/library', LibraryBig], [terms.seriesPlural, '/series', ListVideo], ['Reels', '/reels', Smartphone], ['Favorites', '/favorites', Heart], ['Watch later', '/watch-later', Bookmark], ['Playlists', '/playlists', ListVideo]] },
    { label: 'DISCOVER', links: [[terms.people, '/performers', Users], [terms.studios, '/studios', Building2], [terms.categories, '/categories', Layers3], ['Activity', '/activity', Clock3]] },
  ];
}

// A signed-in user who has not been given any library yet.
function NoLibraryAccess({ onSignOut }) {
  return <div className="rs-no-library">
    <LibraryBig size={40} />
    <h1>No libraries yet</h1>
    <p>An admin hasn’t given you access to a library. Ask them to add you, then sign in again.</p>
    <Button variant="secondary" onClick={onSignOut}>Sign out</Button>
  </div>;
}

const isAdminAppPath = path => /^\/(admin|files|downloads)(\/|$|\?)/.test(path);

// The media app's theme: the Settings page's choice (dark, light or follow the system); the header
// toggle flips between dark and light.
function useMediaTheme() {
  const [theme, setTheme] = useState(resolveMediaTheme);
  useEffect(() => {
    const sync = () => setTheme(resolveMediaTheme());
    const scheme = window.matchMedia?.('(prefers-color-scheme: light)');
    window.addEventListener(MEDIA_THEME_EVENT, sync);
    scheme?.addEventListener?.('change', sync);
    return () => { window.removeEventListener(MEDIA_THEME_EVENT, sync); scheme?.removeEventListener?.('change', sync); };
  }, []);
  const toggle = next => {
    try { localStorage.setItem('reelstash-media-mode', next); } catch { /* storage blocked */ }
    setTheme(next);
  };
  return [theme, toggle];
}

function mediaBackTarget(location) {
  const { pathname, state } = location;
  if (/^\/library\/[^/]+$/.test(pathname)) return state?.from || '/library';
  const detail = pathname.match(/^\/(performers|studios|series|shows|categories|playlists)\/[^/]+$/);
  return detail ? `/${detail[1]}` : null;
}

function MediaShell({ session, onSignOut, children }) {
  const { terms, library, mode } = useLibrary();
  const groups = mediaGroups(terms, mode);
  const location = useLocation();
  const navigate = useNavigate();
  const [mediaTheme, setMediaTheme] = useMediaTheme();
  const [cardStyle] = useCardStyle();
  useMediaColorTheme();
  const recent = useRecentMedia();
  // Menus, dialogs and toasts portal to <body>; it carries the theme so they match.
  useEffect(() => {
    document.body.dataset.rsTheme = mediaTheme;
    document.documentElement.dataset.rsTheme = mediaTheme;
    return () => { delete document.body.dataset.rsTheme; delete document.documentElement.dataset.rsTheme; };
  }, [mediaTheme]);
  const openLink = path => { if (isAdminAppPath(path)) window.location.assign(adminHref(path)); else navigate(path); };
  const current = [...groups.flatMap(group => group.links), ['Search', '/search'], ['Settings', '/settings'], ['You', '/you']].find(([, path]) => path === location.pathname || (path !== '/' && location.pathname.startsWith(`${path}/`)))?.[0] || 'Library';
  const back = mediaBackTarget(location);
  return <ThemeScope theme={mediaTheme} className={`rs-app${cardStyle === 'thumbnail' ? ' rs-cards-thumbnail' : ''}`}>
    <AppFrame groups={groups} title={current} back={back} backLabel={back ? backLabel(back) : ''} session={session} theme={mediaTheme} onThemeChange={setMediaTheme}
      onSignOut={onSignOut} recent={recent} openLink={openLink} adminHref={adminHref}>
      <div className="rs-page-host" key={library?.id || 'all'}>{children}</div>
    </AppFrame>
  </ThemeScope>;
}

// Old /admin, /files and /downloads bookmarks on the media origin: hand them to the admin app when it lives elsewhere.
function AdminAppRedirect() {
  const location = useLocation();
  const target = adminHref(location.pathname + location.search);
  const elsewhere = target !== location.pathname + location.search;
  useEffect(() => { if (elsewhere) window.location.replace(target); }, [elsewhere, target]);
  return elsewhere ? null : <Navigate to="/" replace />;
}

function MediaRoutes({ session }) {
  return <Routes>
    <Route path="/" element={<Home session={session} />} />
    <Route path="/library" element={<Catalog />} />
    <Route path="/library/:id" element={<MediaDetail isAdmin={session.isAdmin} />} />
    <Route path="/search" element={<Catalog type="search" />} />
    {['performers', 'studios', 'categories'].map(type => <React.Fragment key={type}><Route path={`/${type}`} element={<Browse type={type} />} /><Route path={`/${type}/:name`} element={<BrowseDetail type={type} />} /></React.Fragment>)}
    <Route path="/series" element={<Browse type="series" variant="collections" />} />
    <Route path="/series/:name" element={<SeriesDetail />} />
    <Route path="/movies" element={<Catalog type="movies" />} />
    <Route path="/shows" element={<Browse type="series" variant="shows" />} />
    <Route path="/shows/:name" element={<ShowDetail />} />
    <Route path="/directors" element={<Browse type="performers" variant="directors" />} />
    <Route path="/collections" element={<Navigate to="/playlists" replace />} />
    <Route path="/playlists" element={<Playlists />} />
    <Route path="/playlists/:id" element={<PlaylistDetail />} />
    <Route path="/reels" element={<Reels />} />
    <Route path="/activity" element={<Activity />} />
    <Route path="/settings" element={<MediaSettingsPage />} />
    <Route path="/you" element={<You />} />
    <Route path="/link" element={<LinkDevicePage />} />
    <Route path="/favorites" element={<Catalog type="favorites" />} />
    <Route path="/watch-later" element={<Catalog type="watchlater" />} />
    <Route path="/duplicates" element={<Duplicates />} />
    <Route path="/admin/*" element={<AdminAppRedirect />} />
    <Route path="/files/*" element={<AdminAppRedirect />} />
    <Route path="/downloads/*" element={<AdminAppRedirect />} />
    <Route path="*" element={<Navigate to="/" replace />} />
  </Routes>;
}

function LibraryGate({ session, onSignOut }) {
  const { libraries } = useLibrary();
  if (!libraries.length && !session.isAdmin) return <ThemeScope theme={MEDIA_DARK}><NoLibraryAccess onSignOut={onSignOut} /></ThemeScope>;
  return <MediaShell session={session} onSignOut={onSignOut}><MediaRoutes session={session} /></MediaShell>;
}

export default function App() {
  return <Toaster position="bottom-center"><BrowserRouter><ScrollReset /><HeaderScrollState /><AppLockProvider>
    <AuthGate appName="ReelStash" logo={logo}>{(session, signOut) =>
      <LibraryProvider isAdmin={!!session.isAdmin}><NotificationsProvider enabled={!!session.isAdmin}><LibraryGate session={session} onSignOut={signOut} /></NotificationsProvider></LibraryProvider>}
    </AuthGate>
  </AppLockProvider></BrowserRouter></Toaster>;
}
