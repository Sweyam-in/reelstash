import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { Button, Input, Switch } from '@spatika/react';
import { ArrowUpCircle, Plus, Puzzle, ScrollText, Settings } from 'lucide-react';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@spatika/react';
import {
  checkPluginUpdate, getPluginLog, getPlugins, installPluginFromFile, installPluginFromUrl, removePlugin, restartPlugin, savePluginConfig, setPluginEnabled, setPluginUpdateSource, updatePlugin,
} from '../../api';
import { Callout, CopyButton, DetailDrawer, DrawerSection, EmptyState, ErrorState, Field, FormDialog, PageHeader, PageStack, Panel, SkeletonRows, Status, Tag, rowActivation, useConfirm } from '../components';
import { describeError } from '../lib/errors';
import { useNotify } from '../lib/notify';
import { useResource } from '../lib/useResource';
import { useUrlState } from '../lib/useUrlState';

const KIND_LABEL = { provider: 'Metadata provider', auth: 'Sign-in method' };
const STATUS = {
  running: { label: 'Running', tone: 'ok' }, starting: { label: 'Starting…', tone: 'neutral' }, stopped: { label: 'Off', tone: 'neutral' },
  'needs-config': { label: 'Needs settings', tone: 'warn' }, failed: { label: 'Not working', tone: 'crit' },
};

/** What an install or update did, as a toast. */
export function installSummary(plugin) {
  if (!plugin.previousVersion) return { title: `${plugin.name} installed`, description: plugin.status === 'needs-config' || plugin.missing?.length ? 'Fill in its settings to start it.' : 'It is starting now.' };
  const same = plugin.previousVersion === plugin.version;
  return { title: same ? `${plugin.name} reinstalled` : `${plugin.name} updated`, description: same ? `Version ${plugin.version}. Its settings were kept.` : `${plugin.previousVersion} → ${plugin.version}. Its settings were kept and it is restarting.` };
}

// A plugin's update state: checked quietly when shown if it has an update source.
function usePluginUpdate(plugin, onChanged) {
  const notify = useNotify();
  const confirm = useConfirm();
  const [found, setFound] = useState(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (!plugin.updateUrl) { setFound(null); return undefined; }
    let live = true;
    checkPluginUpdate(plugin.id).then(res => { if (live) setFound(res); }).catch(() => {});
    return () => { live = false; };
  }, [plugin.id, plugin.updateUrl, plugin.version]);

  async function check() {
    setBusy(true);
    try { const res = await checkPluginUpdate(plugin.id); setFound(res); notify.success(res.available ? `${plugin.name} ${res.latest} is available` : `${plugin.name} is up to date`, res.available ? `You have ${res.current}.` : `Version ${res.current} is the latest.`); }
    catch (err) { notify.error(`check ${plugin.name} for updates`, err); }
    finally { setBusy(false); }
  }
  async function apply() {
    const ok = await confirm({ title: `Update ${plugin.name} to ${found.latest}?`, consequence: `Downloads the new version, replaces the running plugin and restarts it. ${plugin.name} is unavailable for a few seconds. Its settings are kept.`, affects: plugin.kind === 'auth' ? 'People signing in with this plugin' : 'Libraries that use this provider', reversible: 'You can install the previous .zip again.', confirmLabel: `Update to ${found.latest}` });
    if (!ok) return;
    setBusy(true);
    try { const result = await updatePlugin(plugin.id); const s = installSummary(result); notify.success(s.title, s.description); setFound(null); onChanged(); }
    catch (err) { notify.error(`update ${plugin.name}`, err); }
    finally { setBusy(false); }
  }
  return { found, busy, check, apply };
}

function UpdateSection({ plugin, onChanged }) {
  const notify = useNotify();
  const { found, busy: updating, check, apply } = usePluginUpdate(plugin, onChanged);
  const [source, setSource] = useState('');
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  async function saveSource(event) {
    event.preventDefault();
    setSaving(true);
    try { await setPluginUpdateSource(plugin.id, source.trim()); notify.success(source.trim() ? 'Update source saved' : 'Update source cleared', source.trim() ? 'Checking it for updates.' : 'The address in the plugin itself is used, if it has one.'); setEditing(false); onChanged(); }
    catch (err) { notify.error('save the update source', err); }
    finally { setSaving(false); }
  }
  return <>
    <div className="console-actions-row">
      {found?.available && <Status tone="ok">Update {found.latest} available</Status>}
      {found?.available ? <Button size="sm" disabled={updating || saving} onClick={apply} leadingIcon={<ArrowUpCircle size={14} />}>Update to {found.latest}</Button>
        : <Button size="sm" variant="outline" disabled={updating || saving || !plugin.updateUrl} onClick={check}>Check for updates</Button>}
      <Button size="sm" variant="ghost" onClick={() => { setSource(plugin.updateUrl || ''); setEditing(v => !v); }}>{editing ? 'Cancel' : 'Update source'}</Button>
    </div>
    {!plugin.updateUrl && !editing && <p className="console-field__hint">No update source yet. Add the address of the plugin’s <code>index.json</code>, or install a newer .zip with Add plugin.</p>}
    {editing && <form className="console-form" onSubmit={saveSource}>
      <Field label="Address of index.json">{props => <Input {...props} type="url" placeholder="https://example.com/dist/index.json" value={source} onChange={e => setSource(e.target.value)} />}</Field>
      <div><Button type="submit" size="sm" loading={saving} disabled={source.trim() === (plugin.updateUrl || '')}>Save</Button></div>
    </form>}
  </>;
}

