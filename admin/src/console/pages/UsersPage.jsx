import React, { useEffect, useMemo, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { Button, Switch } from '@spatika/react';
import { Plus, Users } from 'lucide-react';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@spatika/react';
import { getAuthSettings, getLibraries, getUsers, saveAuthSettings } from '../../api';
import {
  EmptyState, ErrorState, MetricStrip, PageHeader, PageStack, Panel, SavedViews, SegmentedTabs, SettingsGroup, SettingsRow, SkeletonRows, Status, Tag,
  TablePagination, TableToolbar, UserAvatar, rowActivation, SortHeader, useConfirm,
} from '../components';
import { formatNumber, plural, timeAgo, userLabel } from '../lib/format';
import { useDebouncedValue } from '../lib/useDebouncedValue';
import { useNotify } from '../lib/notify';
import { useResource } from '../lib/useResource';
import { useUrlState } from '../lib/useUrlState';
import { signInMethod, userStatus } from '../lib/users';
import { AddUserDialog } from './users/UserDialogs';

const PAGE_SIZE_DEFAULT = 25;

function SignInMethods() {
  const settings = useResource(() => getAuthSettings(), []);
  const notify = useNotify();
  const confirm = useConfirm();
  const [busy, setBusy] = useState(false);
  const data = settings.data;

  async function toggle(next) {
    const hasOther = !!data.emergency || (data.plugins || []).length > 0;
    const ok = await confirm({
      title: next ? 'Allow password sign-in?' : 'Turn off password sign-in?',
      consequence: next ? 'People with a ReelStash username and password can sign in again.' : `Nobody can sign in with a username and password. ${hasOther ? 'They must use another method that is running.' : 'No other sign-in method is running, so you could be locked out of ReelStash.'}`,
      affects: 'Everyone who signs in with a password', reversible: next ? true : 'Yes, if you can still sign in to turn it back on.', confirmLabel: next ? 'Allow password sign-in' : 'Turn off', tone: next ? 'default' : 'danger', typeToConfirm: next || hasOther ? undefined : 'lock out', reason: { label: 'Reason', placeholder: 'Why are you doing this? Recorded in the audit log.', required: false },
    });
    if (!ok) return;
    setBusy(true);
    try { await saveAuthSettings(next, { reason: ok.reason }); notify.success(next ? 'Password sign-in is on' : 'Password sign-in is off'); await settings.reload(); }
    catch (err) { notify.error('change password sign-in', err); }
    finally { setBusy(false); }
  }

  if (settings.error && !data) return <Panel bodyless><ErrorState title="Couldn't load sign-in methods" error={settings.error} onRetry={settings.reload} /></Panel>;
  if (!data) return <Panel bodyless><SkeletonRows rows={2} /></Panel>;
  return <SettingsGroup title="Sign-in methods" description="How people can sign in right now. Add more, such as single sign-on, under Plugins.">
    <SettingsRow label="Username and password" description="Accounts kept by ReelStash. Works without any other service.">
      <Switch checked={data.nativeEnabled} disabled={busy} onCheckedChange={toggle} aria-label="Allow password sign-in" />
    </SettingsRow>
    {data.emergency && <SettingsRow label="Emergency password" description="Set in the server environment. Remove it from the environment to turn it off."><Status tone="warn">On</Status></SettingsRow>}
    {(data.plugins || []).map(p => <SettingsRow key={p.id} label={p.name} description="Sign-in plugin"><Status tone="ok">Running</Status></SettingsRow>)}
  </SettingsGroup>;
}

export function UsersPage() {
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const [filters, setFilters] = useUrlState({ tab: 'users', q: '', role: '', status: '', method: '', sort: 'login', dir: 'desc', page: '0', size: String(PAGE_SIZE_DEFAULT) });
  const [search, setSearch] = useState(filters.q);
  const debounced = useDebouncedValue(search, 250);
  const [adding, setAdding] = useState(false);
  const size = Number(filters.size) || PAGE_SIZE_DEFAULT;
  const page = Number(filters.page) || 0;
  const sort = { by: filters.sort, dir: filters.dir };
  // The server pages, searches, filters and sorts; the summary counts come with the same response.
  const users = useResource(() => getUsers({ limit: size, offset: page * size, q: filters.q.trim(), role: filters.role, status: filters.status, method: filters.method, sort: filters.sort, dir: filters.dir }),
    [size, page, filters.q, filters.role, filters.status, filters.method, filters.sort, filters.dir]);
  const libs = useResource(() => getLibraries().then(r => r.items || []), []);
  useEffect(() => { if (debounced !== filters.q) setFilters({ q: debounced }); }, [debounced]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { if (params.get('new') === '1') { setAdding(true); setParams(prev => { const n = new URLSearchParams(prev); n.delete('new'); return n; }, { replace: true }); } }, [params, setParams]);

  const paged = users.data?.items || [];
  const total = users.data?.total ?? 0;
  const counts = users.data?.counts;
  const libraryList = libs.data || [];
  const onSort = next => setFilters({ sort: next.by, dir: next.dir });
  const filtered = !!(filters.q.trim() || filters.role || filters.status || filters.method);

  const accessLabel = u => {
    if (u.role === 'ADMIN') return 'All libraries';
    if (!libraryList.length) return '—';
    const granted = libraryList.filter(l => u.libraryIds.includes(l.id));
    return granted.length === 0 ? 'No access' : granted.length === libraryList.length ? 'All libraries' : granted.length <= 2 ? granted.map(l => l.name).join(', ') : `${granted.length} libraries`;
  };

  return <PageStack>
    <PageHeader title="Users & access" subtitle="Everyone who can sign in to ReelStash, their role, and which libraries they can open."
      actions={<Button onClick={() => setAdding(true)} leadingIcon={<Plus size={14} />}>Add user</Button>} />
    <MetricStrip label="User summary" loading={!counts} items={[
      { label: 'Users', value: formatNumber(counts?.total) },
      { label: 'Administrators', value: formatNumber(counts?.admins), detail: 'Can open every library' },
      { label: 'Signed in, 30 days', value: formatNumber(counts?.signedInRecently), detail: `of ${plural(counts?.total ?? 0, 'user')}` },
      { label: 'Disabled', value: formatNumber(counts?.disabled), tone: counts?.disabled ? 'warning' : undefined },
    ]} />
    <SegmentedTabs page label="Users sections" value={filters.tab} onChange={tab => setFilters({ tab })} options={[{ value: 'users', label: 'Users', count: counts?.total }, { value: 'signin', label: 'Sign-in methods' }]} />

    {filters.tab === 'signin' ? <SignInMethods /> : <Panel bodyless>
      <TableToolbar search={search} onSearch={setSearch} label="Search users" placeholder="Search name, username or email"
        end={<SavedViews storageKey="reelstash-admin-views:users" />}>
        <select className="console-select" aria-label="Filter by role" value={filters.role} onChange={e => setFilters({ role: e.target.value })}><option value="">Any role</option><option value="ADMIN">Administrators</option><option value="USER">Users</option></select>
        <select className="console-select" aria-label="Filter by status" value={filters.status} onChange={e => setFilters({ status: e.target.value })}><option value="">Any status</option><option value="active">Active</option><option value="never">Never signed in</option><option value="disabled">Disabled</option></select>
        <select className="console-select" aria-label="Filter by sign-in method" value={filters.method} onChange={e => setFilters({ method: e.target.value })}><option value="">Any sign-in</option><option value="password">Username and password</option><option value="sso">Single sign-on</option></select>
        {filtered && <button type="button" className="console-link" onClick={() => { setSearch(''); setFilters({ q: '', role: '', status: '', method: '' }); }}>Reset</button>}
      </TableToolbar>
      {users.error && !users.data ? <ErrorState title="Couldn't load users" error={users.error} onRetry={users.reload} />
        : users.loading ? <SkeletonRows rows={5} label="Loading users" />
          : paged.length === 0 ? <EmptyState icon={Users} title={filtered ? 'No users match' : 'No users yet'} message={filtered ? 'Try a different search or clear the filters.' : 'Add a user, or they appear here after their first sign-in.'}
            action={filtered ? <Button size="sm" variant="outline" onClick={() => { setSearch(''); setFilters({ q: '', role: '', status: '', method: '' }); }}>Clear filters</Button> : <Button size="sm" onClick={() => setAdding(true)}>Add user</Button>} />
            : <div className="console-table-responsive"><Table className="console-stack-table">
              <TableHeader><TableRow>
                <TableHead><SortHeader label="User" column="name" sort={sort} onSort={onSort} /></TableHead>
                <TableHead>Status</TableHead>
                <TableHead><SortHeader label="Role" column="role" sort={sort} onSort={onSort} /></TableHead>
                <TableHead>Sign-in</TableHead>
                <TableHead><SortHeader label="Libraries" column="libraries" sort={sort} onSort={onSort} /></TableHead>
                <TableHead><SortHeader label="Last sign-in" column="login" sort={sort} onSort={onSort} /></TableHead>
              </TableRow></TableHeader>
              <TableBody>
                {paged.map(u => {
                  const st = userStatus(u);
                  return <TableRow key={u.id} {...rowActivation(() => navigate(`/admin/users/${u.id}`))} aria-label={`${userLabel(u)}, open details`}>
                    <TableCell><div className="console-user-cell"><UserAvatar name={u.displayName} email={u.email || u.username} /><span className="console-user-cell__copy"><span className="console-user-cell__name">{userLabel(u)}</span><span className="console-user-cell__email">{u.email || u.username || '—'}</span></span></div></TableCell>
                    <TableCell><Status tone={st.tone}>{st.label}</Status></TableCell>
                    <TableCell>{u.role === 'ADMIN' ? <Tag tone="accent">Administrator</Tag> : 'User'}</TableCell>
                    <TableCell className="console-muted">{signInMethod(u)}</TableCell>
                    <TableCell className="console-muted">{accessLabel(u)}</TableCell>
                    <TableCell className="console-muted console-nowrap" title={u.lastLoginAt ? new Date(u.lastLoginAt).toLocaleString() : undefined}>{u.lastLoginAt ? timeAgo(u.lastLoginAt) : 'Never'}</TableCell>
                  </TableRow>;
                })}
              </TableBody>
            </Table></div>}
      {total > 0 && <TablePagination limit={size} offset={page * size} total={total} noun="users" onChange={({ limit, offset }) => setFilters({ size: String(limit), page: String(offset / limit) })} />}
    </Panel>}
    <AddUserDialog open={adding} onOpenChange={setAdding} onAdded={users.reload} />
  </PageStack>;
}
