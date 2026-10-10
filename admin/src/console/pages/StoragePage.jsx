import React, { useEffect, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { Button, Input, NativeSelect } from '@spatika/react';
import { HardDrive, Pencil, Plus, Trash2 } from 'lucide-react';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@spatika/react';
import { createStorageLocation, deleteStorageLocation, getStorageLocations, updateStorageLocation } from '../../api';
import { EmptyState, ErrorState, Field, FormDialog, Meter, MetricStrip, MoreMenu, PageHeader, PageStack, Panel, SectionTabs, SkeletonRows, Status, rowActivation, useConfirm } from '../components';
import { formatBytes, formatNumber, isLowSpace, plural, timeAgo, usedFraction } from '../lib/format';
import { describeError } from '../lib/errors';
import { useNotify } from '../lib/notify';
import { useResource } from '../lib/useResource';
import { NAV_ENTRIES } from '../lib/navigation';

const STORAGE_TYPES = ['LOCAL', 'EXTERNAL', 'NAS', 'NETWORK_SHARE', 'ARCHIVE'];
export const TYPE_LABEL = { LOCAL: 'Local', EXTERNAL: 'External', NAS: 'NAS', NETWORK_SHARE: 'Network share', ARCHIVE: 'Archive' };
export const storageTabs = NAV_ENTRIES.filter(e => e.parent.id === 'storage');
const BLANK = { name: '', type: 'LOCAL', mountPath: '' };

export function DiskDialog({ open, onOpenChange, disk, onSaved }) {
  const notify = useNotify();
  const [form, setForm] = useState(BLANK);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  useEffect(() => { if (open) { setForm(disk ? { name: disk.name, type: disk.type, mountPath: disk.mountPath } : BLANK); setError(null); } }, [open, disk]);
  const initial = disk ? { name: disk.name, type: disk.type, mountPath: disk.mountPath } : BLANK;
  const dirty = JSON.stringify(form) !== JSON.stringify(initial);
  const problems = { name: form.name.trim() ? null : 'Enter a name.', mountPath: !form.mountPath.trim() ? 'Enter the mount path.' : form.mountPath.trim().startsWith('/') ? null : 'Use an absolute path, starting with /.' };

  async function save() {
    setBusy(true); setError(null);
    try {
      if (disk) await updateStorageLocation(disk.id, { name: form.name.trim(), mountPath: form.mountPath.trim() }); else await createStorageLocation({ ...form, name: form.name.trim(), mountPath: form.mountPath.trim() });
      notify.success(disk ? 'Storage location updated' : 'Storage location added');
      onOpenChange(false);
      onSaved();
    } catch (err) { setError(describeError(disk ? 'update the storage location' : 'add the storage location', err)); }
    finally { setBusy(false); }
  }

  return <FormDialog open={open} onOpenChange={onOpenChange} title={disk ? `Edit ${disk.name}` : 'Add storage location'} description="Where ReelStash looks for media."
    submitLabel={disk ? 'Save changes' : 'Add location'} dirty={dirty} canSubmit={!problems.name && !problems.mountPath} submitting={busy} error={error} onSubmit={save}>
    <Field label="Name" error={problems.name}>{props => <Input {...props} autoFocus value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} />}</Field>
    <Field label="Type" hint={disk ? 'The type can’t change after it is added.' : undefined}>{props => <NativeSelect {...props} disabled={!!disk} value={form.type} onChange={e => setForm({ ...form, type: e.target.value })}>{STORAGE_TYPES.map(t => <option key={t} value={t}>{TYPE_LABEL[t]}</option>)}</NativeSelect>}</Field>
    <Field label="Mount path" error={problems.mountPath} hint="Absolute path as seen by the ReelStash server or container.">{props => <Input {...props} value={form.mountPath} onChange={e => setForm({ ...form, mountPath: e.target.value })} placeholder="/mnt/media" />}</Field>
  </FormDialog>;
}

export function useDiskActions(reload) {
  const notify = useNotify();
  const confirm = useConfirm();
  return async function remove(disk) {
    const ok = await confirm({
      title: `Delete “${disk.name}”?`,
      consequence: 'Removes this storage location from ReelStash. It only works once no indexed files are left on it. Files on the disk itself are never deleted.',
      affects: `${disk.mountPath}${disk.online ? '' : ' (offline)'}`, reversible: 'You can add the location again, but you would have to scan it again.', confirmLabel: 'Delete location', tone: 'danger', typeToConfirm: disk.name, reason: { label: 'Reason', placeholder: 'Why are you doing this? Recorded in the audit log.' },
    });
    if (!ok) return false;
    try { await deleteStorageLocation(disk.id, { reason: ok.reason }); notify.success('Storage location deleted'); await reload(); return true; }
    catch (err) { notify.error('delete the storage location', err); return false; }
  };
}