function PluginLog({ id }) {
  const notify = useNotify();
  const [lines, setLines] = useState(null);
  const load = useCallback(() => getPluginLog(id).then(r => setLines(r.lines || [])).catch(err => notify.error('load the log', err)), [id]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { load(); }, [load]);
  return <><pre className="console-pre" aria-label="Plugin log">{lines === null ? 'Loading…' : lines.length ? lines.join('\n') : 'Nothing printed yet.'}</pre><div><Button size="sm" variant="outline" onClick={load}>Refresh log</Button></div></>;
}

function PluginDrawer({ plugin, onChanged, onClose }) {
  const notify = useNotify();
  const confirm = useConfirm();
  const [drafts, setDrafts] = useState({});
  const [busy, setBusy] = useState(false);
  const [showLog, setShowLog] = useState(false);
  const [error, setError] = useState(null);
  useEffect(() => { setDrafts({}); setError(null); }, [plugin?.id]);
  if (!plugin) return null;
  const dirty = Object.values(drafts).some(v => v !== undefined && v !== '');
  const status = STATUS[plugin.status] || STATUS.stopped;
  const missingLabels = plugin.fields.filter(f => plugin.missing.includes(f.key)).map(f => f.label);

  async function run(action, success, failure) {
    setBusy(true); setError(null);
    try { await action(); if (success) notify.success(success); onChanged(); return true; }
    catch (err) { setError(describeError(failure, err)); return false; }
    finally { setBusy(false); }
  }
  const save = () => run(async () => { await savePluginConfig(plugin.id, Object.fromEntries(Object.entries(drafts).filter(([, v]) => v !== undefined && v !== ''))); setDrafts({}); }, `${plugin.name} saved and restarted`, `save ${plugin.name}`);
  const restart = () => run(() => restartPlugin(plugin.id), `${plugin.name} restarted`, `restart ${plugin.name}`);
  async function remove() {
    const ok = await confirm({ title: `Uninstall ${plugin.name}?`, consequence: 'Stops the plugin and deletes its files and saved settings.', affects: plugin.kind === 'auth' ? 'Anyone who signs in with this plugin' : 'Libraries that match titles with this provider', reversible: 'You can install it again, but you would have to enter its settings again.', confirmLabel: 'Uninstall', tone: 'danger', typeToConfirm: plugin.name, reason: { label: 'Reason', placeholder: 'Why are you doing this? Recorded in the audit log.', required: false } });
    if (ok && await run(() => removePlugin(plugin.id, { reason: ok.reason }), `${plugin.name} uninstalled`, `uninstall ${plugin.name}`)) onClose();
  }

  return <DetailDrawer open onClose={onClose} eyebrow={KIND_LABEL[plugin.kind] || plugin.kind} title={plugin.name} description={`Version ${plugin.version}${plugin.description ? ` · ${plugin.description}` : ''}`}
    footer={plugin.fields.length > 0 ? <><Button variant="ghost" onClick={onClose}>Close</Button><Button type="submit" form={`plugin-form-${plugin.id}`} disabled={!dirty} loading={busy}>Save and restart</Button></> : <Button variant="ghost" onClick={onClose}>Close</Button>}>
    <DrawerSection title="Status">
      <Status tone={status.tone}>{status.label}</Status>
      {plugin.status === 'needs-config' && missingLabels.length > 0 && <p className="console-field__hint">Fill in {missingLabels.join(', ')} to start it.</p>}
      {plugin.error && <p className="console-field__error" role="alert">{plugin.error}</p>}
      {error && <p className="console-field__error" role="alert">{error}</p>}
      {plugin.kind === 'auth' && plugin.callbackUrl && <p className="console-field__hint">Register this redirect address with your identity provider: <CopyButton value={plugin.callbackUrl} label="Redirect address" /></p>}
      {plugin.kind === 'provider' && <p className="console-field__hint">Choose which libraries use it on the <Link className="console-link" to="/admin/metadata/providers">Providers</Link> page.</p>}
    </DrawerSection>
    <DrawerSection title="Settings">
      {plugin.fields.length > 0
        ? <form id={`plugin-form-${plugin.id}`} className="console-form" noValidate onSubmit={e => { e.preventDefault(); if (dirty) save(); }}>
          {plugin.fields.map(field => <Field key={field.key} label={<>{field.label}{field.required ? ' *' : ''}</>} hint={field.set && field.secret ? `Saved (${field.value}). Leave empty to keep it.` : undefined}>{props => <Input {...props} type={field.secret ? 'password' : 'text'} autoComplete="off"
            placeholder={field.secret ? (field.set ? 'Enter a new value to replace it' : field.placeholder || '') : (field.value || field.default || field.placeholder || '')} value={drafts[field.key] ?? ''} onChange={e => setDrafts(d => ({ ...d, [field.key]: e.target.value }))} />}</Field>)}
        </form> : <p className="console-muted console-small">This plugin has no settings.</p>}
    </DrawerSection>
    <DrawerSection title="Updates"><UpdateSection plugin={plugin} onChanged={onChanged} /></DrawerSection>
    <DrawerSection title="Maintenance">
      <div className="console-actions-row">
        <Button size="sm" variant="outline" disabled={busy || !plugin.enabled} onClick={restart}>Restart</Button>
        <Button size="sm" variant="outline" onClick={() => setShowLog(v => !v)} leadingIcon={<ScrollText size={14} />}>{showLog ? 'Hide log' : 'Show log'}</Button>
        <Button size="sm" variant="destructive" disabled={busy} onClick={remove}>Uninstall…</Button>
      </div>
      {showLog && <PluginLog id={plugin.id} />}
    </DrawerSection>
  </DetailDrawer>;
}

