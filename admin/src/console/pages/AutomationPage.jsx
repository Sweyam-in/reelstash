import React, { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Button, Input, NativeSelect, Switch } from '@spatika/react';
import { CalendarClock, Eye, Plus, Trash2 } from 'lucide-react';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@spatika/react';
import { createSchedule, deleteSchedule, getAutomation, setLibraryWatch, updateSchedule } from '../../api';
import { CRON_PRESETS, JOB_KINDS, describeCron, jobKindLabel, looksLikeCron, presetFor } from '../../cronUtil';
import { Callout, EmptyState, ErrorState, Field, FormDialog, MetricStrip, PageHeader, PageStack, Panel, SectionTabs, SkeletonRows, Status, Tag, useConfirm } from '../components';
import { describeError } from '../lib/errors';
import { formatNumber, plural, timeAgo, timeUntil } from '../lib/format';
import { tabsOf } from '../lib/navigation';
import { useNotify } from '../lib/notify';
import { useResource } from '../lib/useResource';
import { parseTs } from '../../pages/timeUtil';

const CUSTOM = 'custom';
const when = value => (value ? parseTs(value)?.toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' }) || '—' : '—');
export const automationTabs = tabsOf('automation');

const STARTERS = [
  { id: 'nightly-scan', title: 'Nightly scan', detail: 'Every library, every night at 03:00', body: { jobKind: 'SCAN', cron: '0 3 * * *' } },
  { id: 'weekly-dupes', title: 'Weekly duplicate check', detail: 'Every Sunday at 03:00', body: { jobKind: 'DUPLICATE_SCAN', cron: '0 3 * * 0' } },
  { id: 'weekly-housekeeping', title: 'Weekly housekeeping', detail: 'Every Sunday at 04:00', body: { jobKind: 'HOUSEKEEPING', cron: '0 4 * * 0' } },
];

function ScheduleDialog({ open, onOpenChange, libraries, timezone, onSaved }) {
  const notify = useNotify();
  const [jobKind, setJobKind] = useState('SCAN');
  const [library, setLibrary] = useState('');
  const [preset, setPreset] = useState('nightly');
  const [custom, setCustom] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  useEffect(() => { if (open) { setJobKind('SCAN'); setLibrary(''); setPreset('nightly'); setCustom(''); setError(null); } }, [open]);
  const cron = preset === CUSTOM ? custom.trim() : CRON_PRESETS.find(p => p.id === preset).cron;
  const kind = JOB_KINDS.find(k => k.id === jobKind);
  const cronError = preset === CUSTOM && !looksLikeCron(cron) ? 'Use five fields: minute, hour, day of month, month, weekday. For example */30 * * * *.' : null;

  async function save() {
    setBusy(true); setError(null);
    try {
      await createSchedule({ jobKind, cron, ...(jobKind === 'SCAN' && library ? { libraryId: Number(library) } : {}) });
      notify.success('Schedule added', 'It runs from the next matching minute.');
      onOpenChange(false);
      onSaved();
    } catch (err) { setError(describeError('add the schedule', err)); }
    finally { setBusy(false); }
  }

  return <FormDialog open={open} onOpenChange={onOpenChange} title="New schedule" description={`Times are in the server’s time zone${timezone ? ` (${timezone})` : ''}.`}
    submitLabel="Add schedule" dirty canSubmit={!cronError && looksLikeCron(cron)} submitting={busy} error={error} onSubmit={save}>
    <Field label="Run" hint={kind.hint}>{props => <NativeSelect {...props} value={jobKind} onChange={e => setJobKind(e.target.value)}>{JOB_KINDS.map(k => <option key={k.id} value={k.id}>{k.label}</option>)}</NativeSelect>}</Field>
    {jobKind === 'SCAN' && <Field label="On">{props => <NativeSelect {...props} value={library} onChange={e => setLibrary(e.target.value)}><option value="">Every library</option>{libraries.map(l => <option key={l.id} value={l.id}>{l.name}</option>)}</NativeSelect>}</Field>}
    <Field label="When">{props => <NativeSelect {...props} value={preset} onChange={e => setPreset(e.target.value)}>{CRON_PRESETS.map(p => <option key={p.id} value={p.id}>{p.label}</option>)}<option value={CUSTOM}>Custom (cron expression)…</option></NativeSelect>}</Field>
    {preset === CUSTOM && <Field label="Cron expression" error={cronError} hint="Fields: minute, hour, day of month, month, weekday.">{props => <Input {...props} className="console-mono" autoFocus value={custom} onChange={e => setCustom(e.target.value)} spellCheck={false} placeholder="*/30 * * * *" />}</Field>}
  </FormDialog>;
}

function WatchStatus({ library }) {
  if (!library.watchEnabled) return <Status tone="neutral">Off</Status>;
  const watch = library.watch;
  if (watch?.watching) return <span title={watch.lastEventAt ? `Last change seen ${timeAgo(watch.lastEventAt)}` : 'No changes seen yet'}><Status tone="ok">Watching {watch.directories.toLocaleString()} {watch.directories === 1 ? 'folder' : 'folders'}</Status></span>;
  if (watch?.error) return <span title={watch.error}><Status tone="crit">Can’t watch</Status></span>;
  return <Status tone="neutral">Starting…</Status>;
}

/** Admin: recurring scans and file watching, so new files appear without pressing Scan. */
export function AutomationPage() {
  const [params, setParams] = useSearchParams();
  const notify = useNotify();
  const confirm = useConfirm();
  const data = useResource(() => getAutomation(), [], { interval: 15_000 });
  const [creating, setCreating] = useState(false);
  const [busyId, setBusyId] = useState(null);
  useEffect(() => { if (params.get('new') === '1') { setCreating(true); setParams(p => { const n = new URLSearchParams(p); n.delete('new'); return n; }, { replace: true }); } }, [params, setParams]);

  const schedules = data.data?.schedules || [];
  const libraries = data.data?.libraries || [];
  const active = schedules.filter(s => s.enabled && !s.error);
  const next = active.filter(s => s.nextRunAt).sort((a, b) => parseTs(a.nextRunAt) - parseTs(b.nextRunAt))[0];
  const watching = libraries.filter(l => l.watchEnabled);

  async function toggleSchedule(schedule) {
    setBusyId(`s${schedule.id}`);
    try { await updateSchedule(schedule.id, { enabled: !schedule.enabled }); notify.success(schedule.enabled ? 'Schedule paused' : 'Schedule resumed'); await data.reload(); }
    catch (err) { notify.error('update the schedule', err); }
    finally { setBusyId(null); }
  }
  async function toggleWatch(library) {
    if (!library.watchEnabled) {
      const ok = await confirm({
        title: `Watch ${library.name} for new files?`,
        consequence: `ReelStash scans ${library.name} about a minute after files in its folders stop changing. A library scan also removes titles whose files are gone.`,
        affects: `${library.name} (${plural(library.folders, 'folder')})`, reversible: true, confirmLabel: 'Start watching',
      });
      if (!ok) return;
    }
    setBusyId(`w${library.id}`);
    try { await setLibraryWatch(library.id, !library.watchEnabled); notify.success(library.watchEnabled ? 'File watching off' : 'File watching on'); await data.reload(); if (!library.watchEnabled) setTimeout(data.reload, 3000); }
    catch (err) { notify.error('change file watching', err); }
    finally { setBusyId(null); }
  }
  async function addStarter(starter) {
    setBusyId(starter.id);
    try { await createSchedule(starter.body); notify.success('Schedule added', starter.detail); await data.reload(); }
    catch (err) { notify.error('add the schedule', err); }
    finally { setBusyId(null); }
  }
  async function remove(schedule) {
    const ok = await confirm({ title: 'Remove this schedule?', consequence: `${jobKindLabel(schedule.jobKind)}, ${describeCron(schedule.cron)}. Jobs already running are not affected.`, affects: schedule.libraryName || (schedule.jobKind === 'SCAN' ? 'Every library' : 'Everything'), reversible: 'You can add it again.', confirmLabel: 'Remove schedule', tone: 'danger', reason: { label: 'Reason', placeholder: 'Why are you doing this? Recorded in the audit log.', required: false } });
    if (!ok) return;
    try { await deleteSchedule(schedule.id, { reason: ok.reason }); notify.success('Schedule removed'); await data.reload(); }
    catch (err) { notify.error('remove the schedule', err); }
  }

  return <PageStack>
    <PageHeader title="Automation" subtitle="Keep the library current without pressing Scan: run jobs on a schedule, and pick up new files as they arrive."
      actions={<Button onClick={() => setCreating(true)} leadingIcon={<Plus size={14} />}>New schedule</Button>} />
    <SectionTabs entries={automationTabs} activePath="/admin/automation" label="Automation sections" />
    {data.error && !data.data ? <Panel bodyless><ErrorState title="Couldn't load automation" error={data.error} onRetry={data.reload} /></Panel> : <>
      <MetricStrip label="Automation summary" loading={data.loading} items={[
        { label: 'Active schedules', value: formatNumber(active.length), detail: `of ${schedules.length}` },
        { label: 'Next run', value: next ? timeUntil(next.nextRunAt) : '—', detail: next ? `${jobKindLabel(next.jobKind)}${next.libraryName ? ` · ${next.libraryName}` : ''}` : undefined },
        { label: 'Watching for files', value: formatNumber(watching.length), detail: `of ${plural(libraries.length, 'library', 'libraries')}` },
      ]} />
      <Panel title="Scheduled jobs" subtitle={`Recurring scans and duplicate checks. A failed run notifies you like any other failed job.${data.data?.timezone ? ` Times are in ${data.data.timezone}.` : ''}`} bodyless>
        {data.loading ? <SkeletonRows rows={2} /> : schedules.length === 0
          ? <EmptyState icon={CalendarClock} title="Nothing scheduled" message="Start with one of these, or add your own."
            action={<>{STARTERS.map(s => <Button key={s.id} size="sm" variant="outline" disabled={busyId === s.id} onClick={() => addStarter(s)} title={s.detail}>{s.title}</Button>)}<Button size="sm" onClick={() => setCreating(true)}>Custom schedule…</Button></>} />
          : <div className="console-table-responsive"><Table className="console-stack-table" data-actions>
            <TableHeader><TableRow><TableHead>Job</TableHead><TableHead>Applies to</TableHead><TableHead>Schedule</TableHead><TableHead>Next run</TableHead><TableHead>Last run</TableHead><TableHead>On</TableHead><TableHead><span className="console-sr-only">Actions</span></TableHead></TableRow></TableHeader>
            <TableBody>{schedules.map(s => {
              const label = `${jobKindLabel(s.jobKind)} ${describeCron(s.cron)}`;
              return <TableRow key={s.id}>
                <TableCell><strong>{jobKindLabel(s.jobKind)}</strong></TableCell>
                <TableCell>{s.jobKind !== 'SCAN' ? 'Everything' : s.libraryName ? <Tag>{s.libraryName}</Tag> : 'Every library'}</TableCell>
                <TableCell title={s.cron}>{s.error ? <span title={s.error}><Status tone="crit">Invalid: {s.cron}</Status></span> : presetFor(s.cron) ? presetFor(s.cron).label : <code className="console-code">{s.cron}</code>}</TableCell>
                <TableCell className="console-nowrap" title={s.enabled ? when(s.nextRunAt) : undefined}>{s.enabled ? timeUntil(s.nextRunAt) : <Status tone="neutral">Paused</Status>}</TableCell>
                <TableCell className="console-muted console-nowrap">{s.lastRunAt ? timeAgo(s.lastRunAt) : 'Never'}</TableCell>
                <TableCell><Switch checked={s.enabled} disabled={busyId === `s${s.id}`} onCheckedChange={() => toggleSchedule(s)} aria-label={`${s.enabled ? 'Pause' : 'Resume'} ${label}`} /></TableCell>
                <TableCell><Button variant="ghost" size="icon-sm" aria-label={`Remove ${label}`} onClick={() => remove(s)}><Trash2 size={14} /></Button></TableCell>
              </TableRow>;
            })}</TableBody>
          </Table></div>}
      </Panel>
      <Panel title="Watch for new files" subtitle="ReelStash watches a library’s folders and scans it about a minute after files stop changing. It only sees changes made on this server; for a network share other machines write to, use a schedule instead." bodyless>
        {data.data && !data.data.watcherRunning && <Callout tone="warn" icon={<Eye />} title="File watching is not running on this server">It can’t be switched on until the server supports it.</Callout>}
        {data.loading ? <SkeletonRows rows={2} /> : libraries.length === 0 ? <EmptyState compact icon={Eye} title="No libraries yet" message="Create a library first." />
          : <div className="console-table-responsive"><Table className="console-stack-table"><TableHeader><TableRow><TableHead>Library</TableHead><TableHead className="console-num-cell">Folders</TableHead><TableHead>Status</TableHead><TableHead>Watch</TableHead></TableRow></TableHeader>
            <TableBody>{libraries.map(l => <TableRow key={l.id}>
              <TableCell><strong>{l.name}</strong>{l.watchEnabled && l.watch?.error && <span className="console-row__meta console-error-text"> {l.watch.error}</span>}</TableCell>
              <TableCell className="console-num-cell">{l.folders}</TableCell>
              <TableCell><WatchStatus library={l} /></TableCell>
              <TableCell><Switch checked={l.watchEnabled} disabled={busyId === `w${l.id}` || !data.data.watcherRunning || !l.folders} onCheckedChange={() => toggleWatch(l)} aria-label={`Watch ${l.name} for new files`} /></TableCell>
            </TableRow>)}</TableBody></Table></div>}
      </Panel>
    </>}
    <ScheduleDialog open={creating} onOpenChange={setCreating} libraries={libraries} timezone={data.data?.timezone} onSaved={data.reload} />
  </PageStack>;
}
