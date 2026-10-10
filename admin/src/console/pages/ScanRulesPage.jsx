import React, { useEffect, useState } from 'react';
import { Button, Input, NativeSelect } from '@spatika/react';
import { FileX2, FlaskConical, FolderX, ListFilter, Lock, Plus, Trash2 } from 'lucide-react';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@spatika/react';
import { createScanRule, deleteScanRule, getLibraries, getScanRules, testScanRule } from '../../api';
import { EmptyState, ErrorState, Field, PageHeader, PageStack, Panel, SectionTabs, SkeletonRows, Tag, useConfirm } from '../components';
import { plural, timeAgo } from '../lib/format';
import { describeError } from '../lib/errors';
import { useNotify } from '../lib/notify';
import { useResource } from '../lib/useResource';
import { storageTabs } from './StoragePage';

// Skipped by every scan whatever the rules say (skipDirNames in scanner/walk.go).
const BUILT_IN = [
  ['Hidden', 'Anything starting with a dot', ['.*']],
  ['Synology', 'Thumbnails, recycle bin and snapshots', ['@eaDir', '#recycle', '#snapshot']],
  ['QNAP', 'Recycle bin and snapshots', ['@Recycle', '@Recently-Snapshot']],
  ['Windows', 'Recycle bin and system folders', ['$RECYCLE.BIN', 'System Volume Information']],
  ['Linux', 'Recovered file fragments', ['lost+found']],
];
const EXAMPLES = [['*.part', 'Unfinished downloads, anywhere'], ['Samples', 'Any folder or file named Samples'], ['Downloads/incomplete', 'One folder, from the library folder'], ['Clips/**/tmp', 'Every tmp folder under Clips']];

function RuleComposer({ libraries, onSaved }) {
  const notify = useNotify();
  const [pattern, setPattern] = useState('');
  const [scope, setScope] = useState('');
  const [result, setResult] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const rule = () => ({ pattern: pattern.trim(), libraryId: scope ? Number(scope) : null });
  useEffect(() => { setResult(null); setError(null); }, [pattern, scope]);

  async function test() {
    setBusy(true);
    try { setResult(await testScanRule(rule())); }
    catch (err) { setError(describeError('test the rule', err)); }
    finally { setBusy(false); }
  }
  async function save(event) {
    event.preventDefault();
    if (!pattern.trim()) return;
    setBusy(true);
    try { await createScanRule(rule()); notify.success('Scan rule added', 'It applies from the next scan.'); setPattern(''); onSaved(); }
    catch (err) { setError(describeError('add the rule', err)); }
    finally { setBusy(false); }
  }

  return <Panel title="Add a rule" subtitle="Scans skip matching files and folders. Videos already in the library stay as they are.">
    <form className="console-form" onSubmit={save} noValidate>
      <div className="console-inline-form">
        <Field label="Pattern" hint="A pattern with a slash starts at the library folder; one without matches any file or folder with that name.">{props => <Input {...props} value={pattern} onChange={e => setPattern(e.target.value)} placeholder="*.part or Downloads/incomplete" spellCheck={false} autoComplete="off" />}</Field>
        <Field label="Applies to">{props => <NativeSelect {...props} value={scope} onChange={e => setScope(e.target.value)}><option value="">All libraries</option>{libraries.map(l => <option key={l.id} value={l.id}>{l.name}</option>)}</NativeSelect>}</Field>
        <div className="console-inline-form__actions">
          <Button type="button" variant="outline" disabled={!pattern.trim() || busy} onClick={test} leadingIcon={<FlaskConical size={14} />}>Test</Button>
          <Button type="submit" disabled={!pattern.trim() || busy} leadingIcon={<Plus size={14} />}>Add rule</Button>
        </div>
      </div>
      <div className="console-tags" aria-label="Examples">{EXAMPLES.map(([example, meaning]) => <button key={example} type="button" className="console-tag console-tag--button" title={meaning} onClick={() => setPattern(example)}><code>{example}</code> {meaning}</button>)}</div>
      {error && <p className="console-field__error" role="alert">{error}</p>}
      {result && <div className="console-callout" data-tone={result.matches ? 'warn' : undefined} role="status">
        <FlaskConical aria-hidden="true" />
        <div>
          <p><strong>{result.matches.toLocaleString()}</strong> of {result.checked.toLocaleString()} indexed files match.</p>
          <p>{result.matches ? 'Those stay in the library; new files like them will be skipped.' : 'New files that match will be skipped.'}</p>
          {!!result.examples.length && <ul className="console-plain-list">{result.examples.slice(0, 5).map(e => <li key={`${e.storageLocationName}/${e.relativePath}`}><span className="console-muted">{e.storageLocationName}</span> <code className="console-code">{e.relativePath}</code></li>)}</ul>}
        </div>
      </div>}
    </form>
  </Panel>;
}

