import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { Button, Checkbox, Input } from '@spatika/react';
import { AlertTriangle, BrushCleaning, ChevronDown, ChevronRight, CircleStop, Database, FolderOpen, HardDrive, ScanSearch } from 'lucide-react';
import { cancelJob, getHousekeeping, runHousekeeping, updateHousekeepingSettings } from '../../api';
import { useServerEvent } from '../../events';
import { GROUPS, RETENTION, findingsFor, selectionTotals } from '../../pages/housekeepingModel';
import { Callout, EmptyState, ErrorState, Field, MetricStrip, PageHeader, PageStack, Panel, ProgressBar, SectionTabs, SettingsGroup, SettingsRow, SkeletonRows, Status, Tag, useConfirm } from '../components';
import { formatBytes, formatNumber, plural, timeAgo } from '../lib/format';
import { tabsOf } from '../lib/navigation';
import { useNotify } from '../lib/notify';
import { useResource } from '../lib/useResource';
import { automationTabs } from './AutomationPage';

const GROUP_ICON = { files: HardDrive, database: Database, library: FolderOpen };

function Finding({ finding }) {
  if (!finding) return <span className="console-muted console-small">Not analyzed yet</span>;
  if (finding.error) return <Status tone="crit">Failed: {finding.error}</Status>;
  const verb = finding.mode === 'cleanup' ? 'Removed' : 'Found';
  const failed = finding.failed ? <span className="console-error-text console-small"> · {finding.failed} could not be removed</span> : null;
  const skipped = finding.skipped?.length ? <span className="console-muted console-small"> · {plural(finding.skipped.length, 'place')} skipped</span> : null;
  if (finding.task === 'vacuum') return <span className="console-num">{finding.samples?.[0] || 'Checked'}{failed}</span>;
  if (!finding.count) return <span><Status tone="ok">{finding.mode === 'cleanup' ? 'Nothing to remove' : 'Clean'}</Status>{skipped}</span>;
  return <span className="console-num">{verb} {plural(finding.count, 'item')}{finding.bytes > 0 && ` · ${formatBytes(finding.bytes)}`}{skipped}{failed}</span>;
}

function TaskRow({ task, finding, checked, onToggle, open, onOpen, disabled }) {
  const details = finding && (finding.samples?.length || finding.skipped?.length) && task.id !== 'vacuum';
  const more = finding ? finding.count - (finding.samples?.length || 0) : 0;
  return <li className="console-task">
    <div className="console-task__row">
      <Checkbox id={`hk-${task.id}`} checked={checked} onCheckedChange={() => onToggle(task.id)} disabled={disabled} aria-describedby={`hk-desc-${task.id}`} />
      <label htmlFor={`hk-${task.id}`} className="console-task__copy">
        <strong>{task.title}{!task.default && <Tag>Opt-in</Tag>}</strong>
        <span id={`hk-desc-${task.id}`} className="console-row__meta">{task.description}</span>
      </label>
      <div className="console-task__finding"><Finding finding={finding} /></div>
      {details ? <Button variant="ghost" size="sm" aria-expanded={open} onClick={() => onOpen(open ? null : task.id)} leadingIcon={open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}>Details</Button> : <span className="console-task__spacer" />}
    </div>
    {open && details && <div className="console-task__details">
      {finding.skipped?.map(reason => <p key={reason} className="console-row__meta"><AlertTriangle size={12} aria-hidden="true" /> Skipped {reason}</p>)}
      {!!finding.samples?.length && <ul className="console-plain-list">{finding.samples.map(s => <li key={s}><code className="console-code">{s}</code></li>)}</ul>}
      {more > 0 && <p className="console-row__meta">and {formatNumber(more)} more</p>}
    </div>}
  </li>;
}

function RetentionForm({ settings, defaults, onSaved }) {
  const notify = useNotify();
  const [form, setForm] = useState(settings);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
  useEffect(() => { setForm(settings); }, [settings]);
  const dirty = RETENTION.some(([key]) => Number(form[key]) !== settings[key]);
  const invalid = key => { const n = Number(form[key]); return Number.isInteger(n) && n >= 1 && n <= 3650 ? null : 'Enter a whole number of days from 1 to 3650.'; };
  const bad = RETENTION.some(([key]) => invalid(key));

  async function save(event) {
    event.preventDefault();
    if (bad) { setError('Fix the highlighted fields first.'); return; }
    setSaving(true); setError(null);
    try { await updateHousekeepingSettings(Object.fromEntries(RETENTION.map(([key]) => [key, Number(form[key])]))); notify.success('Retention saved', 'It applies from the next housekeeping run.'); onSaved(); }
    catch (err) { setError(`Couldn’t save retention. ${err.message}. What you typed is still here.`); }
    finally { setSaving(false); }
  }

  return <form onSubmit={save} noValidate>
    <SettingsGroup title="Retention" description="How long history is kept, and how long a cached copy may sit unused, before housekeeping removes it."
      footer={<><span className={error ? 'console-error-text' : undefined} role={error ? 'alert' : undefined}>{error || (dirty ? 'Unsaved changes' : 'Saved')}</span><Button type="submit" size="sm" disabled={!dirty} loading={saving}>Save retention</Button></>}>
      {RETENTION.map(([key, label, hint]) => <SettingsRow key={key} label={label} description={`${hint}${defaults?.[key] ? ` Default ${defaults[key]} days.` : ''}`} htmlFor={`ret-${key}`}>
        <span className="console-days"><Input id={`ret-${key}`} type="number" min={1} max={3650} value={form[key] ?? ''} aria-invalid={dirty && invalid(key) ? true : undefined} onChange={e => setForm(f => ({ ...f, [key]: e.target.value }))} /> days</span>
      </SettingsRow>)}
    </SettingsGroup>
  </form>;
}

