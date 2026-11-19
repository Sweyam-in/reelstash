import React, { useEffect, useRef, useState } from 'react';
import { Link, NavLink, useLocation, useNavigate } from 'react-router-dom';
import { ArrowLeft, Bell, ChevronsUpDown, Film, LayoutGrid, Lock, LogOut, Moon, PanelLeft, Search, Settings, ShieldCheck, Smartphone, Sun, House, UserRound, Check } from 'lucide-react';
import { Button, DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger, Popover, PopoverContent, PopoverTrigger, cx } from '../ui';
import { useLibrary } from '../libraryContext';
import { LIBRARY_MODES } from '../libraryModes';
import { useAppLock } from '../components/AppLock';
import { useNotifications } from '../components/Notifications';
import SurpriseButton from '../components/SurpriseButton';
import { timeAgo } from '../pages/timeUtil';
import logo from '../assets/logo.svg';

// The media app's frame: a sidebar on wide screens, a top bar with search and account, and a
// bottom tab bar on phones. Pages render inside <main>.

const initial = name => (name || 'U').trim().slice(0, 1).toUpperCase();

function Brand({ collapsed }) {
  return <Link to="/" className="rs-brand" aria-label="ReelStash home">
    <img src={logo} alt="" className="rs-brand-mark" />
    {!collapsed && <span className="rs-brand-name">ReelStash</span>}
  </Link>;
}

// Picks the library the whole app browses.
export function LibrarySwitcher({ collapsed = false, className }) {
  const { libraries, library, select } = useLibrary();
  if (libraries.length < 2 || !library) return null;
  const label = l => `${LIBRARY_MODES.find(m => m.id === l.mode)?.label || ''}${l.locked ? ' · Locked' : ''}`;
  return <DropdownMenu>
    <DropdownMenuTrigger asChild>
      <button type="button" className={cx('rs-switcher', collapsed && 'is-collapsed', className)} aria-label={`Library: ${library.name}`}>
        <span className={cx('rs-switcher-mark', `is-${library.mode}`)} aria-hidden="true">{library.mode === 'private' ? <Lock size={15} /> : <Film size={15} />}</span>
        {!collapsed && <span className="rs-switcher-text"><small>Library</small><strong>{library.name}</strong></span>}
        {!collapsed && <ChevronsUpDown size={16} className="rs-switcher-chevron" aria-hidden="true" />}
      </button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="start" className="rs-switcher-menu">
      <DropdownMenuLabel>Libraries</DropdownMenuLabel>
      {libraries.map(l => <DropdownMenuItem key={l.id} role="menuitemradio" aria-checked={l.id === library.id} onSelect={() => select(l.id)} className={l.id === library.id ? 'is-current' : undefined}>
        <span className={cx('rs-switcher-mark', `is-${l.mode}`)} aria-hidden="true">{l.mode === 'private' ? <Lock size={15} /> : <Film size={15} />}</span>
        <span className="rs-switcher-text"><strong>{l.name}</strong><small>{label(l)}</small></span>
        {l.id === library.id && <Check size={16} className="rs-switcher-check" aria-hidden="true" />}
      </DropdownMenuItem>)}
    </DropdownMenuContent>
  </DropdownMenu>;
}

function Sidebar({ groups, collapsed }) {
  return <nav className={cx('rs-sidebar', collapsed && 'is-collapsed')} aria-label="Main">
    <Brand collapsed={collapsed} />
    <div className="rs-sidebar-groups">
      {groups.map(group => <div className="rs-nav-group" key={group.label}>
        {!collapsed && <div className="rs-nav-heading">{group.label}</div>}
        {group.links.map(([label, path, Icon]) => <NavLink key={path} to={path} end={path === '/'} className={({ isActive }) => cx('rs-nav-item', isActive && 'is-active')} title={collapsed ? label : undefined} aria-label={collapsed ? label : undefined}>
          <Icon size={18} aria-hidden="true" />{!collapsed && <span>{label}</span>}
        </NavLink>)}
      </div>)}
    </div>
    <LibrarySwitcher collapsed={collapsed} />
  </nav>;
}