function InstallDialog({ data, open, onOpenChange, onInstalled }) {
  const notify = useNotify();
  const [url, setUrl] = useState('');
  const [file, setFile] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const input = useRef(null);
  useEffect(() => { if (open) { setUrl(''); setFile(null); setError(null); } }, [open]);
  const source = file ? 'file' : url.trim() ? 'url' : null;
  const problem = !source ? 'Choose a .zip file or enter an address.' : file && file.size === 0 ? 'That file is empty.' : null;

  async function install() {
    setBusy(true); setError(null);
    try { const plugin = await (file ? installPluginFromFile(file) : installPluginFromUrl(url.trim())); const s = installSummary(plugin); notify.success(s.title, s.description); onOpenChange(false); onInstalled(); }
    catch (err) { setError(describeError('install the plugin', err)); }
    finally { setBusy(false); }
  }

  return <FormDialog open={open} onOpenChange={onOpenChange} title="Add plugin" description="A bundle is a .zip with a plugin.json and the script it runs. See docs/plugin-bundles.md." submitLabel="Install plugin" dirty={!!source} canSubmit={!problem && data.enabled} submitting={busy} error={error} onSubmit={install}>
    <Callout tone="warn" icon={<Puzzle />} title="A plugin is a program that runs on this server">Only install bundles from people you trust. It can read what the server can read.</Callout>
    <Field label="From a file" hint={file ? `${file.name} selected` : 'A plugin .zip from your computer.'}>{props => <input {...props} ref={input} type="file" accept=".zip,application/zip" disabled={busy || !data.enabled} onChange={e => { setFile(e.target.files?.[0] || null); if (e.target.files?.[0]) setUrl(''); }} />}</Field>
    <Field label="Or from an address" error={problem}>{props => <Input {...props} type="url" placeholder="https://example.com/my-plugin.zip" value={url} disabled={!data.enabled || !!file} onChange={e => setUrl(e.target.value)} />}</Field>
  </FormDialog>;
}

