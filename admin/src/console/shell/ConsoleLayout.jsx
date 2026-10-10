import React, { useEffect, useRef, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { AppShell, MobileTabBar, useAppShell } from '@spatika/react';
import { Film, ListChecks, Menu, Search, House } from 'lucide-react';
import { navEntryFor, SHORTCUTS } from '../lib/navigation';
import { ConsoleSidebar } from './ConsoleSidebar';
import { ConsoleTopBar } from './ConsoleTopBar';
import { CommandPalette } from './CommandPalette';
import { CrumbProvider, useCrumbValue } from './Breadcrumbs';
import { AdminErrorBoundary } from './AdminErrorBoundary';

const COLLAPSED_KEY = 'reelstash-admin-sidebar-collapsed';

function PhoneNav({ onSearch, searchOpen }) {
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const shell = useAppShell();
  const items = [
    { id: 'home', label: 'Home', icon: House, active: pathname === '/admin', onClick: () => navigate('/admin') },
    { id: 'videos', label: 'Videos', icon: Film, active: pathname.startsWith('/admin/videos'), onClick: () => navigate('/admin/videos') },
    { id: 'jobs', label: 'Jobs', icon: ListChecks, active: pathname.startsWith('/admin/jobs'), onClick: () => navigate('/admin/jobs') },
    { id: 'search', label: 'Search', icon: Search, active: searchOpen, onClick: onSearch },
    { id: 'menu', label: 'Menu', icon: Menu, active: shell?.mobileOpen, onClick: () => shell?.setMobileOpen(true) },
  ];
  return <MobileTabBar items={items} />;
}

const isTypingTarget = target => !!target && (['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName) || target.isContentEditable);

function DocumentTitle() {
  const { pathname } = useLocation();
  const crumb = useCrumbValue();
  const label = navEntryFor(pathname)?.label ?? 'Dashboard';
  useEffect(() => { document.title = `${crumb && pathname !== navEntryFor(pathname)?.path ? `${crumb} · ` : ''}${label} · ReelStash Admin`; }, [label, crumb, pathname]);
  return null;
}

export function ConsoleLayout({ session, onSignOut, restricted = false, children }) {
  const { pathname } = useLocation();
  const navigate = useNavigate();
  const [commandOpen, setCommandOpen] = useState(false);
  const [collapsed, setCollapsed] = useState(() => { try { return localStorage.getItem(COLLAPSED_KEY) === '1'; } catch { return false; } });
  const chord = useRef(null);

  // The shell scrolls inside its main panel, so a route change keeps the old offset.
  useEffect(() => {
    document.getElementById('spk-main')?.scrollTo?.({ top: 0, left: 0 });
    document.querySelector('.spk-app-main')?.scrollTo?.({ top: 0, left: 0 });
    window.scrollTo({ top: 0, left: 0 });
  }, [pathname]);

  useEffect(() => { try { localStorage.setItem(COLLAPSED_KEY, collapsed ? '1' : '0'); } catch { /* convenience only */ } }, [collapsed]);

  // "g" then a letter jumps to a section; "/" opens search. ⌘K / Ctrl+K is handled by the top bar's search field.
  useEffect(() => {
    if (restricted) return undefined;
    const onKeyDown = event => {
      if (event.metaKey || event.ctrlKey || event.altKey || isTypingTarget(event.target)) return;
      const key = event.key.toLowerCase();
      if (chord.current !== null) {
        window.clearTimeout(chord.current);
        chord.current = null;
        const match = SHORTCUTS[key];
        if (match) { event.preventDefault(); navigate(match.path); }
        return;
      }
      if (key === 'g') chord.current = window.setTimeout(() => { chord.current = null; }, 1200);
      if (key === '/') { event.preventDefault(); setCommandOpen(true); }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [navigate, restricted]);

  return <div className="console-page">
    <CrumbProvider>
      <DocumentTitle />
      <AppShell className="console-shell" density="compact" layout="inset" mobileNavBreakpoint="md" collapsed={collapsed} onCollapsedChange={setCollapsed}
        sidebar={<ConsoleSidebar session={session} />}
        topbar={<ConsoleTopBar session={session} onSignOut={onSignOut} restricted={restricted} onOpenCommand={() => setCommandOpen(true)} />}
        mobileNav={restricted ? undefined : <PhoneNav onSearch={() => setCommandOpen(true)} searchOpen={commandOpen} />}>
        <div className="console-shell__content">
          <AdminErrorBoundary resetKeys={[pathname]}>{children}</AdminErrorBoundary>
        </div>
      </AppShell>
      {!restricted && <CommandPalette open={commandOpen} onOpenChange={setCommandOpen} />}
    </CrumbProvider>
  </div>;
}
