import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { Button, Checkbox, Input, Switch } from '@spatika/react';
import { ArrowDown, ArrowUp, Building2, Clapperboard, Film, Link2, Lock, Plus, Users } from 'lucide-react';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@spatika/react';
import { addExternalProvider, getAdminProviders, getLibraries, getSubtitleProvider, saveProviderOrder, setExternalProviderEnabled, startLinkRecovery, updateLibrary } from '../../api';
import { EmptyState, ErrorState, Field, FormDialog, MoreMenu, PageHeader, PageStack, Panel, SettingsGroup, SettingsRow, SkeletonRows, Status, Tag, rowActivation, useConfirm } from '../components';
import { describeError } from '../lib/errors';
import { useNotify } from '../lib/notify';
import { useResource } from '../lib/useResource';
import { useUrlState } from '../lib/useUrlState';
import { ProviderDrawer, providerStatus } from './providers/ProviderDrawer';

const KIND_LABEL = { media: 'titles', performer: 'performers', studio: 'studios' };

// What automatic matching does per kind. Defaults (server side): titles off, people and studios on.
const AUTO_MATCH = [
  { kind: 'titles', label: 'Titles', icon: Film, on: 'A provider’s confident match is applied to the title on its own.', off: 'Every title waits in the review queue until you pick its match.' },
  { kind: 'performers', label: 'People', icon: Users, on: 'Linked when exactly one provider result has the same name or alias.', off: 'Suggestions wait in review on the People page.' },
  { kind: 'studios', label: 'Studios', icon: Building2, on: 'Linked when exactly one provider result has the same name.', off: 'Suggestions wait in review on the Studios page.' },
];

// Connects an HTTP provider plugin (see docs/provider-plugins.md) by its address.
function AddPluginDialog({ open, onOpenChange, onAdded }) {
  const notify = useNotify();
  const [baseUrl, setBaseUrl] = useState('');
  const [token, setToken] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  useEffect(() => { if (open) { setBaseUrl(''); setToken(''); setError(null); } }, [open]);
  let urlError = null;
  try { const u = new URL(baseUrl.trim()); if (!/^https?:$/.test(u.protocol)) urlError = 'Use an http:// or https:// address.'; } catch { urlError = baseUrl.trim() ? 'That doesn’t look like a web address.' : 'Enter the address of the plugin.'; }
  async function add() {
    setBusy(true); setError(null);
    try { const r = await addExternalProvider(baseUrl.trim(), token.trim()); notify.success(`${r.name} connected`, 'Tick it for a library to start matching with it.'); onOpenChange(false); onAdded(); }
    catch (err) { setError(describeError('connect the plugin', err)); }
    finally { setBusy(false); }
  }
  return <FormDialog open={open} onOpenChange={onOpenChange} title="Connect a provider plugin" description="Connect your own metadata source: any HTTP service that follows the plugin contract (docs/provider-plugins.md). Bundled plugins are installed from the Plugins page instead."
    submitLabel="Connect" dirty={!!baseUrl || !!token} canSubmit={!urlError} submitting={busy} error={error} onSubmit={add}>
    <Field label="Address" error={urlError}>{props => <Input {...props} type="url" autoFocus placeholder="http://plugin:9100" value={baseUrl} onChange={e => setBaseUrl(e.target.value)} />}</Field>
    <Field label="Access token" optional>{props => <Input {...props} type="password" autoComplete="off" value={token} onChange={e => setToken(e.target.value)} />}</Field>
  </FormDialog>;
}

function LibraryMatrix({ libraries, providers, onChanged }) {
  const notify = useNotify();
  const [busy, setBusy] = useState(null);
  async function toggle(library, id) {
    const next = library.providers.includes(id) ? library.providers.filter(p => p !== id) : [...library.providers, id];
    setBusy(library.id);
    try { await updateLibrary(library.id, { name: library.name, mode: library.mode, defaultAccess: library.defaultAccess, providers: next }); onChanged(); }
    catch (err) { notify.error(`update ${library.name}`, err); }
    finally { setBusy(null); }
  }
  if (!libraries.length) return <EmptyState compact title="No libraries yet" message="Create a library to choose providers per library." action={<Button size="sm" asChild><Link to="/admin/libraries?new=1">New library</Link></Button>} />;
  return <div className="console-table-responsive"><Table aria-label="Providers per library">
    <TableHeader><TableRow><TableHead>Library</TableHead>{providers.map(p => <TableHead key={p.id} className="console-center-cell">{p.name}</TableHead>)}<TableHead>Order</TableHead></TableRow></TableHeader>
    <TableBody>{libraries.map(library => <TableRow key={library.id}>
      <TableCell><span className="console-title-cell">{library.mode === 'private' ? <Lock size={14} aria-hidden="true" /> : <Clapperboard size={14} aria-hidden="true" />}<Link className="console-link" to={`/admin/libraries/${library.id}`}>{library.name}</Link></span></TableCell>
      {providers.map(p => <TableCell key={p.id} className="console-center-cell"><Checkbox aria-label={`${p.name} for ${library.name}`} checked={library.providers.includes(p.id)} disabled={busy === library.id} onCheckedChange={() => toggle(library, p.id)} /></TableCell>)}
      <TableCell className="console-muted">{library.providers.length ? library.providers.map(id => providers.find(p => p.id === id)?.name || id).join(' → ') : 'Matching off'}</TableCell>
    </TableRow>)}</TableBody>
  </Table></div>;
}