function PluginRow({ plugin, busy, onToggle, onOpen, onChanged }) {
  const status = STATUS[plugin.status] || STATUS.stopped;
  const { found, busy: updating, apply } = usePluginUpdate(plugin, onChanged);
  return <TableRow data-plugin={plugin.id} {...rowActivation(() => onOpen(plugin.id))} aria-label={`${plugin.name}, open settings`}>
    <TableCell className="console-wrap"><strong>{plugin.name}</strong>{plugin.description && <div className="console-row__meta">{plugin.description}</div>}</TableCell>
    <TableCell>{KIND_LABEL[plugin.kind] || plugin.kind}</TableCell>
    <TableCell className="console-nowrap">{plugin.version}{found?.available && <span className="console-row__meta"> → {found.latest} available</span>}</TableCell>
    <TableCell className="console-wrap"><Status tone={status.tone}>{status.label}</Status>{plugin.error && <div className="console-field__error" role="alert">{plugin.error}</div>}</TableCell>
    <TableCell><Switch checked={plugin.enabled} disabled={busy} onCheckedChange={v => onToggle(plugin, v)} aria-label={`Turn ${plugin.name} on`} /></TableCell>
    <TableCell><span className="console-nowrap">
      {found?.available && <Button size="sm" disabled={updating} onClick={apply} aria-label={`Update ${plugin.name} to ${found.latest}`} leadingIcon={<ArrowUpCircle size={14} />}>Update</Button>}
      <Button size="sm" variant="ghost" onClick={() => onOpen(plugin.id)} aria-label={`${plugin.name} settings`} leadingIcon={<Settings size={14} />}>Settings</Button>
    </span></TableCell>
  </TableRow>;
}

/** Admin → Plugins: install, set up, switch off and remove plugin bundles. */
export function PluginsPage() {
  const notify = useNotify();
  const confirm = useConfirm();
  const [filters, setFilters] = useUrlState({ plugin: '' });
  const plugins = useResource(() => getPlugins(), []);
  const [adding, setAdding] = useState(false);
  const [busyId, setBusyId] = useState(null);
  const data = plugins.data;
  const items = data?.items || [];
  // A starting plugin is checked again until it settles.
  const settling = items.some(p => p.enabled && p.status === 'starting');
  const { reload } = plugins;
  useEffect(() => {
    if (!settling) return undefined;
    const t = setInterval(reload, 2000);
    return () => clearInterval(t);
  }, [settling, reload]);

  async function toggle(plugin, enabled) {
    if (!enabled && plugin.kind === 'auth') {
      const ok = await confirm({ title: `Switch off ${plugin.name}?`, consequence: `People who sign in with ${plugin.name} can no longer sign in until you switch it back on.`, affects: 'Everyone using this sign-in method', reversible: true, confirmLabel: 'Switch off', tone: 'danger' });
      if (!ok) return;
    }
    setBusyId(plugin.id);
    try { await setPluginEnabled(plugin.id, enabled); notify.success(enabled ? `${plugin.name} switched on` : `${plugin.name} switched off`); await reload(); }
    catch (err) { notify.error(`change ${plugin.name}`, err); }
    finally { setBusyId(null); }
  }
  const openPlugin = items.find(p => p.id === filters.plugin);

  return <PageStack>
    <PageHeader title="Plugins" subtitle="Add metadata sources and sign-in methods to ReelStash. ReelStash starts each plugin itself and keeps it running."
      actions={<Button onClick={() => setAdding(true)} disabled={!data} leadingIcon={<Plus size={14} />}>Add plugin</Button>} />
    {data && !data.enabled && <Callout tone="warn" icon={<Puzzle />} title="Plugin bundles are switched off on this server">Set <code>REELSTASH_PLUGIN_BUNDLES</code> to true and restart the server to use them.</Callout>}
    <Panel title="Installed" bodyless>
      {plugins.error && !data ? <ErrorState title="Couldn't load plugins" error={plugins.error} onRetry={plugins.reload} />
        : plugins.loading ? <SkeletonRows rows={3} />
          : items.length === 0 ? <EmptyState icon={Puzzle} title="Nothing installed yet" message="Add a plugin to get another metadata source or sign-in method." action={data?.enabled && <Button size="sm" onClick={() => setAdding(true)}>Add plugin</Button>} />
            : <div className="console-table-responsive"><Table aria-label="Installed plugins" className="console-stack-table">
              <TableHeader><TableRow><TableHead>Plugin</TableHead><TableHead>Type</TableHead><TableHead>Version</TableHead><TableHead>Status</TableHead><TableHead>On</TableHead><TableHead><span className="console-sr-only">Actions</span></TableHead></TableRow></TableHeader>
              <TableBody>{items.map(plugin => <PluginRow key={plugin.id} plugin={plugin} busy={busyId === plugin.id} onToggle={toggle} onOpen={id => setFilters({ plugin: id })} onChanged={reload} />)}</TableBody>
            </Table></div>}
    </Panel>
    {data && <InstallDialog data={data} open={adding} onOpenChange={setAdding} onInstalled={reload} />}
    {filters.plugin && openPlugin && <PluginDrawer plugin={openPlugin} onChanged={reload} onClose={() => setFilters({ plugin: '' })} />}
  </PageStack>;
}
