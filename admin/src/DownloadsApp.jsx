import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { BrowserRouter, Link, Navigate, Route, Routes, useLocation, useSearchParams } from 'react-router-dom';
import {
  AlertTriangle, ArrowDown, ArrowUp, CheckCircle2, CircleAlert, CircleStop, Download, Gauge, History, Inbox, ListFilter,
  Plus, Settings, Sprout, Tag, Turtle, Workflow, Zap,
} from 'lucide-react';
import { AppShell, Button, NavItem, NavSection, Sidebar, SpatikaThemeProvider, Toaster, TopBar, useToast } from '@spatika/react';
import { getDownloads, toggleAltSpeed } from './api';
import { AppLockProvider } from './components/AppLock';
import { NotificationsProvider } from './components/Notifications';
import { AuthGate, HeaderActions, ScrollReset, useRecentMedia } from './shell';
import { AppWorkspaceSwitcher, OPEN_LIBRARY, useOpenLink, useStoredTheme } from './workspaces';
import { FILTERS, filterCounts, formatSpeed, torrentTags } from './downloadsModel';
import { DownloadsContext } from './pages/downloads/context';
import TorrentsPage from './pages/downloads/TorrentsPage';
import AddTorrentDialog from './pages/downloads/AddTorrentDialog';
import CategoryDialog from './pages/downloads/CategoryDialog';
import MoveRulesPage from './pages/downloads/MoveRulesPage';
import HistoryPage from './pages/downloads/HistoryPage';
import SettingsPage from './pages/downloads/SettingsPage';
import downloadsLogo from './assets/logo-downloads.svg';

// ReelStash Downloads: qBittorrent, managed from ReelStash (downloads.html), deployed beside
// Admin and Files — see workspaces.jsx. Admins only; the server talks to qBittorrent.

const FILTER_ICON = { all: Inbox, downloading: Download, seeding: Sprout, completed: CheckCircle2, stopped: CircleStop, active: Zap, errored: CircleAlert };

// The live torrent list: refreshed every 2 seconds while the tab is visible, every 15 in the
// background (the server itself only polls qBittorrent quickly while someone is looking).
function useDownloadsFeed() {
  const [snap, setSnap] = useState(null);
  const [error, setError] = useState('');
  const timer = useRef(null);
  const reload = useCallback(() => getDownloads().then(res => { setSnap(res); setError(''); return res; }).catch(err => { setError(err.message); }), []);
  useEffect(() => {
    let live = true;
    const tick = async () => {
      if (!live) return;
      await reload();
      if (live) timer.current = setTimeout(tick, document.visibilityState === 'visible' ? 2000 : 15000);
    };
    tick();
    const wake = () => { if (document.visibilityState === 'visible') { clearTimeout(timer.current); tick(); } };
    document.addEventListener('visibilitychange', wake);
    return () => { live = false; clearTimeout(timer.current); document.removeEventListener('visibilitychange', wake); };
  }, [reload]);
  return { snap, error, reload };
}

function SpeedMeter({ server, onToggleAlt, busy }) {
  const alt = !!server?.use_alt_speed_limits;
  const limit = value => (value > 0 ? ` (limit ${formatSpeed(value)})` : '');
  return <div className="rs-dl-speed" aria-label="Transfer speed">
    <span title={`Download${limit(server?.dl_rate_limit)}`}><ArrowDown size={14} />{formatSpeed(server?.dl_info_speed) === '—' ? '0 B/s' : formatSpeed(server?.dl_info_speed)}</span>
    <span title={`Upload${limit(server?.up_rate_limit)}`}><ArrowUp size={14} />{formatSpeed(server?.up_info_speed) === '—' ? '0 B/s' : formatSpeed(server?.up_info_speed)}</span>
    <button type="button" className={`rs-dl-alt${alt ? ' is-on' : ''}`} aria-pressed={alt} disabled={busy} onClick={onToggleAlt}
      title={alt ? 'Alternative speed limits are on' : 'Use alternative speed limits'} aria-label="Alternative speed limits"><Turtle size={16} /></button>
  </div>;
}