export function StoragePage() {
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const disks = useResource(() => getStorageLocations().then(r => r.items || []), []);
  const [editing, setEditing] = useState(null); // null closed, {} new, disk to edit
  const remove = useDiskActions(disks.reload);
  useEffect(() => { if (params.get('new') === '1') { setEditing({}); setParams(prev => { const n = new URLSearchParams(prev); n.delete('new'); return n; }, { replace: true }); } }, [params, setParams]);

  const list = disks.data || [];
  const total = list.reduce((t, d) => t + (d.totalCapacity || 0), 0);
  const free = list.reduce((t, d) => t + (d.availableCapacity || 0), 0);

  return <PageStack>
    <PageHeader title="Storage" subtitle="Where ReelStash looks for media. Capacity is read every hour and after each scan."
      actions={<Button onClick={() => setEditing({})} leadingIcon={<Plus size={14} />}>Add storage location</Button>} />
    <SectionTabs entries={storageTabs} activePath="/admin/storage" label="Storage sections" />
    <MetricStrip label="Storage summary" loading={disks.loading} items={[
      { label: 'Locations', value: formatNumber(list.length), detail: `${list.filter(d => d.online).length} online`, tone: list.some(d => !d.online) ? 'danger' : undefined },
      { label: 'Total size', value: formatBytes(total) },
      { label: 'Free space', value: formatBytes(free), detail: `${plural(list.filter(isLowSpace).length, 'location')} low on space`, tone: list.some(isLowSpace) ? 'warning' : undefined },
    ]} />
    <Panel bodyless>
      {disks.error && !disks.data ? <ErrorState title="Couldn't load storage locations" error={disks.error} onRetry={disks.reload} />
        : disks.loading ? <SkeletonRows rows={3} label="Loading storage" />
          : list.length === 0 ? <EmptyState icon={HardDrive} title="No storage locations yet" message="Add one to start scanning your library." action={<Button size="sm" onClick={() => setEditing({})}>Add storage location</Button>} />
            : <div className="console-table-responsive"><Table className="console-stack-table" data-actions>
              <TableHeader><TableRow><TableHead>Location</TableHead><TableHead>Status</TableHead><TableHead>Mount path</TableHead><TableHead>Usage</TableHead><TableHead>Last scan</TableHead><TableHead><span className="console-sr-only">Actions</span></TableHead></TableRow></TableHeader>
              <TableBody>
                {list.map(disk => {
                  const used = usedFraction(disk);
                  return <TableRow key={disk.id} {...rowActivation(() => navigate(`/admin/storage/${disk.id}`))} aria-label={`${disk.name}, open details`}>
                    <TableCell><span className="console-title-cell"><HardDrive size={14} aria-hidden="true" /><span className="console-title-cell__copy"><span className="console-title-cell__name">{disk.name}</span><span className="console-title-cell__sub">{TYPE_LABEL[disk.type] || disk.type}</span></span></span></TableCell>
                    <TableCell><Status tone={disk.online ? 'ok' : 'crit'}>{disk.online ? 'Online' : 'Offline'}</Status></TableCell>
                    <TableCell><code className="console-code">{disk.mountPath}</code></TableCell>
                    <TableCell className="console-usage-cell">{used != null ? <><Meter fraction={used} label={`${disk.name} used space`} /><span className="console-small console-num">{formatBytes(disk.availableCapacity)} free of {formatBytes(disk.totalCapacity)}{isLowSpace(disk) ? ' · low space' : ''}</span></> : <span className="console-muted">Not read yet</span>}</TableCell>
                    <TableCell className="console-muted console-nowrap">{disk.lastScanAt ? timeAgo(disk.lastScanAt) : 'Never'}</TableCell>
                    <TableCell><MoreMenu iconOnly label={`Actions for ${disk.name}`} items={[{ label: 'Edit…', icon: Pencil, onSelect: () => setEditing(disk) }, 'separator', { label: 'Delete…', icon: Trash2, danger: true, onSelect: () => remove(disk) }]} /></TableCell>
                  </TableRow>;
                })}
              </TableBody>
            </Table></div>}
    </Panel>
    <DiskDialog open={!!editing} onOpenChange={next => { if (!next) setEditing(null); }} disk={editing?.id ? editing : null} onSaved={disks.reload} />
  </PageStack>;
}