function NotificationsButton({ recentItems, unseen, onMarkSeen, openLink }) {
  const notes = useNotifications();
  const [open, setOpen] = useState(false);
  const [tab, setTab] = useState('all');
  const location = useLocation();
  useEffect(() => { setOpen(false); }, [location.pathname]);
  const onOpenChange = next => { setOpen(next); if (next) { onMarkSeen?.(); notes.markRead(); } };
  const jobs = notes.items;
  const showJobs = tab !== 'new';
  const showNew = tab !== 'jobs';
  const count = notes.unread > 9 ? '9+' : notes.unread;
  return <Popover open={open} onOpenChange={onOpenChange}>
    <PopoverTrigger className="rs-icon-btn rs-bell" aria-label={notes.unread ? `Notifications, ${notes.unread} new` : 'Notifications'}>
      <Bell size={18} aria-hidden="true" />
      {notes.unread > 0 ? <span className="rs-bell-count">{count}</span> : unseen && <span className="rs-bell-dot" />}
    </PopoverTrigger>
    <PopoverContent align="end" className="rs-notifications" aria-label="Notifications">
      <div className="rs-notifications-head"><strong>Notifications</strong></div>
      <div className="rs-notifications-tabs" role="tablist" aria-label="Notification type">
        {[['all', 'All'], ['new', 'New titles'], ['jobs', 'Library jobs']].map(([id, label]) => <button key={id} type="button" role="tab" aria-selected={tab === id} className={cx('rs-chip-tab', tab === id && 'is-active')} onClick={() => setTab(id)}>{label}</button>)}
      </div>
      <div className="rs-notifications-list">
        {showJobs && jobs.slice(0, 6).map(note => <button type="button" key={note.id} className="rs-note" onClick={() => { setOpen(false); if (note.link) openLink(note.link); }}>
          <span className={cx('rs-note-icon', note.kind?.endsWith('.failed') && 'is-danger')}><Bell size={15} aria-hidden="true" /></span>
          <span className="rs-note-text"><strong>{note.title}</strong>{note.body && <small>{note.body}</small>}</span>
          <time className="rs-note-time">{timeAgo(note.createdAt)}</time>
        </button>)}
        {showNew && recentItems.map(item => <button type="button" key={`m${item.id}`} className="rs-note" onClick={() => { setOpen(false); openLink(`/library/${item.id}`); }}>
          <span className="rs-note-icon is-new"><Film size={15} aria-hidden="true" /></span>
          <span className="rs-note-text"><strong>{item.title}</strong><small>{item.followed ? `New · with ${(item.performers || [])[0] || 'someone you follow'}` : item.studioName ? `New · ${item.studioName}` : 'New in your library'}</small></span>
        </button>)}
        {((showJobs && !jobs.length) || !showJobs) && (!showNew || !recentItems.length) && <p className="rs-notifications-empty">Nothing new right now.</p>}
      </div>
    </PopoverContent>
  </Popover>;
}

function AccountMenu({ session, isAdmin, theme, onThemeChange, onSignOut, adminHref }) {
  const navigate = useNavigate();
  const appLock = useAppLock();
  const name = session?.displayName || session?.email || 'User';
  const light = theme === 'light';
  return <DropdownMenu>
    <DropdownMenuTrigger asChild>
      <button type="button" className="rs-avatar-btn" aria-label={`Account: ${name}`}>{initial(name)}</button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end" className="rs-account-menu">
      <div className="rs-account-head"><span className="rs-avatar" aria-hidden="true">{initial(name)}</span><span><strong>{name}</strong>{isAdmin && <small>Owner</small>}</span></div>
      <DropdownMenuSeparator />
      <DropdownMenuItem onSelect={() => navigate('/settings')}><Settings size={16} aria-hidden="true" /> Settings</DropdownMenuItem>
      <DropdownMenuItem onSelect={() => navigate('/activity')}><LayoutGrid size={16} aria-hidden="true" /> Activity</DropdownMenuItem>
      {isAdmin && <DropdownMenuItem onSelect={() => window.location.assign(adminHref('/admin'))}><ShieldCheck size={16} aria-hidden="true" /> Admin console</DropdownMenuItem>}
      <DropdownMenuItem onSelect={() => onThemeChange(light ? 'dark' : 'light')}>{light ? <Moon size={16} aria-hidden="true" /> : <Sun size={16} aria-hidden="true" />} {light ? 'Dark theme' : 'Light theme'}</DropdownMenuItem>
      {appLock?.hasPin && <DropdownMenuItem onSelect={() => appLock.lockNow()}><Lock size={16} aria-hidden="true" /> Lock now</DropdownMenuItem>}
      <DropdownMenuItem onSelect={() => appLock?.openSettings()}><Lock size={16} aria-hidden="true" /> PIN lock…</DropdownMenuItem>
      <DropdownMenuSeparator />
      <DropdownMenuItem className="is-danger" onSelect={onSignOut}><LogOut size={16} aria-hidden="true" /> Sign out</DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>;
}