const PAGE_TITLE = { '/downloads/rules': 'Move rules', '/downloads/history': 'History', '/downloads/settings': 'Settings' };

function DownloadsShell({ session, onSignOut }) {
  const location = useLocation();
  const [params] = useSearchParams();
  const openLink = useOpenLink('/downloads');
  const { toast } = useToast();
  const [theme, setTheme] = useStoredTheme('reelstash-downloads-theme', 'mukta');
  const { recentItems, unseen, markSeen } = useRecentMedia();
  const feed = useDownloadsFeed();
  const [adding, setAdding] = useState(false);
  const [categoryDialog, setCategoryDialog] = useState(null); // null closed, { category } to edit, {} to add
  const [altBusy, setAltBusy] = useState(false);

  const torrents = useMemo(() => feed.snap?.torrents || [], [feed.snap]);
  const counts = useMemo(() => filterCounts(torrents), [torrents]);
  const categoryCounts = useMemo(() => {
    const out = { '-': 0 };
    for (const t of torrents) out[t.category || '-'] = (out[t.category || '-'] || 0) + 1;
    return out;
  }, [torrents]);
  const tagCounts = useMemo(() => {
    const out = {};
    for (const t of torrents) for (const tag of torrentTags(t)) out[tag] = (out[tag] || 0) + 1;
    return out;
  }, [torrents]);

  const onList = location.pathname === '/downloads' || location.pathname === '/downloads/';
  const filter = onList ? params.get('filter') || (params.get('category') == null && params.get('tag') == null ? 'all' : '') : '';
  const category = onList ? params.get('category') : null;
  const tag = onList ? params.get('tag') : null;

  async function flipAlt() {
    setAltBusy(true);
    try { await toggleAltSpeed(); await feed.reload(); }
    catch (err) { toast({ title: 'Could not switch speed limits', description: err.message, tone: 'danger' }); }
    finally { setAltBusy(false); }
  }

  const connected = !!feed.snap?.connected;
  const sidebar = <Sidebar
    header={<AppWorkspaceSwitcher current="downloads" canAdmin={session.isAdmin} />}
    footer={<NavItem label="Settings" icon={<Settings size={18} />} active={location.pathname === '/downloads/settings'} asChild><Link to="/downloads/settings" /></NavItem>}
  >
    <NavSection title="Torrents">
      {FILTERS.map(f => {
        const Icon = FILTER_ICON[f.id] || ListFilter;
        return <NavItem key={f.id} label={f.label} icon={<Icon size={18} />} active={filter === f.id} meta={counts[f.id] ? counts[f.id] : null} asChild>
          <Link to={f.id === 'all' ? '/downloads' : `/downloads?filter=${f.id}`} />
        </NavItem>;
      })}
    </NavSection>
    <NavSection title="Categories" action={<button type="button" className="rs-nav-plus" aria-label="Add category" disabled={!connected} onClick={() => setCategoryDialog({})}><Plus size={14} /></button>}>
      <NavItem label="Uncategorized" icon={<Tag size={18} />} active={category === '-'} meta={categoryCounts['-'] || null} asChild><Link to="/downloads?category=-" /></NavItem>
      {(feed.snap?.categories || []).map(c => <NavItem key={c.name} label={c.name} icon={<Tag size={18} />} active={category === c.name} meta={categoryCounts[c.name] || null} asChild>
        <Link to={`/downloads?category=${encodeURIComponent(c.name)}`} />
      </NavItem>)}
    </NavSection>
    {!!feed.snap?.tags?.length && <NavSection title="Tags">
      {feed.snap.tags.map(t => <NavItem key={t} label={t} icon={<Tag size={18} />} active={tag === t} meta={tagCounts[t] || null} asChild>
        <Link to={`/downloads?tag=${encodeURIComponent(t)}`} />
      </NavItem>)}
    </NavSection>}
    <NavSection title="Automation">
      <NavItem label="Move rules" icon={<Workflow size={18} />} active={location.pathname === '/downloads/rules'} meta={feed.snap?.autoMove ? 'On' : null} asChild><Link to="/downloads/rules" /></NavItem>
      <NavItem label="History" icon={<History size={18} />} active={location.pathname === '/downloads/history'} asChild><Link to="/downloads/history" /></NavItem>
    </NavSection>
  </Sidebar>;

  const title = PAGE_TITLE[location.pathname] || (category === '-' ? 'Uncategorized' : category || tag || FILTERS.find(f => f.id === filter)?.label || 'Torrents');

  return <DownloadsContext.Provider value={{ ...feed, torrents, openCategory: category => setCategoryDialog({ category }) }}>
    <SpatikaThemeProvider theme={theme} syncDocument={false} className="rs-media-theme rs-downloads-app">
      <AppShell sidebar={sidebar} topbar={<TopBar className="rs-dl-topbar" title={<div className="rs-dl-title">
        <span>{title}</span>
        {feed.snap && !connected && <span className="rs-dl-offline" title={feed.snap.error || ''}><AlertTriangle size={14} /> {feed.snap.configured ? 'qBittorrent unreachable' : 'Not set up'}</span>}
      </div>} actions={<>
        {connected && <SpeedMeter server={feed.snap.server} onToggleAlt={flipAlt} busy={altBusy} />}
        <Button size="sm" disabled={!connected} onClick={() => setAdding(true)}><Plus size={15} /> Add</Button>
        <HeaderActions
          theme={theme} onThemeChange={setTheme}
          recentItems={recentItems} unseen={unseen} onMarkSeen={markSeen}
          session={session} onSignOut={onSignOut} openLink={openLink}
          accountItems={OPEN_LIBRARY}
        />
      </>} />} density="compact" layout="inset"><div className="rs-media-surface rs-dl-surface">
        <Routes>
          <Route path="/downloads" element={<TorrentsPage filter={filter || 'all'} category={category ?? ''} tag={tag ?? ''} onAdd={() => setAdding(true)} />} />
          <Route path="/downloads/rules" element={<MoveRulesPage />} />
          <Route path="/downloads/history" element={<HistoryPage />} />
          <Route path="/downloads/settings" element={<SettingsPage />} />
          <Route path="*" element={<Navigate to="/downloads" replace />} />
        </Routes>
      </div></AppShell>
      <AddTorrentDialog open={adding} onClose={() => setAdding(false)} categories={feed.snap?.categories || []} onAdded={feed.reload} />
      <CategoryDialog open={!!categoryDialog} category={categoryDialog?.category} onClose={() => setCategoryDialog(null)} onSaved={feed.reload} />
    </SpatikaThemeProvider>
  </DownloadsContext.Provider>;
}

function AdminsOnly({ onSignOut }) {
  return <SpatikaThemeProvider theme="mukta" syncDocument={false} className="rs-media-theme">
    <div className="rs-file-empty rs-dl-gate">
      <Gauge size={32} />
      <h2>Downloads is for admins</h2>
      <p>Ask an admin if you need something downloaded.</p>
      <Button variant="secondary" onClick={onSignOut}>Sign out</Button>
    </div>
  </SpatikaThemeProvider>;
}

export default function DownloadsApp() {
  return <SpatikaThemeProvider defaultTheme="mukta" storageKey="reelstash-theme"><Toaster position="top-right"><BrowserRouter><ScrollReset /><AppLockProvider>
    <AuthGate appName="ReelStash Downloads" logo={downloadsLogo}>{(session, signOut) => session.isAdmin
      ? <NotificationsProvider enabled><DownloadsShell session={session} onSignOut={signOut} /></NotificationsProvider>
      : <AdminsOnly onSignOut={signOut} />}
    </AuthGate>
  </AppLockProvider></BrowserRouter></Toaster></SpatikaThemeProvider>;
}