export function ScanRulesPage() {
  const notify = useNotify();
  const confirm = useConfirm();
  const rules = useResource(() => getScanRules().then(r => r.items || []), []);
  const libs = useResource(() => getLibraries().then(r => r.items || []), []);

  async function remove(rule) {
    const ok = await confirm({ title: `Remove the rule ${rule.pattern}?`, consequence: 'The next scan indexes matching files again.', affects: rule.libraryName || 'All libraries', reversible: true, confirmLabel: 'Remove rule' });
    if (!ok) return;
    try { await deleteScanRule(rule.id); notify.success('Scan rule removed', 'Matching files are picked up again on the next scan.'); await rules.reload(); }
    catch (err) { notify.error('remove the rule', err); }
  }

  const list = rules.data || [];
  return <PageStack>
    <PageHeader title="Scan rules" subtitle="Paths library scans leave out." />
    <SectionTabs entries={storageTabs} activePath="/admin/scan-rules" label="Storage sections" />
    <RuleComposer libraries={libs.data || []} onSaved={rules.reload} />
    <Panel title="Your rules" subtitle={list.length ? `${plural(list.length, 'rule')}, applied on every scan.` : undefined} bodyless>
      {rules.error && !rules.data ? <ErrorState title="Couldn't load scan rules" error={rules.error} onRetry={rules.reload} />
        : rules.loading ? <SkeletonRows rows={3} />
          : list.length === 0 ? <EmptyState icon={ListFilter} title="No scan rules" message="Scans index every video file they find. Pick an example above to start." />
            : <div className="console-table-responsive"><Table className="console-stack-table" data-actions>
              <TableHeader><TableRow><TableHead>Pattern</TableHead><TableHead>Kind</TableHead><TableHead>Applies to</TableHead><TableHead>Added</TableHead><TableHead><span className="console-sr-only">Actions</span></TableHead></TableRow></TableHeader>
              <TableBody>{list.map(rule => {
                const folder = !/[*?]/.test(rule.pattern.split('/').pop());
                return <TableRow key={rule.id}>
                  <TableCell><span className="console-title-cell">{folder ? <FolderX size={14} aria-hidden="true" /> : <FileX2 size={14} aria-hidden="true" />}<code className="console-code">{rule.pattern}</code></span></TableCell>
                  <TableCell className="console-muted">{folder ? 'Folder or file name' : 'File pattern'}</TableCell>
                  <TableCell>{rule.libraryName ? <Tag>{rule.libraryName}</Tag> : 'All libraries'}</TableCell>
                  <TableCell className="console-muted console-nowrap">{timeAgo(rule.createdAt)}</TableCell>
                  <TableCell><Button variant="ghost" size="icon-sm" aria-label={`Remove ${rule.pattern}`} onClick={() => remove(rule)}><Trash2 size={14} /></Button></TableCell>
                </TableRow>;
              })}</TableBody>
            </Table></div>}
    </Panel>
    <Panel title="Always skipped" subtitle="Built in, so scans never pick up system clutter. These can’t be removed." bodyless>
      <ul className="console-rows">{BUILT_IN.map(([system, meaning, patterns]) => <li key={system} className="console-row">
        <div className="console-row__main"><span className="console-row__title"><Lock size={13} aria-hidden="true" />{system}</span><span className="console-row__meta">{meaning}</span></div>
        <div className="console-row__aside">{patterns.map(p => <code key={p} className="console-code">{p}</code>)}</div>
      </li>)}</ul>
    </Panel>
  </PageStack>;
}
