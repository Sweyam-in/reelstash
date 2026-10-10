import React, { useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { Button, Checkbox } from '@spatika/react';
import { Copy, KeyRound, LogOut, ScrollText, ShieldCheck, ShieldOff, Trash2, UserCheck, UserX, Users } from 'lucide-react';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@spatika/react';
import { deleteUser, getAuditEvents, getLibraries, getUser, getUserSessions, getUsers, revokeUserSession, revokeUserSessions, setUserLibraries, updateUser } from '../../api';
import {
  Callout, CopyButton, DetailList, EmptyState, ErrorState, MoreMenu, PageHeader, PageStack, Panel, ProfileHeader, SegmentedTabs, SettingsGroup, SettingsRow,
  SkeletonRows, Status, Tag, UserAvatar, useConfirm,
} from '../components';
import { formatDateTime, plural, timeAgo, userLabel } from '../lib/format';
import { useNotify } from '../lib/notify';
import { useResource } from '../lib/useResource';
import { useSession } from '../lib/session';
import { useUrlState } from '../lib/useUrlState';
import { accountGuard, isAdminUser, signInMethod, userStatus } from '../lib/users';
import { useCrumb } from '../shell/Breadcrumbs';
import { AuditEventDrawer, AuditEvents } from './AuditPage';
import { ResetPasswordDialog } from './users/UserDialogs';

const REASON = { label: 'Reason', placeholder: 'Why are you doing this? Recorded in the audit log.', required: false };
const REQUIRED_REASON = { ...REASON, required: true };

function SessionsPanel({ userId, name, isSelf }) {
  const notify = useNotify();
  const confirm = useConfirm();
  const sessions = useResource(() => getUserSessions(userId), [userId]);
  const items = sessions.data?.items || [];

  async function revoke(session) {
    const label = session.kind === 'device' ? session.device : 'this browser session';
    const ok = await confirm({ title: `Sign out ${label}?`, consequence: `${name} is signed out on ${label}. They can sign in again.`, affects: name, reversible: true, confirmLabel: 'Sign out', reason: REASON });
    if (!ok) return;
    try { await revokeUserSession(userId, session.id, { reason: ok.reason }); notify.success('Signed out'); await sessions.reload(); }
    catch (err) { notify.error('sign them out', err); }
  }
  async function revokeAll() {
    const ok = await confirm({ title: `Sign ${name} out everywhere?`, consequence: `Every browser and app session of ${name} ends now${isSelf ? ' (except the one you are using)' : ''}. They can sign in again.`, affects: plural(items.length, 'session'), reversible: true, confirmLabel: 'Sign out everywhere', tone: 'danger', reason: REASON });
    if (!ok) return;
    try { const r = await revokeUserSessions(userId, { reason: ok.reason }); notify.success(`Ended ${plural(r.revoked, 'session')}`); await sessions.reload(); }
    catch (err) { notify.error('sign them out', err); }
  }

  return <Panel title="Active sessions" subtitle="Browsers and apps this person is signed in on." actions={items.length > 0 && <Button size="sm" variant="outline" onClick={revokeAll} leadingIcon={<LogOut size={14} />}>Sign out everywhere…</Button>} bodyless>
    {sessions.error && !sessions.data ? <ErrorState title="Couldn't load sessions" error={sessions.error} onRetry={sessions.reload} />
      : sessions.loading ? <SkeletonRows rows={2} />
        : items.length === 0 ? <EmptyState compact title="Not signed in anywhere" message="No live sessions." />
          : <div className="console-table-responsive"><Table className="console-stack-table" data-actions>
            <TableHeader><TableRow><TableHead>Where</TableHead><TableHead>Started</TableHead><TableHead>Last seen</TableHead><TableHead>Expires</TableHead><TableHead><span className="console-sr-only">Actions</span></TableHead></TableRow></TableHeader>
            <TableBody>{items.map(s => <TableRow key={s.id}>
              <TableCell>{s.kind === 'device' ? <><strong>{s.device}</strong> <span className="console-muted">{s.platform}</span></> : <>Browser{s.plugin ? <span className="console-muted"> · via {s.plugin}</span> : null}</>}</TableCell>
              <TableCell className="console-nowrap" title={formatDateTime(s.createdAt)}>{timeAgo(s.createdAt)}</TableCell>
              <TableCell className="console-nowrap console-muted">{s.lastSeenAt ? timeAgo(s.lastSeenAt) : '—'}</TableCell>
              <TableCell className="console-nowrap console-muted">{formatDateTime(s.expiresAt)}</TableCell>
              <TableCell><Button size="sm" variant="ghost" onClick={() => revoke(s)} aria-label={`Sign out ${s.kind === 'device' ? s.device : 'browser session'}`}>Sign out…</Button></TableCell>
            </TableRow>)}</TableBody>
          </Table></div>}
  </Panel>;
}

function ActivityPanel({ userId }) {
  const [open, setOpen] = useState(null);
  const log = useResource(() => getAuditEvents({ targetKind: 'users', targetId: userId, limit: 25 }), [userId]);
  const items = log.data?.items || [];
  return <Panel title="Changes made to this account" subtitle="From the audit log. Watch history and other personal content are never shown here." bodyless>
    {log.error && !log.data ? <ErrorState title="Couldn't load the audit log" error={log.error} onRetry={log.reload} />
      : log.loading ? <SkeletonRows rows={3} />
        : items.length === 0 ? <EmptyState compact icon={ScrollText} title="No recorded changes" message="Changes administrators make to this account show up here." />
          : <AuditEvents events={items} onOpen={setOpen} />}
    <AuditEventDrawer event={open} onClose={() => setOpen(null)} />
  </Panel>;
}

export function UserDetailPage() {
  const { id } = useParams();
  const navigate = useNavigate();
  const session = useSession();
  const notify = useNotify();
  const confirm = useConfirm();
  const [filters, setFilters] = useUrlState({ tab: 'overview' });
  const found = useResource(() => getUser(id), [id]);
  const adminCount = useResource(() => getUsers({ role: 'ADMIN', status: 'active', limit: 1 }).then(r => r.total), [id]);
  const libs = useResource(() => getLibraries().then(r => r.items || []), []);
  const [resetting, setResetting] = useState(false);
  const [busy, setBusy] = useState(false);
  const user = found.data;
  const libraries = libs.data || [];
  const missing = found.error?.status === 404;
  useCrumb(user ? userLabel(user) : missing ? 'Not found' : null);

  // Library access edits stay local until saved.
  const [granted, setGranted] = useState(null);
  useEffect(() => { if (user) setGranted(user.libraryIds); }, [user?.id, user?.libraryIds?.join(',')]); // eslint-disable-line react-hooks/exhaustive-deps
  const accessDirty = !!user && granted !== null && [...granted].sort().join() !== [...user.libraryIds].sort().join();
  useEffect(() => {
    if (!accessDirty) return undefined;
    const warn = e => { e.preventDefault(); e.returnValue = ''; };
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, [accessDirty]);

  const guard = useMemo(() => (user ? accountGuard(user, adminCount.data ?? 2, session.id) : {}), [user, adminCount.data, session.id]);

  if (found.error && !user && !missing) return <PageStack><PageHeader title="User" back={{ to: '/admin/users', label: 'All users' }} /><Panel bodyless><ErrorState title="Couldn't load this user" error={found.error} onRetry={found.reload} /></Panel></PageStack>;
  if (missing) return <PageStack><PageHeader title="User not found" back={{ to: '/admin/users', label: 'All users' }} /><Panel bodyless><EmptyState icon={Users} title="There is no user with this ID" message="They may have been deleted, or the link is wrong." /></Panel></PageStack>;
  if (!user) return <PageStack><Panel bodyless><SkeletonRows rows={4} label="Loading user" /></Panel></PageStack>;

  const status = userStatus(user);
  const name = userLabel(user);
  const reload = () => Promise.all([found.reload(), adminCount.reload()]);

  async function apply(patch, success, reason) {
    setBusy(true);
    try { await updateUser(user.id, { role: user.role, disabled: user.disabled, ...patch }, { reason }); notify.success(success); await reload(); }
    catch (err) { notify.error('update this user', err); }
    finally { setBusy(false); }
  }

  async function toggleDisabled() {
    if (!user.disabled && guard.cannotDisable) { notify.info('Can’t disable this account', guard.cannotDisable); return; }
    const disabling = !user.disabled;
    const ok = await confirm({
      title: disabling ? `Disable ${name}?` : `Enable ${name}?`,
      consequence: disabling ? `${name} is signed out and can’t sign in again until you enable the account. Their history, ratings and playlists are kept.` : `${name} can sign in again with their previous access.`,
      affects: name, reversible: true, confirmLabel: disabling ? 'Disable account' : 'Enable account', tone: disabling ? 'danger' : 'default', reason: REASON,
    });
    if (ok) await apply({ disabled: disabling }, disabling ? `${name} disabled` : `${name} enabled`, ok.reason);
  }

  async function changeRole(role) {
    if (role !== 'ADMIN' && guard.cannotDemote) { notify.info('Can’t change this role', guard.cannotDemote); return; }
    const toAdmin = role === 'ADMIN';
    const ok = await confirm({
      title: toAdmin ? `Make ${name} an administrator?` : `Remove ${name}’s administrator role?`,
      consequence: toAdmin ? 'Administrators open every library, edit all metadata, run jobs and manage users, plugins and settings.' : `${name} can no longer use this console, and only opens the libraries you grant them.`,
      affects: name, reversible: true, confirmLabel: toAdmin ? 'Make administrator' : 'Make user', tone: toAdmin ? 'danger' : 'default', typeToConfirm: toAdmin ? user.username || 'admin' : undefined, reason: REASON,
    });
    if (ok) await apply({ role }, toAdmin ? `${name} is now an administrator` : `${name} is now a user`, ok.reason);
  }

  async function remove() {
    if (guard.cannotDelete) { notify.info('Can’t delete this account', guard.cannotDelete); return; }
    const ok = await confirm({
      title: `Delete ${name}?`,
      consequence: `${name}’s account is removed for good, with their watch history, ratings, playlists, sessions and library access. They are signed out immediately. Disabling the account instead keeps all of that.`,
      affects: name, reversible: false, confirmLabel: 'Delete account', tone: 'danger', typeToConfirm: user.username || name, reason: REQUIRED_REASON,
    });
    if (!ok) return;
    try { await deleteUser(user.id, { reason: ok.reason }); notify.success(`${name} deleted`); navigate('/admin/users'); }
    catch (err) { notify.error('delete this user', err); }
  }

  async function saveAccess() {
    setBusy(true);
    try { await setUserLibraries(user.id, granted); notify.success('Library access saved', `${name} can open ${granted.length === 1 ? '1 library' : `${granted.length} libraries`}.`); await found.reload(); }
    catch (err) { notify.error('save library access', err); }
    finally { setBusy(false); }
  }

  const switchTab = async tab => {
    if (accessDirty && tab !== filters.tab) {
      const ok = await confirm({ title: 'Discard unsaved changes?', consequence: 'The library access changes have not been saved and will be lost.', confirmLabel: 'Discard changes', tone: 'danger' });
      if (!ok) return;
      setGranted(user.libraryIds);
    }
    setFilters({ tab });
  };

  return <PageStack>
    <ProfileHeader
      mark={<UserAvatar name={user.displayName} email={user.email || user.username} size={44} />}
      title={name}
      status={<Status tone={status.tone}>{status.label}</Status>}
      meta={<>
        {user.email && <span>{user.email}</span>}
        <span>{isAdminUser(user) ? 'Administrator' : 'User'}</span>
        <span>{signInMethod(user)}</span>
        <CopyButton value={String(user.id)} label="User ID" display={`ID ${user.id}`} />
      </>}
      actions={<>
        <Button variant={user.disabled ? 'default' : 'outline'} disabled={busy} onClick={toggleDisabled} leadingIcon={user.disabled ? <UserCheck size={14} /> : <UserX size={14} />}>{user.disabled ? 'Enable account' : 'Disable account'}</Button>
        <MoreMenu items={[
          user.passwordLogin && { label: 'Reset password…', icon: KeyRound, onSelect: () => setResetting(true) },
          isAdminUser(user) ? { label: 'Remove administrator role…', icon: ShieldOff, onSelect: () => changeRole('USER'), disabled: !!guard.cannotDemote } : { label: 'Make administrator…', icon: ShieldCheck, onSelect: () => changeRole('ADMIN') },
          { label: 'Copy user ID', icon: Copy, onSelect: () => navigator.clipboard?.writeText(String(user.id)) },
          'separator',
          { label: 'Delete account…', icon: Trash2, danger: true, disabled: !!guard.cannotDelete, onSelect: remove },
        ]} />
      </>} />

    <SegmentedTabs page label="User sections" value={filters.tab} onChange={switchTab} options={[
      { value: 'overview', label: 'Overview' }, { value: 'libraries', label: 'Library access', count: isAdminUser(user) ? undefined : user.libraryIds.length },
      { value: 'sessions', label: 'Sessions' }, { value: 'activity', label: 'Activity' }, { value: 'danger', label: 'Access & danger zone' },
    ]} />

    {filters.tab === 'overview' && <Panel title="Account" subtitle="What ReelStash knows about this account. Watch history and other personal content are never shown here." bodyless>
      <DetailList flush items={[
        { term: 'Name', details: user.displayName || '—' },
        { term: 'Username', details: user.username ? <span className="console-code">{user.username}</span> : '—' },
        { term: 'Email', details: user.email || '—' },
        { term: 'Role', details: isAdminUser(user) ? <Tag tone="accent">Administrator</Tag> : 'User' },
        { term: 'Sign-in', details: signInMethod(user) },
        { term: 'Last sign-in', details: user.lastLoginAt ? `${formatDateTime(user.lastLoginAt)} (${timeAgo(user.lastLoginAt)})` : 'Never' },
        { term: 'Libraries', details: isAdminUser(user) ? 'All libraries (administrator)' : user.libraryIds.length ? libraries.filter(l => user.libraryIds.includes(l.id)).map(l => l.name).join(', ') || `${user.libraryIds.length}` : 'None' },
        { term: 'User ID', details: <CopyButton value={String(user.id)} label="User ID" /> },
      ]} />
    </Panel>}

    {filters.tab === 'libraries' && (isAdminUser(user)
      ? <Panel bodyless><EmptyState icon={ShieldCheck} title="Administrators open every library" message="Library access only applies to users. Remove the administrator role first to limit what this account can open." /></Panel>
      : <SettingsGroup title="Libraries this user can open" description="Changes apply the next time they load a page."
        footer={<><span>{accessDirty ? 'Unsaved changes' : 'Saved'}</span><span><Button size="sm" variant="ghost" disabled={!accessDirty || busy} onClick={() => setGranted(user.libraryIds)}>Discard</Button> <Button size="sm" disabled={!accessDirty} loading={busy} onClick={saveAccess}>Save access</Button></span></>}>
        {libs.loading ? <SkeletonRows rows={2} /> : libraries.length === 0 ? <EmptyState compact title="No libraries yet" message="Create a library first, then choose who can open it." />
          : libraries.map(l => <SettingsRow key={l.id} label={l.name} description={`${l.mode === 'private' ? 'Private videos' : 'Entertainment'} · ${l.mediaCount.toLocaleString()} titles${l.defaultAccess ? ' · new users get access by default' : ''}`} htmlFor={`lib-${l.id}`}>
            <Checkbox id={`lib-${l.id}`} checked={(granted || []).includes(l.id)} onCheckedChange={on => setGranted(cur => (on ? [...(cur || []), l.id] : (cur || []).filter(x => x !== l.id)))} aria-label={`Can open ${l.name}`} />
          </SettingsRow>)}
      </SettingsGroup>)}

    {filters.tab === 'sessions' && <SessionsPanel userId={user.id} name={name} isSelf={guard.isSelf} />}
    {filters.tab === 'activity' && <ActivityPanel userId={user.id} />}

    {filters.tab === 'danger' && <>
      {(guard.isSelf || guard.lastAdmin) && <Callout tone="warn" icon={<ShieldCheck />} title={guard.isSelf ? 'This is your own account' : 'This is the only enabled administrator'}>{guard.isSelf ? 'You can’t disable, demote or delete it, so you can’t lock yourself out.' : 'Make someone else an administrator before disabling, demoting or deleting this account.'}</Callout>}
      <SettingsGroup title="Role" description="What this account may do in ReelStash.">
        <SettingsRow label={isAdminUser(user) ? 'Administrator' : 'User'} description={isAdminUser(user) ? 'Opens every library and manages everything in this console.' : 'Opens only the libraries granted to them.'}>
          {isAdminUser(user) ? <Button size="sm" variant="outline" disabled={busy || !!guard.cannotDemote} title={guard.cannotDemote || undefined} onClick={() => changeRole('USER')}>Remove administrator role…</Button>
            : <Button size="sm" variant="outline" disabled={busy} onClick={() => changeRole('ADMIN')}>Make administrator…</Button>}
        </SettingsRow>
      </SettingsGroup>
      {user.passwordLogin && <SettingsGroup title="Password" description="Accounts that sign in with a ReelStash password.">
        <SettingsRow label="Reset password" description="Sets a new password and signs this person out everywhere."><Button size="sm" variant="outline" onClick={() => setResetting(true)}>Reset password…</Button></SettingsRow>
      </SettingsGroup>}
      <SettingsGroup danger title="Danger zone" description="Disabling is reversible. Deleting is not.">
        <SettingsRow label={user.disabled ? 'Enable account' : 'Disable account'} description={user.disabled ? 'Lets this person sign in again.' : 'Signs them out and blocks sign-in. Their data is kept.'}>
          <Button size="sm" variant={user.disabled ? 'default' : 'outline'} disabled={busy || (!user.disabled && !!guard.cannotDisable)} title={(!user.disabled && guard.cannotDisable) || undefined} onClick={toggleDisabled}>{user.disabled ? 'Enable account' : 'Disable account…'}</Button>
        </SettingsRow>
        <SettingsRow label="Delete account" description="Removes the account with its history, ratings, playlists and access. This can’t be undone.">
          <Button size="sm" variant="destructive" disabled={busy || !!guard.cannotDelete} title={guard.cannotDelete || undefined} onClick={remove}>Delete account…</Button>
        </SettingsRow>
      </SettingsGroup>
    </>}
    <ResetPasswordDialog user={user} open={resetting} onOpenChange={setResetting} />
  </PageStack>;
}