// The header search box: typing searches live (after a short pause); ⌘K / Ctrl+K or "/" focus it.
function HeaderSearch({ placeholder }) {
  const navigate = useNavigate();
  const location = useLocation();
  const [query, setQuery] = useState('');
  const input = useRef(null);
  const timer = useRef();
  useEffect(() => {
    if (location.pathname === '/search' && document.activeElement !== input.current) setQuery(new URLSearchParams(location.search).get('q') || '');
    if (location.pathname !== '/search' && document.activeElement !== input.current) setQuery('');
  }, [location.pathname, location.search]);
  useEffect(() => {
    const onKey = event => {
      const typing = /input|textarea|select/i.test(document.activeElement?.tagName || '') || document.activeElement?.isContentEditable;
      if (((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') || (event.key === '/' && !typing)) { event.preventDefault(); input.current?.focus(); input.current?.select(); }
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, []);
  const go = (value, replace) => navigate(`/search${value.trim() ? `?q=${encodeURIComponent(value.trim())}` : ''}`, { replace });
  const onChange = value => {
    setQuery(value);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      if (!value.trim() && location.pathname !== '/search') return;
      go(value, location.pathname === '/search');
    }, 250);
  };
  const mac = typeof navigator !== 'undefined' && /mac/i.test(navigator.platform || navigator.userAgent || '');
  return <form className="rs-topsearch" role="search" onSubmit={event => { event.preventDefault(); clearTimeout(timer.current); go(query, location.pathname === '/search'); }}>
    <Search size={18} aria-hidden="true" />
    <input ref={input} aria-label="Search the library" placeholder={placeholder} value={query} onChange={event => onChange(event.target.value)} />
    <kbd aria-hidden="true">{mac ? '⌘K' : 'Ctrl K'}</kbd>
  </form>;
}

function TabBar({ tabs }) {
  return <nav className="rs-tabbar" aria-label="Main">
    {tabs.map(([label, path, Icon, match]) => <NavLink key={path} to={path} end={path === '/'} className={({ isActive }) => cx('rs-tabbar-item', (isActive || match?.()) && 'is-active')}>
      <Icon size={22} aria-hidden="true" /><span>{label}</span>
    </NavLink>)}
  </nav>;
}

export default function AppFrame({ groups, title, back, backLabel, session, theme, onThemeChange, onSignOut, recent, openLink, adminHref, searchPlaceholder, children }) {
  const { terms, mode } = useLibrary();
  const location = useLocation();
  const [collapsed, setCollapsed] = useState(() => { try { return localStorage.getItem('reelstash-media-sidebar-collapsed') === 'true'; } catch { return false; } });
  useEffect(() => { try { localStorage.setItem('reelstash-media-sidebar-collapsed', String(collapsed)); } catch { /* storage blocked */ } }, [collapsed]);
  const libraryPath = mode === 'entertainment' ? '/movies' : '/library';
  const youPaths = ['/you', '/favorites', '/watch-later', '/playlists', '/performers', '/studios', '/categories', '/series', '/activity', '/settings', '/directors', '/shows'];
  const tabs = [
    ['Home', '/', House],
    [mode === 'entertainment' ? terms.movies : terms.library, libraryPath, LayoutGrid],
    ['Reels', '/reels', Smartphone],
    ['Search', '/search', Search],
    ['You', '/you', UserRound, () => youPaths.some(p => location.pathname === p || location.pathname.startsWith(`${p}/`))],
  ];
  return <div className={cx('rs-frame', collapsed && 'is-collapsed', location.pathname === '/reels' && 'is-immersive')}>
    <a href="#rs-main" className="rs-skip">Skip to content</a>
    <Sidebar groups={groups} collapsed={collapsed} />
    <div className="rs-frame-main">
      <header className="rs-topbar">
        <Button variant="ghost" size="icon-sm" className="rs-sidebar-toggle" aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'} aria-pressed={collapsed} onClick={() => setCollapsed(!collapsed)}><PanelLeft size={18} /></Button>
        <Link to="/" className="rs-topbar-brand" aria-label="ReelStash home"><img src={logo} alt="" /></Link>
        {back
          ? <Link className="rs-back" to={back} state={{ restoreScroll: true }} aria-label={`Back to ${backLabel}`} title={`Back to ${backLabel}`}><ArrowLeft size={18} aria-hidden="true" /><span>{backLabel}</span></Link>
          : <h1 className="rs-topbar-title">{title}</h1>}
        <HeaderSearch placeholder={searchPlaceholder || terms.searchPlaceholder} />
        <div className="rs-topbar-actions">
          <span className="rs-surprise"><SurpriseButton /></span>
          <NotificationsButton recentItems={recent.recentItems} unseen={recent.unseen} onMarkSeen={recent.markSeen} openLink={openLink} />
          <AccountMenu session={session} isAdmin={!!session?.isAdmin} theme={theme} onThemeChange={onThemeChange} onSignOut={onSignOut} adminHref={adminHref} />
        </div>
      </header>
      <main id="rs-main" className="rs-main" tabIndex={-1}>{children}</main>
    </div>
    <TabBar tabs={tabs} />
  </div>;
}