export function HousekeepingPage() {
  const notify = useNotify();
  const confirm = useConfirm();
  const data = useResource(() => getHousekeeping(), []);
  const [selected, setSelected] = useState(null);
  const [open, setOpen] = useState(null);
  const [busy, setBusy] = useState(false);
  const activeRef = useRef(null);
  const { reload } = data;
  const d = data.data;

  useEffect(() => { if (d && !selected) setSelected(new Set((d.tasks || []).filter(t => t.default).map(t => t.id))); }, [d, selected]);
  useEffect(() => {
    if (!d) return;
    if (activeRef.current && !d.active) {
      const done = activeRef.current;
      const finished = done.payload?.dryRun ? d.lastAnalyze : d.lastCleanup;
      if (finished?.id === done.id) notify.success(done.payload?.dryRun ? 'Analysis finished' : 'Clean-up finished');
    }
    activeRef.current = d.active || null;
  }, [d]); // eslint-disable-line react-hooks/exhaustive-deps
  // Follow a running job: the jobs stream for prompt updates, a poll as the fallback.
  useServerEvent('jobs', snapshot => { if (activeRef.current || (snapshot.items || []).some(j => j.kind === 'HOUSEKEEPING' && (j.status === 'PENDING' || j.status === 'RUNNING'))) reload(); });
  const active = d?.active;
  useEffect(() => {
    if (!active) return undefined;
    const t = setInterval(reload, 2000);
    return () => clearInterval(t);
  }, [active?.id, reload]); // eslint-disable-line react-hooks/exhaustive-deps

  const tasks = d?.tasks || [];
  const { findings, analyzedAt } = useMemo(() => findingsFor(d), [d]);
  const totals = useMemo(() => selectionTotals(selected, findings), [selected, findings]);
  const storageBytes = (d?.storage || []).reduce((s, x) => s + (x.bytes || 0), 0);
  const storageFiles = (d?.storage || []).reduce((s, x) => s + (x.files || 0), 0);
  const cleanup = d?.lastCleanup?.result;
  const libraryChosen = tasks.some(t => t.group === 'library' && selected?.has(t.id));
  const toggle = id => setSelected(prev => { const next = new Set(prev); if (next.has(id)) next.delete(id); else next.add(id); return next; });

  async function start(dryRun) {
    let reason;
    if (!dryRun) {
      const ok = await confirm({
        title: `Clean up ${plural(selected.size, 'task')}?`,
        consequence: `${analyzedAt ? `The last analysis found ${plural(totals.count, 'item')} (${formatBytes(totals.bytes)}) still to remove in these tasks. ` : 'These tasks have not been analyzed yet, so the amount is unknown. '}Removed files and records can’t be restored.${libraryChosen ? ' This includes files in your library folders, which are deleted permanently.' : ''}`,
        affects: libraryChosen ? 'ReelStash data and files in your library folders' : 'ReelStash’s own generated files and records', reversible: false, confirmLabel: 'Clean up', tone: 'danger', typeToConfirm: libraryChosen ? 'delete files' : undefined, reason: { label: 'Reason', placeholder: 'Why are you doing this? Recorded in the audit log.', required: false },
      });
      if (!ok) return;
      reason = ok.reason;
    }
    setBusy(true);
    try {
      const result = await runHousekeeping(dryRun ? tasks.map(t => t.id) : [...selected], dryRun, { reason });
      activeRef.current = { id: result.jobId, payload: { dryRun } };
      notify.success(dryRun ? 'Analyzing' : 'Cleaning up', dryRun ? 'Nothing is removed during an analysis.' : `${plural(selected.size, 'task')} running in the background.`);
      await reload();
    } catch (err) { notify.error(dryRun ? 'start the analysis' : 'start the clean-up', err); }
    finally { setBusy(false); }
  }
  async function stop() {
    try { await cancelJob(active.id); await reload(); }
    catch (err) { notify.error('stop housekeeping', err); }
  }
  const progress = active && active.progressTotal > 0 ? Math.round((active.progressDone / active.progressTotal) * 100) : null;

  return <PageStack>
    <PageHeader title="Housekeeping" subtitle="Clear out what ReelStash leaves behind: unused images, stale caches, unfinished files and old history. Analyze first; nothing is deleted until you clean up."
      actions={<>
        <Button variant="outline" onClick={() => start(true)} disabled={!d || !!active || busy} leadingIcon={<ScanSearch size={14} />}>Analyze</Button>
        <Button onClick={() => start(false)} disabled={!d || !!active || busy || !selected?.size} leadingIcon={<BrushCleaning size={14} />}>Clean up</Button>
      </>} />
    <SectionTabs entries={automationTabs} activePath="/admin/housekeeping" label="Automation sections" />
    <p className="console-muted console-small">To run it regularly, add a schedule on the <Link className="console-link" to="/admin/automation">Automation</Link> page.</p>

    {data.error && !d ? <Panel bodyless><ErrorState title="Couldn't load housekeeping" error={data.error} onRetry={data.reload} /></Panel> : !d ? <Panel bodyless><SkeletonRows rows={6} /></Panel> : <>
      {active && <div className="console-banner" role="status">
        <Status tone="accent" pulse><span className="console-banner__title">{active.payload?.dryRun ? 'Analyzing' : 'Cleaning up'}{active.status === 'PENDING' ? ' · waiting in queue' : ''}</span></Status>
        <span className="console-banner__meta">{active.progressLabel || 'Starting'}{progress != null ? ` · ${active.progressDone} of ${active.progressTotal} tasks` : ''}</span>
        <Button size="sm" variant="outline" className="console-banner__link" onClick={stop} leadingIcon={<CircleStop size={14} />}>Stop</Button>
        <div className="console-banner__bar"><ProgressBar percent={progress} label="Housekeeping progress" /></div>
      </div>}
      <MetricStrip label="Housekeeping summary" items={[
        analyzedAt ? { label: 'Reclaimable', value: formatBytes(totals.bytes), detail: `${plural(totals.count, 'item')} in selected tasks · analyzed ${timeAgo(analyzedAt)}` } : { label: 'Reclaimable', value: '—', detail: 'Run Analyze to measure' },
        { label: 'Last clean-up', value: d.lastCleanup ? timeAgo(d.lastCleanup.finishedAt) : 'Never', detail: d.lastCleanup?.status === 'FAILED' ? `Failed: ${d.lastCleanup.error}` : cleanup ? `${formatBytes(cleanup.bytes)} · ${plural(cleanup.count, 'item')} removed` : undefined, tone: d.lastCleanup?.status === 'FAILED' ? 'danger' : undefined },
        { label: 'Data folders', value: storageBytes ? formatBytes(storageBytes) : '—', detail: storageFiles ? `${formatNumber(storageFiles)} files` : 'Measuring…' },
        { label: 'Selected', value: formatNumber(selected?.size ?? 0), detail: `of ${tasks.length} tasks for the next clean-up` },
      ]} />
      {GROUPS.map(group => {
        const list = tasks.filter(t => t.group === group.id);
        if (!list.length) return null;
        const ids = list.map(t => t.id);
        const allOn = ids.every(id => selected?.has(id));
        const Icon = GROUP_ICON[group.id];
        return <Panel key={group.id} title={<span className="console-title-cell"><Icon size={15} aria-hidden="true" />{group.title}</span>} subtitle={group.description} bodyless
          actions={<Button variant="ghost" size="sm" disabled={!!active} onClick={() => setSelected(prev => { const next = new Set(prev); ids.forEach(id => (allOn ? next.delete(id) : next.add(id))); return next; })}>{allOn ? 'Clear all' : 'Select all'}</Button>}>
          {group.id === 'library' && <Callout tone="crit" icon={<AlertTriangle />} title="These delete files in your library folders, permanently">Check the analysis details first. Disks being scanned or organized are skipped.</Callout>}
          <ul className="console-rows" aria-label={group.title}>
            {list.map(task => <TaskRow key={task.id} task={task} finding={findings[task.id]} checked={!!selected?.has(task.id)} onToggle={toggle} open={open === task.id} onOpen={setOpen} disabled={!!active} />)}
          </ul>
        </Panel>;
      })}
      <RetentionForm settings={d.settings} defaults={d.defaults} onSaved={reload} />
      {!!d.storage?.length && <Panel title="Data folders" subtitle="Space ReelStash’s generated files take on the server. Sizes refresh every few minutes." bodyless>
        <ul className="console-rows" aria-label="Data folders">{d.storage.map(f => <li key={f.name} className="console-row">
          <div className="console-row__main"><span className="console-row__title">{f.name}</span><span className="console-row__meta"><code className="console-code">{f.path}</code></span></div>
          <div className="console-row__aside">{f.error ? <Status tone="crit">{f.error}</Status> : <span className="console-num">{formatBytes(f.bytes)} · {(f.files || 0).toLocaleString()} files</span>}</div>
        </li>)}</ul>
      </Panel>}
    </>}
  </PageStack>;
}
