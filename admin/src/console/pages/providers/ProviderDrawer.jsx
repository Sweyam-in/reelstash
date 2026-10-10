import React, { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { Button, Checkbox, Input } from '@spatika/react';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@spatika/react';
import {
  clearExternalProviderLog, clearProviderConfig, clearSubtitleProvider, getExternalProviderLog, refreshExternalProvider, removeExternalProvider, saveProviderConfig, saveSubtitleProvider,
  setExternalProviderEnabled, testProvider, testSubtitleProvider,
} from '../../../api';
import { DetailDrawer, DrawerSection, Field, SegmentedTabs, Status, useConfirm } from '../../components';
import { timeAgo } from '../../lib/format';
import { describeError } from '../../lib/errors';
import { useNotify } from '../../lib/notify';

const KIND_LABEL = { media: 'titles', performer: 'performers', studio: 'studios' };

export function providerStatus(provider) {
  if (provider.canDownload) return { label: 'Connected', tone: 'ok' };
  if (provider.enabled && provider.canDownload === false) return { label: 'Search only', tone: 'warn' };
  if (provider.enabled) return { label: 'Connected', tone: 'ok' };
  if (provider.switchedOff) return { label: 'Switched off', tone: 'neutral' };
  return { label: 'Not configured', tone: 'neutral' };
}

// A provider's fields: text and secret inputs first, then checkboxes. Secrets are never shown, only a
// masked preview of the saved value; an empty input keeps what is saved.
function Fields({ idPrefix, fields, drafts, setDrafts }) {
  const ordered = [...fields.filter(f => f.type !== 'bool'), ...fields.filter(f => f.type === 'bool')];
  return <>
    {ordered.map(field => field.type === 'bool'
      ? <label key={field.key} className="console-inline-check"><Checkbox checked={(drafts[field.key] ?? field.value) === 'true'} onCheckedChange={on => setDrafts(d => ({ ...d, [field.key]: on ? 'true' : 'false' }))} /> {field.label}</label>
      : <Field key={field.key} label={field.label} hint={field.set && field.secret ? `Saved (${field.value}). Leave empty to keep it.` : undefined}>{props => <Input {...props} id={`${idPrefix}-${field.key}`} type={field.secret ? 'password' : 'text'} autoComplete="off"
        placeholder={field.secret ? (field.set ? 'Enter a new value to replace it' : field.placeholder || '') : (field.value || field.placeholder || '')} value={drafts[field.key] ?? ''} onChange={e => setDrafts(d => ({ ...d, [field.key]: e.target.value }))} />}</Field>)}
  </>;
}

// The latest requests ReelStash made to a plugin, newest first.
function RequestLog({ provider }) {
  const notify = useNotify();
  const [calls, setCalls] = useState(null);
  const load = useCallback(() => getExternalProviderLog(provider.id).then(r => setCalls(r.items || [])).catch(err => notify.error('load the request log', err)), [provider.id]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { load(); }, [load]);
  async function clear() {
    try { await clearExternalProviderLog(provider.id); setCalls([]); }
    catch (err) { notify.error('clear the request log', err); }
  }
  if (calls === null) return <p className="console-muted console-small">Loading…</p>;
  return <>
    {calls.length === 0 ? <p className="console-muted console-small">No requests yet. They appear here when ReelStash searches this plugin.</p>
      : <div className="console-table-responsive"><Table aria-label={`${provider.name} requests`}><TableHeader><TableRow><TableHead>When</TableHead><TableHead>Request</TableHead><TableHead>Result</TableHead><TableHead className="console-num-cell">Time</TableHead></TableRow></TableHeader>
        <TableBody>{calls.map((call, i) => <TableRow key={`${call.at}-${i}`}>
          <TableCell className="console-nowrap" title={call.at}>{timeAgo(call.at)}</TableCell>
          <TableCell><code className="console-code">{call.method} {call.path}</code></TableCell>
          <TableCell className="console-wrap">{call.error ? <Status tone="crit">{call.status ? `HTTP ${call.status}` : 'No answer'}: {call.error}</Status> : `HTTP ${call.status}`}</TableCell>
          <TableCell className="console-num-cell">{call.durationMs} ms</TableCell>
        </TableRow>)}</TableBody></Table></div>}
    <div className="console-actions-row"><Button size="sm" variant="outline" onClick={load}>Refresh</Button><Button size="sm" variant="ghost" disabled={calls.length === 0} onClick={clear}>Clear log</Button></div>
  </>;
}

/**
 * One provider's settings in a right-side drawer. `subtitle` switches it to the OpenSubtitles
 * connection (its own endpoints); everything else is a metadata provider.
 */
export function ProviderDrawer({ provider, usedBy = [], subtitle = false, onChanged, onClose }) {
  const notify = useNotify();
  const confirm = useConfirm();
  const [drafts, setDrafts] = useState({});
  const [tab, setTab] = useState('settings');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  useEffect(() => { setDrafts({}); setTab('settings'); setError(null); }, [provider?.id]);
  if (!provider) return null;
  const dirty = Object.values(drafts).some(v => v !== undefined && v !== '');
  const status = providerStatus(provider);
  const kinds = (provider.kinds || []).map(k => KIND_LABEL[k] || k).join(', ');
  const usage = subtitle ? '' : usedBy.length ? `Used by ${usedBy.map(l => l.name).join(', ')}.` : 'Not used by any library.';
  const formId = `provider-form-${provider.id}`;

  async function run(fn, failure) {
    setBusy(true); setError(null);
    try { await fn(); } catch (err) { setError(describeError(failure, err)); } finally { setBusy(false); }
  }
  const values = () => Object.fromEntries(Object.entries(drafts).filter(([, v]) => v !== '' && v !== undefined));
  const save = () => run(async () => {
    if (subtitle) await saveSubtitleProvider(values()); else await saveProviderConfig(provider.id, values());
    setDrafts({}); notify.success(`${provider.name} saved`, 'Active immediately, no restart needed.'); onChanged();
  }, `save ${provider.name}`);
  const test = () => run(async () => {
    const result = subtitle ? await testSubtitleProvider() : await testProvider(provider.id);
    if (result.ok) {
      const a = result.account;
      notify.success(`${provider.name} is reachable`, subtitle ? (a ? `Signed in${a.level ? ` as ${a.level}` : ''}. ${a.remainingDownloads} of ${a.allowedDownloads} downloads left today.` : 'The API key works. Add a username and password to download.') : 'The saved settings work.');
    } else setError(`${provider.name} test failed. ${result.error}`);
  }, `test ${provider.name}`);
  async function clear() {
    const ok = await confirm({ title: `Clear ${provider.name}?`, consequence: subtitle ? 'Its settings are cleared and subtitles can’t be searched or downloaded. Subtitle files already saved stay where they are.' : 'Its saved settings are cleared and it stops being used for matching. Titles it matched keep their links.', affects: usage || 'Subtitle search in title settings', reversible: 'You can enter the settings again.', confirmLabel: 'Clear settings', tone: 'danger' });
    if (!ok) return;
    await run(async () => { if (subtitle) await clearSubtitleProvider(); else await clearProviderConfig(provider.id); setDrafts({}); notify.success(`${provider.name} disabled`, 'Its settings were cleared.'); onChanged(); }, `clear ${provider.name}`);
  }
  const refresh = () => run(async () => { await refreshExternalProvider(provider.id); notify.success(`${provider.name} refreshed`, 'Its manifest was read again.'); onChanged(); }, `refresh ${provider.name}`);
  async function remove() {
    const ok = await confirm({ title: `Remove ${provider.name}?`, consequence: 'Titles it matched keep their links, but it is no longer searched.', affects: usage, reversible: 'You can connect it again by its address.', confirmLabel: 'Remove plugin', tone: 'danger', typeToConfirm: provider.name });
    if (!ok) return;
    await run(async () => { await removeExternalProvider(provider.id); notify.success(`${provider.name} removed`); onClose(); onChanged(); }, `remove ${provider.name}`);
  }

  const form = <form id={formId} className="console-form" noValidate onSubmit={e => { e.preventDefault(); if (dirty) save(); }}>
    {provider.notice && <p className="console-field__hint">{provider.notice}</p>}
    {provider.bundle && <p className="console-field__hint">Runs from an installed bundle. Its restart and uninstall are on the <Link className="console-link" to="/admin/plugins">Plugins</Link> page.</p>}
    {provider.external && !provider.bundle && <p className="console-field__hint">Plugin at <code className="console-code">{provider.baseUrl}</code></p>}
    {provider.fields?.length ? <Fields idPrefix={provider.id} fields={provider.fields} drafts={drafts} setDrafts={setDrafts} /> : <p className="console-muted console-small">This provider has no settings.</p>}
  </form>;

  return <DetailDrawer open onClose={onClose} eyebrow={subtitle ? 'Subtitles' : provider.external ? 'Plugin' : 'Metadata provider'} title={provider.name}
    description={subtitle ? 'Searches subtitles by each file’s fingerprint and title. Get an API key at opensubtitles.com under your profile’s API consumers; downloading also needs the account’s username and password.' : `Matches ${kinds}. ${usage} Settings take effect immediately.`}
    footer={<>
      {(provider.external && !provider.bundle) ? <Button variant="destructive" size="sm" disabled={busy} onClick={remove}>Remove…</Button> : provider.enabled && <Button variant="outline" size="sm" disabled={busy} onClick={clear}>Clear settings…</Button>}
      <Button variant="outline" size="sm" disabled={!provider.enabled || busy} onClick={test}>Test connection</Button>
      <Button type="submit" form={formId} size="sm" disabled={!dirty} loading={busy}>Save</Button>
    </>}>
    <DrawerSection title="Status"><Status tone={status.tone}>{status.label}</Status>{error && <p className="console-field__error" role="alert">{error}</p>}</DrawerSection>
    {provider.external && !subtitle
      ? <><SegmentedTabs label="Plugin sections" value={tab} onChange={setTab} options={[{ value: 'settings', label: 'Settings' }, { value: 'log', label: 'Request log' }]} />
        <DrawerSection title={tab === 'settings' ? 'Settings' : 'Requests'}>{tab === 'settings' ? <>{form}{!provider.bundle && <div className="console-actions-row"><Button size="sm" variant="outline" disabled={busy} onClick={refresh}>Refresh manifest</Button></div>}</> : <RequestLog provider={provider} />}</DrawerSection></>
      : <DrawerSection title="Settings">{form}</DrawerSection>}
  </DetailDrawer>;
}

export { setExternalProviderEnabled };
