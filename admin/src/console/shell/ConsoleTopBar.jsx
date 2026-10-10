import React from 'react';
import { Link, useLocation } from 'react-router-dom';
import {
  Button, DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuRadioGroup, DropdownMenuRadioItem,
  DropdownMenuSeparator, DropdownMenuTrigger, SearchTrigger, THEME_IDS, TopBar, useSpatikaTheme,
} from '@spatika/react';
import { Bell, ChevronDown, Film, Lock, LogOut, Palette, Tv } from 'lucide-react';
import { useAppLock } from '../../components/AppLock';
import { useNotifications } from '../../components/Notifications';
import { mediaHref } from '../../appLinks';
import { timeAgo } from '../lib/format';
import { navEntryFor } from '../lib/navigation';
import { useSignals } from '../lib/signals';
import { useRecentMedia } from '../../shell';
import { useOpenLink } from '../../workspaces';
import { Status } from '../components/States';
import { UserAvatar } from '../components/Detail';
import { useCrumbValue } from './Breadcrumbs';

// Explicit label wins (e.g. "staging"); local dev builds are always tagged.
const ENV_LABEL = import.meta.env.VITE_ADMIN_ENV_LABEL || (import.meta.env.DEV ? 'development' : null);

const THEME_NAMES = { neelam: 'Midnight', sandhya: 'Dusk', mukta: 'Light', usha: 'Warm light' };

function ThemePicker() {
  const { theme, setTheme } = useSpatikaTheme();
  return <DropdownMenu>
    <DropdownMenuTrigger asChild><Button type="button" variant="ghost" size="icon" aria-label="Choose theme"><Palette size={16} /></Button></DropdownMenuTrigger>
    <DropdownMenuContent align="end">
      <DropdownMenuLabel>Appearance</DropdownMenuLabel>
      <DropdownMenuRadioGroup value={THEME_IDS.includes(theme) ? theme : 'usha'} onValueChange={setTheme}>
        {THEME_IDS.map(id => <DropdownMenuRadioItem key={id} value={id}>{THEME_NAMES[id] || id}</DropdownMenuRadioItem>)}
      </DropdownMenuRadioGroup>
    </DropdownMenuContent>
  </DropdownMenu>;
}

function HealthPill() {
  const { summary, healthError, healthLoading, health } = useSignals();
  const label = healthLoading && !health ? 'Checking…' : healthError && !health ? 'Status unavailable' : summary.label;
  const tone = healthError && !health ? 'warn' : summary.tone;
  return <Link to="/admin/system" className="console-health-pill" data-tone={tone} aria-label={`System status: ${label}`}>
    <Status tone={tone}><span className="console-health-pill__label">{label}</span></Status>
  </Link>;
}

function Notices({ openLink }) {
  const notes = useNotifications();
  const { recentItems, unseen, markSeen } = useRecentMedia();
  return <DropdownMenu onOpenChange={open => { if (open) { markSeen(); notes.markRead(); } }}>
    <DropdownMenuTrigger asChild>
      <Button type="button" variant="ghost" size="icon" aria-label={notes.unread > 0 ? `Notifications, ${notes.unread} unread` : 'Notifications'} className="console-bell">
        <Bell size={16} />{(notes.unread > 0 || unseen) && <span className="console-bell__dot" aria-hidden="true" />}
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end" className="console-notices">
      <DropdownMenuLabel>Notifications</DropdownMenuLabel>
      {notes.items.length === 0 && <DropdownMenuItem disabled>Nothing new</DropdownMenuItem>}
      {notes.items.slice(0, 6).map(note => <DropdownMenuItem key={note.id} onSelect={() => note.link && openLink(note.link)}>
        <span className="console-notice"><strong>{note.title}</strong><small>{[note.body, timeAgo(note.createdAt)].filter(Boolean).join(' · ')}</small></span>
      </DropdownMenuItem>)}
      {recentItems.length > 0 && <>
        <DropdownMenuSeparator />
        <DropdownMenuLabel>Recently added</DropdownMenuLabel>
        {recentItems.map(item => <DropdownMenuItem key={item.id} onSelect={() => openLink(`/library/${item.id}`)}>
          <Film size={14} aria-hidden="true" /><span className="console-notice"><strong>{item.title}</strong><small>{item.studioName || 'Library'}</small></span>
        </DropdownMenuItem>)}
      </>}
    </DropdownMenuContent>
  </DropdownMenu>;
}

