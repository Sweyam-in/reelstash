import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { ScrollText } from 'lucide-react';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@spatika/react';
import { getAuditEvents } from '../../api';
import { DetailDrawer, DetailList, DrawerSection, EmptyState, ErrorState, PageHeader, PageStack, Panel, SkeletonRows, TablePagination, TableToolbar, rowActivation } from '../components';
import { AUDIT_GROUPS, auditLabel, auditTargetLink } from '../lib/audit';
import { formatDateTime, timeAgo } from '../lib/format';
import { useDebouncedValue } from '../lib/useDebouncedValue';
import { useResource } from '../lib/useResource';
import { useUrlState } from '../lib/useUrlState';

export const AuditEvents = ({ events, onOpen }) => <div className="console-table-responsive"><Table className="console-stack-table">
  <TableHeader><TableRow><TableHead>When</TableHead><TableHead>Who</TableHead><TableHead>What</TableHead><TableHead>Target</TableHead><TableHead>Reason</TableHead></TableRow></TableHeader>
  <TableBody>{events.map(e => <TableRow key={e.id} {...rowActivation(() => onOpen(e))} aria-label={`${auditLabel(e.action)}, ${timeAgo(e.at)}, open details`}>
    <TableCell className="console-nowrap" title={formatDateTime(e.at)}>{timeAgo(e.at)}</TableCell>
    <TableCell className="console-nowrap">{e.actorName || (e.actorId ? `User ${e.actorId}` : '—')}</TableCell>
    <TableCell><strong>{auditLabel(e.action)}</strong></TableCell>
    <TableCell className="console-truncate-cell">{e.targetLabel || (e.targetId ? `#${e.targetId}` : '—')}</TableCell>
    <TableCell className="console-truncate-cell console-muted" title={e.reason}>{e.reason || '—'}</TableCell>
  </TableRow>)}</TableBody>
</Table></div>;

export function AuditEventDrawer({ event, onClose }) {
  if (!event) return null;
  const link = auditTargetLink(event);
  return <DetailDrawer open onClose={onClose} eyebrow={`Event #${event.id}`} title={auditLabel(event.action)} description={formatDateTime(event.at)}>
    <DrawerSection title="What happened"><DetailList items={[
      { term: 'Who', details: event.actorName || (event.actorId ? `User ${event.actorId}` : '—') },
      { term: 'Action', details: <code className="console-code">{event.action}</code> },
      { term: 'Target', details: link ? <Link className="console-link" to={link}>{event.targetLabel || `#${event.targetId}`}</Link> : (event.targetLabel || (event.targetId ? `#${event.targetId}` : '—')) },
      { term: 'Reason', details: event.reason || <span className="console-muted">None given</span> },
      { term: 'Request', details: <code className="console-code">{event.method} {event.route}</code> },
    ]} /></DrawerSection>
    {event.detail && <DrawerSection title="Details"><pre className="console-pre">{JSON.stringify(event.detail, null, 2)}</pre></DrawerSection>}
  </DetailDrawer>;
}

export function AuditPage() {
  const [filters, setFilters] = useUrlState({ q: '', action: '', page: '0', size: '50', event: '' });
  const [search, setSearch] = useState(filters.q);
  const q = useDebouncedValue(search.trim(), 300);
  const [open, setOpen] = useState(null);
  useEffect(() => { if (q !== filters.q) setFilters({ q }); }, [q]); // eslint-disable-line react-hooks/exhaustive-deps
  const size = Number(filters.size) || 50;
  const page = Number(filters.page) || 0;
  const log = useResource(() => getAuditEvents({ q: filters.q, action: filters.action, limit: size, offset: page * size }), [filters.q, filters.action, page, size]);
  const items = log.data?.items || [];
  const filtered = !!(filters.q || filters.action);

  return <PageStack>
    <PageHeader title="Audit log" subtitle="Every change an administrator makes, with who made it, what it touched and why. Reading the console is not recorded." />
    <Panel bodyless>
      <TableToolbar search={search} onSearch={setSearch} label="Search the audit log" placeholder="Search who, what, target or reason">
        <select className="console-select" aria-label="Filter by area" value={filters.action} onChange={e => setFilters({ action: e.target.value })}>{AUDIT_GROUPS.map(([v, l]) => <option key={v} value={v}>{l}</option>)}</select>
        {filtered && <button type="button" className="console-link" onClick={() => { setSearch(''); setFilters({ q: '', action: '' }); }}>Reset</button>}
      </TableToolbar>
      {log.error && !log.data ? <ErrorState title="Couldn't load the audit log" error={log.error} onRetry={log.reload} />
        : log.loading ? <SkeletonRows rows={8} label="Loading the audit log" />
          : items.length === 0 ? <EmptyState icon={ScrollText} title={filtered ? 'No events match' : 'Nothing recorded yet'} message={filtered ? 'Try another search or area.' : 'Changes made from now on show up here.'} />
            : <AuditEvents events={items} onOpen={setOpen} />}
      {log.data && log.data.total > 0 && <TablePagination limit={size} offset={page * size} total={log.data.total} noun={log.data.total === 1 ? 'event' : 'events'} onChange={({ limit, offset }) => setFilters({ size: String(limit), page: String(offset / limit) })} />}
    </Panel>
    <AuditEventDrawer event={open} onClose={() => setOpen(null)} />
  </PageStack>;
}
