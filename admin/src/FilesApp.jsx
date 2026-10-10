import React, { useCallback, useEffect, useState } from 'react';
import { BrowserRouter, Link, Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import { ArrowLeft, ArrowRight, ArrowUp, Clapperboard, Download, FileText, Folder, HardDrive, House, Image, Plus, RefreshCw, Share2 } from 'lucide-react';
import { AppShell, NavItem, NavSection, Sidebar, SpatikaThemeProvider, Toaster, TopBar } from '@spatika/react';
import { getFileLocations, listFiles } from './api';
import FileBrowserPage, { FilesChromeContext, FilesPathBar, FilesSearch, useFilesChromeState } from './pages/FileBrowserPage';
import { AppLockProvider } from './components/AppLock';
import { NotificationsProvider } from './components/Notifications';
import { AuthGate, HeaderActions, ScrollReset, useRecentMedia } from './shell';
import { AppWorkspaceSwitcher, OPEN_LIBRARY, useOpenLink, useStoredTheme } from './workspaces';
import FileTabs from './components/FileTabs';
import { FileTransfersButton, useFileTransfers } from './components/FileTransfers';
import { activateTab, activeTab, canGoBack, canGoForward, closeTab, goBack, goForward, isFilesUrl, loadTabs, openTab, saveTabs, syncActive } from './filesTabs';
import filesLogo from './assets/logo-files.svg';

// ReelStash Files: the file browser, a React app of its own (files.html) deployed beside
// ReelStash Admin — see workspaces.jsx. Open to every signed-in user; editing is admin-only.

function folderNavIcon(name) {
  const label = name.toLowerCase();
  const Icon = label.includes('document') ? FileText : label.includes('download') ? Download : label.includes('galler') || label.includes('photo') || label.includes('image') ? Image : label.includes('media') ? Clapperboard : Folder;
  return <Icon size={18} />;
}

function FilesIndexRedirect({ locations, locationsLoaded }) {
  const navigate = useNavigate();
  useEffect(() => { if (locationsLoaded && locations.length) navigate(`/files/${locations[0].id}`, { replace: true }); }, [locationsLoaded, locations, navigate]);
  if (!locationsLoaded) return <div className="rs-file-empty"><RefreshCw size={28} className="rs-spin" /><h2>Loading storage locations</h2></div>;
  if (!locations.length) return <div className="rs-file-empty"><HardDrive size={32} /><h2>No storage locations yet</h2><p>Add a storage location to browse its files here.</p></div>;
  return null;
}

function FilesShell({ session, onSignOut }) {
  const navigate = useNavigate();
  const location = useLocation();
  const openLink = useOpenLink('/files');
  const [filesTheme, setFilesTheme] = useStoredTheme('reelstash-files-theme', 'mukta');
  const transfers = useFileTransfers(session.isAdmin);
  const filesChrome = useFilesChromeState(transfers);
  const { recentItems, unseen, markSeen } = useRecentMedia();
  const match = location.pathname.match(/^\/files\/([^/]+)(?:\/(.*))?$/);
  const activeLocationId = match?.[1] || '';
  const pathParts = (match?.[2] || '').split('/').filter(Boolean).map(decodeURIComponent);
  const activeTopFolder = pathParts[0] || '';
  const [locations, setLocations] = useState([]);
  const [locationsLoaded, setLocationsLoaded] = useState(false);
  const [quickFolders, setQuickFolders] = useState([]);
  const [tabState, setTabState] = useState(() => loadTabs(location.pathname));

  // The active tab follows the router; every other tab keeps the folder it was left on.
  useEffect(() => { if (isFilesUrl(location.pathname)) setTabState(state => syncActive(state, location.pathname)); }, [location.pathname]);
  useEffect(() => { saveTabs(tabState); }, [tabState]);

  const goToTab = useCallback(next => { setTabState(next); navigate(activeTab(next).url); }, [navigate]);
  const newTab = useCallback((url = location.pathname) => { if (isFilesUrl(url)) goToTab(openTab(tabState, url)); }, [goToTab, tabState, location.pathname]);
  const switchTab = useCallback(id => { if (id !== tabState.activeId) goToTab(activateTab(tabState, id)); }, [goToTab, tabState]);
  const stepHistory = useCallback(step => { const next = step(tabState); if (next !== tabState) goToTab(next); }, [goToTab, tabState]);
  const goUpFolder = useCallback(() => {
    if (pathParts.length) navigate(`/files/${activeLocationId}${pathParts.slice(0, -1).map(part => `/${encodeURIComponent(part)}`).join('')}`);
  }, [navigate, activeLocationId, pathParts]);
  const dropTab = useCallback(id => { const next = closeTab(tabState, id); if (next !== tabState) goToTab(next); }, [goToTab, tabState]);

  // Alt+T new tab, Alt+W close tab, Alt+1…9 jump to a tab, Alt+←/→ back and forward (plain Ctrl/⌘ combos belong to the browser).
  useEffect(() => {
    const onKey = event => {
      const typing = event.target.closest?.('input:not([type="checkbox"]), textarea, select, [contenteditable="true"], [role="dialog"]');
      if (typing) return;
      if (event.metaKey && !event.altKey && !event.ctrlKey && (event.key === '[' || event.key === ']')) { event.preventDefault(); stepHistory(event.key === '[' ? goBack : goForward); return; }
      if (!event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
      if (event.key === 'ArrowLeft') { event.preventDefault(); stepHistory(goBack); return; }
      if (event.key === 'ArrowRight') { event.preventDefault(); stepHistory(goForward); return; }
      const digit = /^Digit([1-9])$/.exec(event.code);
      if (event.code === 'KeyT') { event.preventDefault(); newTab(); }
      else if (event.code === 'KeyW') { event.preventDefault(); dropTab(tabState.activeId); }
      else if (digit && tabState.tabs[Number(digit[1]) - 1]) { event.preventDefault(); switchTab(tabState.tabs[Number(digit[1]) - 1].id); }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [newTab, dropTab, switchTab, stepHistory, tabState]);

  useEffect(() => {
    let active = true;
    getFileLocations().then(result => { if (active) setLocations(result.items || []); }).catch(() => {})
      .finally(() => { if (active) setLocationsLoaded(true); });
    return () => { active = false; };
  }, []);

  useEffect(() => {
    if (!activeLocationId) { setQuickFolders([]); return; }
    let active = true;
    listFiles(activeLocationId, '').then(result => { if (active) setQuickFolders((result.items || []).filter(item => item.isDir)); })
      .catch(() => { if (active) setQuickFolders([]); });
    return () => { active = false; };
  }, [activeLocationId]);

  const activeLocation = locations.find(item => String(item.id) === String(activeLocationId));

  useEffect(() => { filesChrome.setQuery(''); }, [location.pathname]); // eslint-disable-line react-hooks/exhaustive-deps

  const sidebar = <Sidebar
    header={<AppWorkspaceSwitcher current="files" canAdmin={session.isAdmin} />}
    footer={<NavItem label="Shared" icon={<Share2 size={18} />} disabled title="Sharing isn't available yet" />}
  >
    <NavSection>
      <NavItem label="Root" icon={<House size={18} />} active={!activeLocationId} asChild><Link to="/files" /></NavItem>
    </NavSection>
    {!!quickFolders.length && <NavSection title="Data">
      {quickFolders.map(folder => <NavItem key={folder.name} label={folder.name} icon={folderNavIcon(folder.name)} active={activeTopFolder === folder.name} asChild><Link to={`/files/${activeLocationId}/${encodeURIComponent(folder.name)}`} /></NavItem>)}
    </NavSection>}
    <NavSection title="Location" action={session.isAdmin ? <a className="rs-nav-plus" href="/admin/storage" aria-label="Manage storage locations"><Plus size={14} /></a> : null}>
      {locations.map(item => <NavItem key={item.id} label={item.name} icon={<HardDrive size={18} />} active={String(item.id) === String(activeLocationId)} asChild><Link to={`/files/${item.id}`} /></NavItem>)}
    </NavSection>
  </Sidebar>;

  return <FilesChromeContext.Provider value={filesChrome}>
    <SpatikaThemeProvider theme={filesTheme} syncDocument={false} className="rs-media-theme rs-files-app">
      <AppShell sidebar={sidebar} topbar={<TopBar className="rs-files-topbar" title={<div className="rs-fm-nav">
        <button type="button" className="rs-fm-navbtn" aria-label="Back" title="Back (Alt+←)" disabled={!canGoBack(tabState)} onClick={() => stepHistory(goBack)}><ArrowLeft size={17} /></button>
        <button type="button" className="rs-fm-navbtn" aria-label="Forward" title="Forward (Alt+→)" disabled={!canGoForward(tabState)} onClick={() => stepHistory(goForward)}><ArrowRight size={17} /></button>
        <button type="button" className="rs-fm-navbtn" aria-label="Up one folder" title="Up (Alt+↑)" disabled={!pathParts.length} onClick={goUpFolder}><ArrowUp size={17} /></button>
        <FilesPathBar locationId={activeLocationId} locationName={activeLocation?.name} parts={pathParts} onNavigate={navigate} />
      </div>} actions={<>
        <FilesSearch />
        <FileTransfersButton transfers={transfers} />
        <HeaderActions
          theme={filesTheme} onThemeChange={setFilesTheme}
          recentItems={recentItems} unseen={unseen} onMarkSeen={markSeen}
          session={session} onSignOut={onSignOut} openLink={openLink} hideNotifications
          accountItems={OPEN_LIBRARY}
        />
      </>} />} density="compact" layout="inset"><div className="rs-media-surface">
        <FileTabs tabs={tabState.tabs} activeId={tabState.activeId} locations={locations} onActivate={switchTab} onClose={dropTab} onNew={() => newTab()} />
        <Routes>
        <Route path="/files" element={<FilesIndexRedirect locations={locations} locationsLoaded={locationsLoaded} />} />
        <Route path="/files/:locationId/*" element={<FileBrowserPage canAdmin={session.isAdmin} locations={locations} onOpenInTab={newTab} />} />
        <Route path="*" element={<Navigate to="/files" replace />} />
      </Routes></div></AppShell>
    </SpatikaThemeProvider>
  </FilesChromeContext.Provider>;
}

export default function FilesApp() {
  return <SpatikaThemeProvider defaultTheme="mukta" storageKey="reelstash-theme"><Toaster position="top-right"><BrowserRouter><ScrollReset /><AppLockProvider>
    <AuthGate appName="ReelStash Files" logo={filesLogo}>{(session, signOut) =>
      <NotificationsProvider enabled={!!session.isAdmin}><FilesShell session={session} onSignOut={signOut} /></NotificationsProvider>}
    </AuthGate>
  </AppLockProvider></BrowserRouter></Toaster></SpatikaThemeProvider>;
}