export function ConsoleTopBar({ session, onSignOut, onOpenCommand, restricted = false }) {
  const { pathname } = useLocation();
  const appLock = useAppLock();
  const openLink = useOpenLink('/admin');
  const entry = navEntryFor(pathname);
  const crumb = useCrumbValue();
  const name = (session?.displayName || '').trim() || (session?.email || session?.username || 'Admin').split('@')[0];
  const parent = entry?.parent;
  const isDetail = !!entry && pathname !== entry.path && pathname !== '/admin';
  // "Metadata / People", or "Storage / Disks / Archive" for a detail page.
  const trail = [];
  if (entry && parent && parent !== entry && parent.tabs) trail.push({ label: parent.label, to: parent.path });
  const pageLabel = entry?.label ?? 'Dashboard';

  return <TopBar className="console-topbar"
    title={<nav aria-label="Breadcrumb" className="console-topbar__crumbs">
      <Link to="/admin" className="console-topbar__title-link"><span className="console-topbar__product">ReelStash Admin</span></Link>
      <span className="console-topbar__sep console-topbar__sep--lead" aria-hidden="true">/</span>
      {trail.map(t => <React.Fragment key={t.to}><Link to={t.to} className="console-topbar__crumb">{t.label}</Link><span className="console-topbar__sep" aria-hidden="true">/</span></React.Fragment>)}
      {isDetail ? <>
        <Link to={entry.path} className="console-topbar__crumb">{pageLabel}</Link>
        <span className="console-topbar__sep" aria-hidden="true">/</span>
        <span className="console-topbar__page" aria-current="page">{crumb ?? '…'}</span>
      </> : <span className="console-topbar__page" aria-current="page">{pageLabel}</span>}
      {ENV_LABEL && <span className="console-env-tag" data-env={ENV_LABEL.toLowerCase()}>{ENV_LABEL.charAt(0).toUpperCase() + ENV_LABEL.slice(1)}</span>}
    </nav>}
    actions={<div className="console-topbar__actions">
      {!restricted && <HealthPill />}
      <ThemePicker />
      {!restricted && <Notices openLink={openLink} />}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button type="button" variant="ghost" className="console-topbar__profile" aria-label="Account menu">
            <UserAvatar name={name} email={session?.email} size={28} />
            <span className="console-topbar__profile-text"><span className="console-topbar__profile-name">{name}</span><span className="console-topbar__profile-role">Administrator</span></span>
            <ChevronDown size={14} aria-hidden="true" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuLabel className="console-profile-menu__head"><span className="console-profile-menu__name">{name}</span>{session?.email && <span className="console-profile-menu__email">{session.email}</span>}<span className="console-profile-menu__email">Administrator</span></DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => window.location.assign(mediaHref('/'))}><Tv size={14} />Open ReelStash</DropdownMenuItem>
          {appLock?.hasPin && <DropdownMenuItem onSelect={() => appLock.lockNow()}><Lock size={14} />Lock now</DropdownMenuItem>}
          <DropdownMenuItem onSelect={() => appLock?.openSettings()}><Lock size={14} />PIN lock…</DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={onSignOut}><LogOut size={14} />Sign out</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>}>
    {!restricted && <SearchTrigger hotkey onOpen={onOpenCommand} placeholder="Search pages, videos, people, actions…" className="console-topbar__search" />}
  </TopBar>;
}
