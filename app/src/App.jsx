import React, { useEffect, useRef, useState } from 'react';
import { BrowserRouter, Link, Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import SurpriseButton from './components/SurpriseButton';
import { ArrowLeft, Bookmark, Clapperboard, Clock3, Film, Heart, House, Library, LibraryBig, ListVideo, Lock, Search, Settings, Smartphone, Tv, Users, Building2, Layers3, ShieldCheck } from 'lucide-react';
import { AppShell, Button, NavItem, NavSection, Sidebar, SpatikaThemeProvider, Toaster, TopBar, WorkspaceSwitcher, useAppShell } from '@spatika/react';
import Duplicates from './pages/Duplicates';
import { Home, Catalog, Browse, BrowseDetail, MediaDetail, backLabel } from './pages/Catalog';
import { Playlists, PlaylistDetail } from './pages/Playlists';
import Activity from './pages/Activity';
import Reels from './pages/Reels';
import { AppLockProvider } from './components/AppLock';
import { NotificationsProvider } from './components/Notifications';
import { AuthGate, HeaderActions, HeaderScrollState, MEDIA_DARK, MEDIA_LIGHT, ScrollReset, useRecentMedia } from './shell';
import { adminHref } from './appLinks';
import { LibraryProvider, useLibrary } from './libraryContext';
import { LIBRARY_MODES } from './libraryModes';
import MediaSettingsPage, { MEDIA_THEME_EVENT, resolveMediaTheme } from './pages/UserSettings';
import ShowDetail from './pages/ShowDetail';
import { LinkDevicePage } from './components/Devices';
import SeriesDetail from './pages/SeriesDetail';
import { useCardStyle } from './cardStyle';
import { useMediaColorTheme } from './useColorTheme';
import './libraries.css';
import './features.css';
import logo from './assets/logo.svg';

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

const modeIcon = mode => mode === 'private' ? Lock : Clapperboard;

// Picks the library the whole media app browses, like a workspace switcher, at the foot of the
// sidebar. Hidden when there is only one library.
function LibrarySwitcher() {
  const { libraries, library, select } = useLibrary();
  if (libraries.length < 2 || !library) return null;
  const workspaces = libraries.map(l => {
    const Icon = modeIcon(l.mode);
    return { id: String(l.id), name: l.name, description: `${LIBRARY_MODES.find(m => m.id === l.mode)?.label || ''}${l.locked ? ' · Locked' : ''}`, logo: <span className="rs-library-logo"><Icon size={16} /></span> };
  });
  return <WorkspaceSwitcher className="rs-library-switcher" workspaces={workspaces} value={String(library.id)} onValueChange={id => select(Number(id))} />;
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

// The app's fixed brand mark at the top of the sidebar (the media app has no workspaces to switch).
function AppBrand() {
  const collapsed = useAppShell()?.collapsed ?? false;
  return <Link to="/" data-slot="app-brand" aria-label="ReelStash home" className="spk-nav-item text-fg before:hidden" data-collapsed={collapsed ? 'true' : undefined}>
    <img src={logo} alt="" className="rs-app-brand-logo" />
    {!collapsed && <span className="spk-nav-item-label font-semibold">ReelStash</span>}
  </Link>;
}

function mediaBackTarget(location) {
  const { pathname, state } = location;
  if (/^\/library\/[^/]+$/.test(pathname)) return state?.from || '/library';
  const detail = pathname.match(/^\/(performers|studios|series|shows|categories|playlists)\/[^/]+$/);
  return detail ? `/${detail[1]}` : null;
}

function MediaShell({ session, onSignOut, children }) {
  const { terms, library, mode } = useLibrary();
  const MEDIA_GROUPS = mediaGroups(terms, mode);
  const location = useLocation();
  const navigate = useNavigate();
  const [mediaTheme, setMediaTheme] = useMediaTheme();
  const [cardStyle] = useCardStyle();
  useMediaColorTheme();
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => {
    try { return localStorage.getItem('reelstash-media-sidebar-collapsed') === 'true'; }
    catch { return false; }
  });
  useEffect(() => {
    try { localStorage.setItem('reelstash-media-sidebar-collapsed', String(sidebarCollapsed)); }
    catch { /* storage unavailable */ }
  }, [sidebarCollapsed]);
  const [headerQuery, setHeaderQuery] = useState('');
  const { recentItems, unseen, markSeen } = useRecentMedia();
  // Spatika menus and dialogs render in a body portal, outside the scoped media provider: the body carries
  // the theme plus rs-media-portal, which discover.css uses to give portaled UI the media app's look.
  useEffect(() => {
    document.body.classList.add(mediaTheme, 'rs-media-portal');
    return () => document.body.classList.remove(mediaTheme, 'rs-media-portal');
  }, [mediaTheme]);
  const headerInputRef = useRef(null);
  const headerTimer = useRef();
  // Sync from the URL only when the header box isn't being typed in, so live navigation can't clobber keystrokes.
  useEffect(() => { if (location.pathname === '/search' && document.activeElement !== headerInputRef.current) setHeaderQuery(new URLSearchParams(location.search).get('q') || ''); }, [location.pathname, location.search]);
  const liveSearch = value => {
    setHeaderQuery(value);
    clearTimeout(headerTimer.current);
    headerTimer.current = setTimeout(() => {
      const q = value.trim();
      if (!q && location.pathname !== '/search') return;
      navigate(`/search${q ? `?q=${encodeURIComponent(q)}` : ''}`, { replace: location.pathname === '/search' });
    }, 250);
  };
  const openLink = path => { if (isAdminAppPath(path)) window.location.assign(adminHref(path)); else navigate(path); };
  const current = [...MEDIA_GROUPS.flatMap(group => group.links), ['Search', '/search'], ['Settings', '/settings']].find(([, path]) => path === location.pathname || (path !== '/' && location.pathname.startsWith(`${path}/`)))?.[0] || 'Library';
  // Detail pages get a back button beside the page name in the header, in place of an in-page back link.
  const back = mediaBackTarget(location);
  const title = <span className="rs-topbar-title">{back && <Link className="rs-topbar-back" to={back} state={{ restoreScroll: true }} aria-label={`Back to ${backLabel(back)}`} title={`Back to ${backLabel(back)}`}><ArrowLeft size={18} /></Link>}<span>{current}</span></span>;
  const navLink = ([label, path, Icon]) => <NavItem key={path} label={label} icon={<Icon size={18} />} active={location.pathname === path || (path !== '/' && location.pathname.startsWith(`${path}/`))} asChild><Link to={path} /></NavItem>;
  const sidebar = <Sidebar header={<AppBrand />} footer={<LibrarySwitcher />}>
    {MEDIA_GROUPS.map(group => <NavSection title={group.label[0] + group.label.slice(1).toLowerCase()} key={group.label}>{group.links.map(navLink)}</NavSection>)}
  </Sidebar>;
  return <SpatikaThemeProvider theme={mediaTheme} syncDocument={false} className={`rs-media-theme rs-app-media${cardStyle === 'thumbnail' ? ' rs-cards-thumbnail' : ''}`}>
    <AppShell sidebar={sidebar} topbar={<TopBar title={title} actions={<HeaderActions
      theme={mediaTheme} onThemeChange={setMediaTheme} themeToggle
      recentItems={recentItems} unseen={unseen} onMarkSeen={markSeen}
      session={session} onSignOut={onSignOut} openLink={openLink}
      accountItems={<><button type="button" role="menuitem" onClick={() => navigate('/settings')}><Settings size={16} /> Settings</button>{session.isAdmin && <button type="button" role="menuitem" onClick={() => window.location.assign(adminHref('/admin'))}><ShieldCheck size={16} /> Admin console</button>}</>}
      leading={<><SurpriseButton /><Button className="rs-mobile-search-button" variant="ghost" size="icon-sm" aria-label="Search" onClick={() => navigate('/search')}><Search size={18} /></Button></>}
    />}>
      <form className="rs-header-search" role="search" onSubmit={event => { event.preventDefault(); clearTimeout(headerTimer.current); navigate(`/search${headerQuery.trim() ? `?q=${encodeURIComponent(headerQuery.trim())}` : ''}`); }}><Search size={17} /><input ref={headerInputRef} aria-label="Search the library" placeholder={terms.searchPlaceholder} value={headerQuery} onChange={event => liveSearch(event.target.value)} /><kbd>↵</kbd></form>
    </TopBar>} density="compact" layout="inset" collapsed={sidebarCollapsed} onCollapsedChange={setSidebarCollapsed}><div className="rs-media-surface" key={library?.id || 'all'}>{children}</div></AppShell>
  </SpatikaThemeProvider>;
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
  if (!libraries.length && !session.isAdmin) return <SpatikaThemeProvider theme={MEDIA_DARK} syncDocument={false} className="rs-media-theme"><NoLibraryAccess onSignOut={onSignOut} /></SpatikaThemeProvider>;
  return <MediaShell session={session} onSignOut={onSignOut}><MediaRoutes session={session} /></MediaShell>;
}

export default function App() {
  return <SpatikaThemeProvider defaultTheme="mukta" storageKey="reelstash-theme"><Toaster position="top-right"><BrowserRouter><ScrollReset /><HeaderScrollState /><AppLockProvider>
    <AuthGate appName="ReelStash" logo={logo}>{(session, signOut) =>
      <LibraryProvider isAdmin={!!session.isAdmin}><NotificationsProvider enabled={!!session.isAdmin}><LibraryGate session={session} onSignOut={signOut} /></NotificationsProvider></LibraryProvider>}
    </AuthGate>
  </AppLockProvider></BrowserRouter></Toaster></SpatikaThemeProvider>;
}