export function ProvidersPage() {
  const notify = useNotify();
  const confirm = useConfirm();
  const [filters, setFilters] = useUrlState({ provider: '' });
  const data = useResource(() => getAdminProviders(), []);
  const libs = useResource(() => getLibraries().then(r => r.items || []), []);
  const subtitle = useResource(() => getSubtitleProvider(), []);
  const [adding, setAdding] = useState(false);
  const [busyId, setBusyId] = useState(null);
  const reloadAll = () => { data.reload(); libs.reload(); subtitle.reload(); };
  const items = data.data?.items || [];
  const libraries = libs.data || [];
  const open = filters.provider === 'subtitles' ? subtitle.data : items.find(p => p.id === filters.provider);

  async function move(index, delta) {
    const order = items.map(p => p.id);
    [order[index], order[index + delta]] = [order[index + delta], order[index]];
    try { await saveProviderOrder({ order }); data.reload(); }
    catch (err) { notify.error('save the fallback order', err); }
  }
  async function setMode(mode) {
    try { await saveProviderOrder({ mode }); notify.success('Matching mode saved'); data.reload(); }
    catch (err) { notify.error('save the matching mode', err); }
  }
  async function setAutoMatch(item, on) {
    const ok = await confirm({
      title: `${on ? 'Match' : 'Stop matching'} ${item.label.toLowerCase()} automatically?`,
      consequence: on ? `After a scan or sync, ${item.on.charAt(0).toLowerCase()}${item.on.slice(1)} Your own choices and rejected matches are never overridden.` : `New ${item.label.toLowerCase()} are no longer linked on their own. ${item.off}`,
      affects: 'Every library and every future scan or sync', reversible: on ? 'Links already applied stay; you can switch it off again.' : true, confirmLabel: on ? 'Turn on' : 'Turn off', tone: on ? 'default' : 'danger',
    });
    if (!ok) return;
    setBusyId(item.kind);
    try { await saveProviderOrder({ autoMatch: { [item.kind]: on } }); data.setData(cur => ({ ...cur, autoMatch: { ...cur.autoMatch, [item.kind]: on } })); notify.success(`Automatic ${item.label.toLowerCase()} matching is ${on ? 'on' : 'off'}`); }
    catch (err) { notify.error('save automatic matching', err); }
    finally { setBusyId(null); }
  }
  async function toggleExternal(provider, enabled) {
    setBusyId(provider.id);
    try { await setExternalProviderEnabled(provider.id, enabled); notify.success(enabled ? `${provider.name} switched on` : `${provider.name} switched off`, enabled ? 'It is used for matching again.' : 'It stays connected but is not used for matching.'); await data.reload(); }
    catch (err) { notify.error(`switch ${provider.name}`, err); }
    finally { setBusyId(null); }
  }
  async function recover() {
    const ok = await confirm({ title: 'Find provider links?', consequence: 'Titles matched from an older NFO have no provider link. ReelStash looks each one up and links it only when the title and date agree exactly. Only links are added; files and details are not changed.', affects: 'Matched titles without a provider link', reversible: true, confirmLabel: 'Start' });
    if (!ok) return;
    try { const r = await startLinkRecovery(); notify.success(r.alreadyRunning ? 'Already running' : 'Looking for provider links', 'Progress is on the Jobs page.'); }
    catch (err) { notify.error('start link recovery', err); }
  }

  const sub = subtitle.data;
  const subStatus = sub ? (sub.canDownload ? { label: 'Connected', tone: 'ok' } : sub.enabled ? { label: 'Search only', tone: 'warn' } : { label: 'Not configured', tone: 'neutral' }) : null;

  return <PageStack>
    <PageHeader title="Providers" subtitle="Where ReelStash looks up metadata and subtitles, which libraries use which, and what it may apply without you."
      actions={<><Button onClick={() => setAdding(true)} leadingIcon={<Plus size={14} />}>Add plugin</Button><MoreMenu items={[{ label: 'Find provider links…', icon: Link2, disabled: !items.some(p => p.enabled), onSelect: recover }]} /></>} />
    <Panel title="Metadata providers" bodyless>
      {data.error && !data.data ? <ErrorState title="Couldn't load providers" error={data.error} onRetry={data.reload} />
        : data.loading ? <SkeletonRows rows={3} />
          : items.length === 0 ? <EmptyState compact title="No metadata providers" />
            : <div className="console-table-responsive"><Table aria-label="Metadata providers" className="console-stack-table">
              <TableHeader><TableRow><TableHead>Provider</TableHead><TableHead>Status</TableHead><TableHead>Matches</TableHead><TableHead>Used by</TableHead><TableHead>Use</TableHead><TableHead><span className="console-sr-only">Actions</span></TableHead></TableRow></TableHeader>
              <TableBody>{items.map(provider => {
                const st = providerStatus(provider);
                const used = libraries.filter(l => l.providers.includes(provider.id));
                return <TableRow key={provider.id} {...rowActivation(() => setFilters({ provider: provider.id }))} aria-label={`${provider.name}, open settings`}>
                  <TableCell><strong>{provider.name}</strong>{provider.external && <> <Tag>Plugin</Tag></>}</TableCell>
                  <TableCell><Status tone={st.tone}>{st.label}</Status></TableCell>
                  <TableCell className="console-muted">{(provider.kinds || []).map(k => KIND_LABEL[k] || k).join(', ')}</TableCell>
                  <TableCell className="console-muted console-wrap">{used.length ? used.map(l => l.name).join(', ') : 'No library'}</TableCell>
                  <TableCell>{provider.external ? <Switch checked={!provider.switchedOff} disabled={busyId === provider.id} onCheckedChange={on => toggleExternal(provider, on)} aria-label={`Use ${provider.name} for matching`} /> : <span className="console-muted">—</span>}</TableCell>
                  <TableCell><Button size="sm" variant="ghost" onClick={() => setFilters({ provider: provider.id })} aria-label={`${provider.enabled ? 'Configure' : 'Set up'} ${provider.name}`}>{provider.enabled ? 'Configure' : 'Set up'}</Button></TableCell>
                </TableRow>;
              })}</TableBody>
            </Table></div>}
    </Panel>
    <Panel title="Subtitles" bodyless>
      {!sub ? <SkeletonRows rows={1} /> : <ul className="console-rows"><li className="console-row" data-clickable onClick={() => setFilters({ provider: 'subtitles' })}>
        <div className="console-row__main"><span className="console-row__title">{sub.name}<Status tone={subStatus.tone}>{subStatus.label}</Status></span><span className="console-row__meta">Finds subtitles from a title’s settings (Audio & subtitles) and saves them next to the video, like Movie.en.srt.</span></div>
        <div className="console-row__aside"><Button size="sm" variant="ghost" onClick={e => { e.stopPropagation(); setFilters({ provider: 'subtitles' }); }}>{sub.enabled ? 'Configure' : 'Set up'}</Button></div>
      </li></ul>}
    </Panel>
    <SettingsGroup title="Automatic matching" description="When matching runs after a scan or a sync, decide what may be applied without you. Anything not applied is kept as a suggestion to review.">
      {AUTO_MATCH.map(item => { const on = !!data.data?.autoMatch?.[item.kind]; return <SettingsRow key={item.kind} label={<span className="console-title-cell"><item.icon size={14} aria-hidden="true" />{item.label} <Status tone={on ? 'ok' : 'neutral'}>{on ? 'Automatic' : 'Review first'}</Status></span>} description={on ? item.on : item.off}>
        <Switch checked={on} disabled={!data.data || busyId === item.kind} onCheckedChange={next => setAutoMatch(item, next)} aria-label={`Match ${item.label.toLowerCase()} automatically`} />
      </SettingsRow>; })}
    </SettingsGroup>
    <Panel title="Libraries" subtitle="Each library matches its titles with its own providers, in its own order. People and studios use the providers of the libraries they appear in." bodyless>
      {libs.loading ? <SkeletonRows rows={2} /> : <LibraryMatrix libraries={libraries} providers={items} onChanged={libs.reload} />}
    </Panel>
    {items.length > 1 && data.data && <SettingsGroup title="Several providers" description="How matching chooses between a library’s providers, and the fallback order for titles outside every library.">
      <SettingsRow label="Which match wins" description={data.data.mode === 'best' ? 'Searches all of a library’s providers and takes the most confident match.' : 'Tries each library’s providers in order and stops at the first confident match. Fewer API calls.'}>
        <select className="console-select" aria-label="Which match wins" value={data.data.mode} onChange={e => setMode(e.target.value)}><option value="priority">First confident match</option><option value="best">Best confident match</option></select>
      </SettingsRow>
      <SettingsRow label="Fallback order" description="For titles outside every library, and people and studios with no titles in one.">
        <ol className="console-fallback">{items.map((p, i) => <li key={p.id}><span>{p.name}</span>
          <Button variant="ghost" size="icon-sm" aria-label={`Move ${p.name} up`} disabled={i === 0} onClick={() => move(i, -1)}><ArrowUp size={14} /></Button>
          <Button variant="ghost" size="icon-sm" aria-label={`Move ${p.name} down`} disabled={i === items.length - 1} onClick={() => move(i, 1)}><ArrowDown size={14} /></Button>
        </li>)}</ol>
      </SettingsRow>
    </SettingsGroup>}
    <AddPluginDialog open={adding} onOpenChange={setAdding} onAdded={reloadAll} />
    {filters.provider && open && <ProviderDrawer provider={open} subtitle={filters.provider === 'subtitles'} usedBy={libraries.filter(l => l.providers.includes(open.id))} onChanged={reloadAll} onClose={() => setFilters({ provider: '' })} />}
  </PageStack>;
}
