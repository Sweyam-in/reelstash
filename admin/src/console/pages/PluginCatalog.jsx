import React, { useEffect, useState } from 'react';
import { Button } from '@spatika/react';
import { Download, Library, Pencil, Puzzle } from 'lucide-react';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@spatika/react';
import { getPluginCatalog, installFromPluginCatalog, setPluginCatalogs } from '../../api';
import { Callout, EmptyState, ErrorState, Field, FormDialog, Panel, SkeletonRows, Status, useConfirm } from '../components';
import { describeError } from '../lib/errors';
import { useNotify } from '../lib/notify';
import { useResource } from '../lib/useResource';

const KIND_LABEL = { provider: 'Metadata provider', auth: 'Sign-in method' };

function SourcesDialog({ open, onOpenChange, sources, onSaved }) {
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  useEffect(() => { if (open) { setText(sources.join('\n')); setError(null); } }, [open, sources]);
  const list = text.split('\n').map(line => line.trim()).filter(Boolean);
  async function save() {
    setBusy(true); setError(null);
    try { await setPluginCatalogs(list); onOpenChange(false); onSaved(); }
    catch (err) { setError(describeError('save the catalogs', err)); }
    finally { setBusy(false); }
  }
  return <FormDialog open={open} onOpenChange={onOpenChange} title="Plugin catalogs" description="The address of each catalog's index.json, one per line. The reelstash_plugins build writes one at dist/index.json."
    submitLabel="Save catalogs" dirty={text !== sources.join('\n')} canSubmit submitting={busy} error={error} onSubmit={save}>
    <Field label="Catalog addresses" hint="Only add catalogs from people you trust: their plugins run on this server.">{props => <textarea {...props} className="rs-pm-textarea" rows={4} value={text} placeholder="https://example.com/dist/index.json" onChange={e => setText(e.target.value)} />}</Field>
  </FormDialog>;
}

function CatalogRow({ item, enabled, busy, onInstall }) {
  const action = item.update ? `Update to ${item.latest}` : item.installed ? null : 'Install';
  return <TableRow data-catalog-plugin={item.id}>
    <TableCell className="console-wrap"><strong>{item.name}</strong>{item.description && <div className="console-row__meta">{item.description}</div>}</TableCell>
    <TableCell>{KIND_LABEL[item.kind] || item.kind}</TableCell>
    <TableCell className="console-nowrap">{item.latest}</TableCell>
    <TableCell>{item.installed ? <Status tone={item.update ? 'warn' : 'ok'}>{item.update ? `${item.installed} installed` : 'Installed'}</Status> : <Status tone="neutral">Not installed</Status>}</TableCell>
    <TableCell>{action && <Button size="sm" variant={item.update ? undefined : 'outline'} disabled={!enabled || busy} onClick={() => onInstall(item)} aria-label={`${action}: ${item.name}`} leadingIcon={<Download size={14} />}>{action}</Button>}</TableCell>
  </TableRow>;
}

/** Admin → Plugins → Catalog: browse saved plugin catalogs and install from them. */
export default function PluginCatalog({ enabled, onInstalled }) {
  const notify = useNotify();
  const confirm = useConfirm();
  const catalog = useResource(() => getPluginCatalog(), []);
  const [editing, setEditing] = useState(false);
  const [busyId, setBusyId] = useState(null);
  const sources = catalog.data?.sources || [];
  const addresses = sources.map(source => source.address);

  async function install(item) {
    const ok = await confirm({
      title: `${item.installed ? 'Update' : 'Install'} ${item.name} ${item.latest}?`,
      consequence: `Downloads the plugin from ${new URL(item.source).host}, checks it against the catalog's checksum, and starts it on this server. The catalog becomes its update source.`,
      affects: item.kind === 'auth' ? 'Sign-in for everyone' : 'Metadata matching',
      reversible: 'You can uninstall it under Installed.',
      confirmLabel: item.installed ? 'Update' : 'Install',
    });
    if (!ok) return;
    setBusyId(item.id);
    try {
      await installFromPluginCatalog(item.source, item.id);
      notify.success(`${item.name} ${item.installed ? 'updated' : 'installed'}`, 'Open its settings under Installed if it needs any.');
      await catalog.reload();
      onInstalled?.();
    } catch (err) { notify.error(`install ${item.name}`, err); }
    finally { setBusyId(null); }
  }

  const items = sources.flatMap(source => source.items || []);
  const failed = sources.filter(source => source.error);
  return <Panel title="Catalog" subtitle="Plugins listed by the catalogs you trust, ready to install." bodyless
    actions={<Button size="sm" variant="outline" disabled={!catalog.data} onClick={() => setEditing(true)} leadingIcon={<Pencil size={14} />}>{addresses.length ? 'Edit catalogs' : 'Add a catalog'}</Button>}>
    {catalog.error && !catalog.data ? <ErrorState title="Couldn't read the catalogs" error={catalog.error} onRetry={catalog.reload} />
      : catalog.loading ? <SkeletonRows rows={2} label="Reading catalogs" />
        : addresses.length === 0 ? <EmptyState icon={Library} title="No catalogs yet" message="Add the address of a plugin catalog (an index.json) to browse its plugins here." action={<Button size="sm" onClick={() => setEditing(true)}>Add a catalog</Button>} />
          : <>
            {failed.map(source => <Callout key={source.address} tone="warn" icon={<Puzzle />} title={`Couldn't read ${source.address}`}>{source.error}</Callout>)}
            {items.length === 0 ? <EmptyState compact icon={Puzzle} title="These catalogs list no plugins" />
              : <div className="console-table-responsive"><Table aria-label="Plugin catalog" className="console-stack-table">
                <TableHeader><TableRow><TableHead>Plugin</TableHead><TableHead>Type</TableHead><TableHead>Latest</TableHead><TableHead>Here</TableHead><TableHead><span className="console-sr-only">Actions</span></TableHead></TableRow></TableHeader>
                <TableBody>{items.map(item => <CatalogRow key={`${item.source}:${item.id}`} item={item} enabled={enabled} busy={busyId === item.id} onInstall={install} />)}</TableBody>
              </Table></div>}
          </>}
    <SourcesDialog open={editing} onOpenChange={setEditing} sources={addresses} onSaved={catalog.reload} />
  </Panel>;
}
