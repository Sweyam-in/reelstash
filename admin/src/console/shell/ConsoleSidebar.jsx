import React from 'react';
import { Link, useLocation } from 'react-router-dom';
import { NavItem, NavSection, Sidebar } from '@spatika/react';
import { ExternalLink } from 'lucide-react';
import { mediaHref } from '../../appLinks';
import { AppWorkspaceSwitcher } from '../../workspaces';
import { visibleNavGroups, navEntryFor } from '../lib/navigation';
import { formatCompact } from '../lib/format';
import { useSignals } from '../lib/signals';

export function ConsoleSidebar({ session }) {
  const { pathname } = useLocation();
  const { reviewCount, summary } = useSignals();
  const active = navEntryFor(pathname)?.parent?.id;

  function meta(item) {
    if (item.id === 'review' && reviewCount > 0) return <span className="console-nav-count" data-tone="warn" aria-label={`${reviewCount} titles waiting`}>{formatCompact(reviewCount)}</span>;
    if (item.id === 'system' && summary.issues > 0) return <span className="console-nav-count" data-tone={summary.tone} aria-label={`${summary.issues} issues`}>{summary.issues}</span>;
    return undefined;
  }

  return <Sidebar className="console-sidebar" aria-label="Console navigation"
    header={<AppWorkspaceSwitcher current="admin" canAdmin />}
    footer={<div className="console-sidebar__foot"><a href={mediaHref('/')}><ExternalLink size={12} aria-hidden="true" />Open ReelStash</a></div>}>
    {visibleNavGroups(session).map(group => <NavSection key={group.id} title={group.title}>
      {group.items.map(item => {
        const isActive = item.id === active;
        return <NavItem key={item.id} asChild label={item.label} icon={<item.icon size={16} aria-hidden="true" />} active={isActive} meta={meta(item)}>
          <Link to={item.path} aria-current={isActive ? 'page' : undefined} />
        </NavItem>;
      })}
    </NavSection>)}
  </Sidebar>;
}
