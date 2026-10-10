import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  ArrowDown, ArrowUp, Building2, Check, FileText, Film, Folder, FolderTree, GripVertical, Image as ImageIcon,
  MoreHorizontal, Play, Plus, RefreshCw, Search, Trash2, UserRound, WandSparkles,
} from 'lucide-react';
import {
  Badge, Button, Dialog, DialogBody, DialogContent,
  DialogFooter, DialogHeader, DialogTitle, FormField, Input, Menu, MenuItem, SegmentedControl, Spinner, Switch, useToast,
} from '@spatika/react';
import { EmptyState, PageHeader, PageStack, SectionTabs, useConfirm } from '../components';
import { useResource } from '../lib/useResource';
import { LibraryScopeSelect, useAdminLibrary } from '../lib/libraryScope';
import { tabsOf } from '../lib/navigation';
import {
  createOrganizerRule, deleteOrganizerRule, getBackgroundJob, getOrganizerRules, getOrganizerSettings,
  getPerformers, getStorageLocations, getStudios, performerImageUrl, previewCleanup, previewOrganizer, runCleanup, reorderOrganizerRules, runOrganizer,
  studioImageUrl, updateOrganizerRule, updateOrganizerSettings,
} from '../../api';
import { monogram } from '../../components/Monogram';
import { useErrorToast } from '../../hooks/useErrorToast';

const KIND_LABEL = { studio: 'Studio', performer: 'Performer' };
const TOKENS = ['{title}', '{studio}', '{date}', '{year}', '{performers}', '{series}', '{resolution}'];
const SAMPLE = { title: 'Sunset Session', studio: 'Studio X', date: '2024-05-12', year: '2024', performers: 'Jane Doe, Alex Roe', series: '', resolution: '2160p', height: '2160' };

// Mirrors rename.BuildFileName closely enough for a live example.
function sampleName(template) {
  let name = (template || '{title}').replace(/\{(\w+)\}/g, (_, key) => SAMPLE[key] ?? '');
  name = name.replace(/\s*\[\s*\]/g, '').replace(/\s{2,}/g, ' ').replace(/(\s*-\s*){2,}/g, ' - ').replace(/^[\s-]+|[\s-]+$/g, '');
  return name.replace(/[\\/:*?"<>|]/g, '').trim() || SAMPLE.title;
}

function imageUrl(kind, id, imageId) {
  if (!imageId) return null;
  return kind === 'studio' ? studioImageUrl(id, imageId) : performerImageUrl(id, imageId);
}

function EntityAvatar({ kind, id, imageId, name, size = 'md' }) {
  const [failed, setFailed] = useState(false);
  const src = imageUrl(kind, id, imageId);
  const mono = monogram(name);
  const cls = `org-avatar org-avatar-${size}${kind === 'studio' ? ' is-studio' : ''}`;
  if (src && !failed) return <span className={cls}><img src={src} alt="" loading="lazy" onError={() => setFailed(true)} /></span>;
  return <span className={`${cls} rs-mono`} style={mono.style} aria-hidden="true">{mono.initials}</span>;
}

// ---------------------------------------------------------------------------
// Picker: search studios / performers by name and image, add rules.
// ---------------------------------------------------------------------------
function RulePicker({ open, onClose, rules, onAdd }) {
  const [kind, setKind] = useState('performer');
  const [query, setQuery] = useState('');
  const [items, setItems] = useState(null);
  const [busyKey, setBusyKey] = useState(null);
  const notifyError = useErrorToast();
  const seq = useRef(0);

  useEffect(() => {
    if (!open) return undefined;
    const mine = ++seq.current;
    setItems(null);
    const timer = setTimeout(() => {
      const params = { limit: 60 };
      if (query.trim()) params.q = query.trim();
      (kind === 'studio' ? getStudios(params) : getPerformers(params))
        .then(result => { if (mine === seq.current) setItems(result.items || []); })
        .catch(err => { if (mine === seq.current) { setItems([]); notifyError(err, `Could not load ${kind}s`); } });
    }, query ? 220 : 0);
    return () => clearTimeout(timer);
  }, [open, kind, query]);

  const existing = useMemo(() => new Set(rules.map(r => `${r.kind}:${r.entityId}`)), [rules]);
  const sorted = useMemo(() => (items || []).slice().sort((a, b) => (b.mediaCount || 0) - (a.mediaCount || 0)), [items]);

  async function add(item) {
    const key = `${kind}:${item.id}`;
    setBusyKey(key);
    try { await onAdd(kind, item); }
    finally { setBusyKey(null); }
  }

  return <Dialog open={open} onOpenChange={next => { if (!next) onClose(); }}>
    <DialogContent size="lg" className="org-picker">
      <DialogHeader><DialogTitle>Add organizer rules</DialogTitle></DialogHeader>
      <DialogBody className="org-picker-body">
        <div className="org-picker-bar">
          <SegmentedControl aria-label="Rule type" size="sm" value={kind} onChange={value => { setKind(value); }} options={[
            { value: 'performer', label: 'Performers' }, { value: 'studio', label: 'Studios' },
          ]} />
          <label className="org-search">
            <Search size={16} aria-hidden="true" />
            <span className="sr-only">Search {kind}s</span>
            <Input autoFocus value={query} onChange={event => setQuery(event.target.value)} placeholder={`Search ${kind}s…`} />
          </label>
        </div>
        {items === null ? <div className="org-picker-state"><Spinner /> Loading…</div>
          : sorted.length === 0 ? <div className="org-picker-state">No {kind}s match “{query}”.</div>
            : <ul className="org-picker-grid">
              {sorted.map(item => {
                const key = `${kind}:${item.id}`;
                const added = existing.has(key);
                const name = item.displayName || item.name;
                return <li key={key}>
                  <button type="button" className={`org-tile${added ? ' is-added' : ''}`} disabled={added || busyKey === key} onClick={() => add(item)}>
                    <EntityAvatar kind={kind} id={item.id} imageId={kind === 'studio' ? item.logoImageId : item.profileImageId} name={name} size="lg" />
                    <span className="org-tile-name">{name}</span>
                    <span className="org-tile-meta">{item.mediaCount || 0} video{item.mediaCount === 1 ? '' : 's'}</span>
                    <span className="org-tile-badge" aria-hidden="true">{added ? <Check size={14} /> : busyKey === key ? <RefreshCw size={14} className="rs-spin" /> : <Plus size={14} />}</span>
                    <span className="sr-only">{added ? 'Already has a rule' : 'Add rule'}</span>
                  </button>
                </li>;
              })}
            </ul>}
      </DialogBody>
      <DialogFooter><Button onClick={onClose}>Done</Button></DialogFooter>
    </DialogContent>
  </Dialog>;
}

// ---------------------------------------------------------------------------
// Rule precedence list (drag to reorder).
// ---------------------------------------------------------------------------
function RuleList({ rules, settings, onReorder, onToggle, onEdit, onDelete }) {
  const [dragId, setDragId] = useState(null);
  const [overId, setOverId] = useState(null);
  const [menu, setMenu] = useState({ anchor: null, rule: null });
  const root = settings?.categorizedDir || 'categorized';

  function move(from, to) {
    if (to < 0 || to >= rules.length || from === to) return;
    const next = rules.slice();
    const [item] = next.splice(from, 1);
    next.splice(to, 0, item);
    onReorder(next);
  }

  function drop(targetId) {
    const from = rules.findIndex(r => r.id === dragId);
    const to = rules.findIndex(r => r.id === targetId);
    setDragId(null); setOverId(null);
    if (from >= 0 && to >= 0) move(from, to);
  }

  return <>
    <ol className="org-rules" aria-label="Organizer rules, highest precedence first">
      {rules.map((rule, index) => {
        const folder = (rule.folderName || rule.name).replace(/[\\/:*?"<>|]/g, '').trim();
        return <li key={rule.id}
          className={`org-rule${rule.enabled ? '' : ' is-disabled'}${dragId === rule.id ? ' is-dragging' : ''}${overId === rule.id && dragId !== rule.id ? ' is-over' : ''}`}
          draggable
          onDragStart={event => { setDragId(rule.id); event.dataTransfer.effectAllowed = 'move'; event.dataTransfer.setData('text/plain', String(rule.id)); }}
          onDragOver={event => { event.preventDefault(); if (overId !== rule.id) setOverId(rule.id); }}
          onDragLeave={() => setOverId(current => (current === rule.id ? null : current))}
          onDrop={event => { event.preventDefault(); drop(rule.id); }}
          onDragEnd={() => { setDragId(null); setOverId(null); }}>
          <span className="org-grip" aria-hidden="true"><GripVertical size={16} /></span>
          <span className="org-rank">{index + 1}</span>
          <EntityAvatar kind={rule.kind} id={rule.entityId} imageId={rule.imageId} name={rule.name} />
          <span className="org-rule-main">
            <span className="org-rule-name">{rule.name}
              <Badge size="sm" variant={rule.kind === 'performer' ? 'accent' : 'info'}>{rule.kind === 'performer' ? <UserRound size={11} /> : <Building2 size={11} />} {KIND_LABEL[rule.kind]}</Badge>
            </span>
            <span className="org-rule-path"><Folder size={13} aria-hidden="true" /> {root}/<b>{folder}</b>/</span>
          </span>
          <span className="org-rule-count">{rule.mediaCount}<small>videos</small></span>
          <span className="org-rule-actions">
            <Switch size="sm" checked={rule.enabled} onCheckedChange={() => onToggle(rule)} aria-label={`${rule.enabled ? 'Disable' : 'Enable'} rule for ${rule.name}`} />
            <Button size="icon-sm" variant="ghost" aria-label={`Move ${rule.name} up`} disabled={index === 0} onClick={() => move(index, index - 1)}><ArrowUp size={15} /></Button>
            <Button size="icon-sm" variant="ghost" aria-label={`Move ${rule.name} down`} disabled={index === rules.length - 1} onClick={() => move(index, index + 1)}><ArrowDown size={15} /></Button>
            <Button size="icon-sm" variant="ghost" aria-label={`More actions for ${rule.name}`} onClick={event => setMenu({ anchor: event.currentTarget, rule })}><MoreHorizontal size={16} /></Button>
          </span>
        </li>;
      })}
      <li className="org-rule org-rule-fallback">
        <span className="org-grip" />
        <span className="org-rank">·</span>
        <span className="org-avatar org-avatar-md org-avatar-fallback" aria-hidden="true"><Film size={17} /></span>
        <span className="org-rule-main">
          <span className="org-rule-name">Everything else</span>
          <span className="org-rule-path"><Folder size={13} aria-hidden="true" /> <b>{settings?.uncategorizedDir || 'uncategorized'}</b>/</span>
        </span>
      </li>
    </ol>
    <Menu anchorEl={menu.anchor} open={Boolean(menu.anchor)} onOpenChange={open => { if (!open) setMenu({ anchor: null, rule: null }); }}>
      <MenuItem onClick={() => { onEdit(menu.rule); setMenu({ anchor: null, rule: null }); }}>Rename folder…</MenuItem>
      <MenuItem className="org-danger" onClick={() => { onDelete(menu.rule); setMenu({ anchor: null, rule: null }); }}>Remove rule</MenuItem>
    </Menu>
  </>;
}

// ---------------------------------------------------------------------------
// Layout: folder roots, naming template, sidecars.
// ---------------------------------------------------------------------------
function LayoutCard({ libraryId, settings, exampleFolder, onSaved }) {
  const [form, setForm] = useState(settings);
  const [busy, setBusy] = useState(false);
  const templateRef = useRef(null);
  const notifyError = useErrorToast();
  const { toast } = useToast();
  useEffect(() => { setForm(settings); }, [settings]);
  if (!form) return null;

  const dirty = JSON.stringify(form) !== JSON.stringify(settings);
  const name = form.renameFiles ? sampleName(form.template) : 'original-file-name';
  const set = (key, value) => setForm(current => ({ ...current, [key]: value }));

  function insertToken(token) {
    const input = templateRef.current;
    const value = form.template || '';
    const start = input?.selectionStart ?? value.length;
    const end = input?.selectionEnd ?? value.length;
    set('template', value.slice(0, start) + token + value.slice(end));
    requestAnimationFrame(() => { input?.focus(); input?.setSelectionRange(start + token.length, start + token.length); });
  }

  async function save(event) {
    event.preventDefault();
    setBusy(true);
    try {
      const saved = await updateOrganizerSettings(libraryId, form);
      onSaved(saved);
      toast({ title: 'Layout saved', tone: 'success' });
    } catch (err) { notifyError(err, 'Could not save layout'); }
    finally { setBusy(false); }
  }

  return <form className="org-card org-layout" onSubmit={save}>
    <header className="org-card-head">
      <h2><FolderTree size={17} aria-hidden="true" /> Folder layout</h2>
      <p>Every video gets a folder of its own, named after it.</p>
    </header>

    <div className="org-tree" aria-label="Example layout">
      <div className="org-tree-row" style={{ '--d': 0 }}><Folder size={14} /> {form.categorizedDir || 'categorized'}/</div>
      <div className="org-tree-row" style={{ '--d': 1 }}><Folder size={14} /> {exampleFolder || 'Jane Doe'}/</div>
      <div className="org-tree-row is-accent" style={{ '--d': 2 }}><Folder size={14} /> {name}/</div>
      <div className="org-tree-row" style={{ '--d': 3 }}><Film size={14} /> {name}.mp4</div>
      {form.writeNfo && <div className="org-tree-row" style={{ '--d': 3 }}><FileText size={14} /> {name}.nfo</div>}
      {form.writeImages && <div className="org-tree-row" style={{ '--d': 3 }}><ImageIcon size={14} /> {name}-poster.jpg</div>}
      {form.writeImages && <div className="org-tree-row" style={{ '--d': 3 }}><ImageIcon size={14} /> {name}-thumb.jpg</div>}
      <div className="org-tree-row" style={{ '--d': 0 }}><Folder size={14} /> {form.uncategorizedDir || 'uncategorized'}/</div>
      <div className="org-tree-row is-muted" style={{ '--d': 1 }}><Folder size={14} /> {name}/ …</div>
    </div>

    <div className="org-fields">
      <FormField label="Categorized folder" description="Relative to each disk's root.">
        <Input value={form.categorizedDir} onChange={e => set('categorizedDir', e.target.value)} required />
      </FormField>
      <FormField label="Uncategorized folder" description="For videos no rule claims.">
        <Input value={form.uncategorizedDir} onChange={e => set('uncategorizedDir', e.target.value)} required />
      </FormField>
    </div>

    <div className="org-toggle">
      <div><strong>Rename videos</strong><span>Name each video and its folder from the template below.</span></div>
      <Switch checked={form.renameFiles} onCheckedChange={v => set('renameFiles', v)} aria-label="Rename videos" />
    </div>
    {form.renameFiles && <FormField label="Naming template" description={<>Shared with the Rename Pipeline and metadata matching. Example: <b>{sampleName(form.template)}</b></>}>
      <Input ref={templateRef} value={form.template} onChange={e => set('template', e.target.value)} placeholder={form.defaultTemplate} spellCheck={false} className="org-mono" />
      <div className="org-tokens">{TOKENS.map(t => <button type="button" key={t} className="org-token" onClick={() => insertToken(t)}>{t}</button>)}</div>
    </FormField>}
    <div className="org-toggle">
      <div><strong>Write NFO</strong><span>Create or update a Kodi-style NFO in the video's folder.</span></div>
      <Switch checked={form.writeNfo} onCheckedChange={v => set('writeNfo', v)} aria-label="Write NFO" />
    </div>
    <div className="org-toggle">
      <div><strong>Export artwork</strong><span>Copy the poster and the generated thumbnail into the video's folder.</span></div>
      <Switch checked={form.writeImages} onCheckedChange={v => set('writeImages', v)} aria-label="Export artwork" />
    </div>

    <footer className="org-card-foot">
      {dirty && <Button type="button" variant="ghost" onClick={() => setForm(settings)}>Discard</Button>}
      <Button type="submit" loading={busy} disabled={!dirty}>Save layout</Button>
    </footer>
  </form>;
}

// ---------------------------------------------------------------------------
// Preview + run for the whole library, across every disk it has folders on.
// ---------------------------------------------------------------------------
function PreviewCard({ libraryId, libraryName, disks, rulesVersion }) {
  const [preview, setPreview] = useState(null);
  const [loading, setLoading] = useState(false);
  const [bucket, setBucket] = useState('');
  const [query, setQuery] = useState('');
  const [running, setRunning] = useState(false);
  const notifyError = useErrorToast();
  const confirm = useConfirm();
  const { toast } = useToast();

  const load = useCallback(async () => {
    if (!disks.length) return;
    setLoading(true);
    try {
      const params = { limit: 300 };
      if (bucket) params.rule = bucket;
      if (query.trim()) params.q = query.trim();
      setPreview(await previewOrganizer(libraryId, params));
    } catch (err) { notifyError(err, 'Could not build preview'); }
    finally { setLoading(false); }
  }, [libraryId, disks.length, bucket, query]);

  useEffect(() => {
    const timer = setTimeout(load, query ? 250 : 0);
    return () => clearTimeout(timer);
  }, [load, rulesVersion]);

  async function run() {
    const moves = preview?.plan?.toMove || 0;
    const ok = await confirm({
      title: `Organize ${moves.toLocaleString()} videos in ${libraryName}?`,
      consequence: 'Each video moves into its own folder together with its NFO, subtitles and artwork. The library is updated as files move, so watch history and ratings are kept. Nothing is overwritten.',
      affects: `${moves.toLocaleString()} files on ${disks.length === 1 ? disks[0].name : `${disks.length} disks`}`, reversible: 'Partly. Files can be moved back by changing the rules and running again; folder names are not restored.', confirmLabel: 'Organize',
    });
    if (!ok) return;
    setRunning(true);
    try {
      // One job per disk of the library; wait for all of them and add up their results.
      const { backgroundJobIds = [] } = await runOrganizer(libraryId);
      toast({ title: 'Organizing…', description: 'Files are being moved in the background.', tone: 'info' });
      let pending = [...backgroundJobIds];
      const total = { moved: 0, unchanged: 0, held: 0, failed: 0, errors: [] };
      while (pending.length) {
        await new Promise(resolve => setTimeout(resolve, 1500));
        const jobs = await Promise.all(pending.map(getBackgroundJob));
        for (const job of jobs) {
          if (job.status === 'FAILED' || job.status === 'CANCELLED') throw new Error(job.error || `Organizer ${job.status.toLowerCase()}`);
          if (job.status !== 'COMPLETED') continue;
          const r = job.result || {};
          total.moved += r.moved || 0;
          total.unchanged += r.unchanged || 0;
          total.held += r.held || 0;
          total.failed += r.failed || 0;
          total.errors.push(...(r.errors || []));
        }
        pending = jobs.filter(job => job.status !== 'COMPLETED').map(job => job.id);
      }
      toast({ title: 'Organized', description: `${total.moved} moved, ${total.unchanged} already in place${total.held ? `, ${total.held} held (in use by qBittorrent)` : ''}${total.failed ? `, ${total.failed} failed` : ''}.`, tone: total.failed ? 'warning' : 'success' });
      if (total.errors.length) notifyError({ message: total.errors.slice(0, 3).join('\n') }, `${total.failed} video(s) could not be moved`);
    } catch (err) { notifyError(err, 'Organizer failed'); }
    finally { setRunning(false); load(); }
  }

  const plan = preview?.plan;
  const manyDisks = disks.length > 1;

  return <section className="org-card org-preview">
    <header className="org-card-head org-preview-head">
      <div>
        <h2><WandSparkles size={17} aria-hidden="true" /> Preview &amp; run</h2>
        <p>See exactly what moves before anything touches the disk.</p>
      </div>
      <div className="org-preview-actions">
        <Button size="sm" variant="secondary" onClick={load} disabled={loading || !disks.length}><RefreshCw size={15} className={loading ? 'rs-spin' : ''} /> Refresh</Button>
        <Button size="sm" onClick={run} disabled={running || !plan?.toMove}>{running ? <><RefreshCw size={15} className="rs-spin" /> Organizing…</> : <><Play size={15} /> Organize {plan?.toMove ? plan.toMove.toLocaleString() : ''}</>}</Button>
      </div>
    </header>

    {!disks.length ? <EmptyState compact icon={Folder} title="No folders yet" message="Add folders to this library under Libraries to organize it." />
      : !plan ? <div className="org-picker-state"><Spinner /> Building preview…</div>
        : <>
          <div className="org-stats">
            <div><strong>{plan.total.toLocaleString()}</strong><span>videos in {libraryName}{manyDisks ? ` on ${disks.length} disks` : ''}</span></div>
            <div className="is-accent"><strong>{plan.toMove.toLocaleString()}</strong><span>will move</span></div>
            <div><strong>{plan.unchanged.toLocaleString()}</strong><span>already in place</span></div>
            {plan.held > 0 && <div title="Torrent data qBittorrent is still using stays put; move or remove those torrents from Downloads."><strong>{plan.held.toLocaleString()}</strong><span>held: in use by qBittorrent</span></div>}
          </div>
          <div className="org-buckets" role="group" aria-label="Filter by destination">
            <button type="button" className={`org-chip${bucket === '' ? ' is-on' : ''}`} onClick={() => setBucket('')}>All</button>
            {(plan.buckets || []).map(b => {
              const value = b.ruleId ? String(b.ruleId) : 'uncategorized';
              return <button type="button" key={value} className={`org-chip${bucket === value ? ' is-on' : ''}`} onClick={() => setBucket(bucket === value ? '' : value)}>
                {b.kind === 'performer' ? <UserRound size={12} /> : b.kind === 'studio' ? <Building2 size={12} /> : <Folder size={12} />} {b.name} <em>{b.count}</em>
              </button>;
            })}
          </div>
          <label className="org-search org-preview-search">
            <Search size={16} aria-hidden="true" />
            <span className="sr-only">Filter moves</span>
            <Input value={query} onChange={e => setQuery(e.target.value)} placeholder="Filter by path or title…" />
          </label>
          {plan.moves.length === 0 ? <div className="org-picker-state">{plan.toMove === 0 ? 'Everything is already where it belongs.' : 'No moves match this filter.'}</div>
            : <ul className="org-moves">
              {plan.moves.map(m => <li key={m.mediaFileId}>
                <span className="org-move-from" title={manyDisks ? `${m.storageLocation}: ${m.from}` : m.from}>{manyDisks && <Badge size="sm" variant="neutral">{m.storageLocation}</Badge>} {m.from}</span>
                <span className="org-move-to" title={m.to}>
                  <span className="org-move-arrow" aria-hidden="true">→</span>
                  {m.to}
                </span>
                {m.ruleName ? <Badge size="sm" variant={m.ruleKind === 'performer' ? 'accent' : 'info'}>{m.ruleName}</Badge> : <Badge size="sm" variant="neutral">Uncategorized</Badge>}
              </li>)}
              {preview.matching > plan.moves.length && <li className="org-moves-more">…and {(preview.matching - plan.moves.length).toLocaleString()} more</li>}
            </ul>}
        </>}

  </section>;
}

// ---------------------------------------------------------------------------
// Leftover cleanup: folders with no videos outside the two organized roots.
// ---------------------------------------------------------------------------
function formatBytes(n) {
  if (!n) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
  return `${(n / 1024 ** i).toFixed(i ? 1 : 0)} ${units[i]}`;
}

function CleanupCard({ libraryId, libraryName, disks, settings }) {
  const [plan, setPlan] = useState(null);
  const [loading, setLoading] = useState(false);
  const [running, setRunning] = useState(false);
  const notifyError = useErrorToast();
  const confirm = useConfirm();
  const { toast } = useToast();

  const scan = useCallback(async () => {
    if (!disks.length) return;
    setLoading(true);
    try { setPlan(await previewCleanup(libraryId)); }
    catch (err) { setPlan(null); notifyError(err, 'Could not look for leftovers'); }
    finally { setLoading(false); }
  }, [libraryId, disks.length]);

  async function run() {
    const ok = await confirm({
      title: `Delete ${plan?.dirs?.toLocaleString()} leftover folders in ${libraryName}?`,
      consequence: `These folders contain no videos. They and the ${plan?.files?.toLocaleString()} file(s) inside them are deleted permanently. Folders with videos, and everything in ${settings?.categorizedDir || 'categorized'}/ and ${settings?.uncategorizedDir || 'uncategorized'}/, are left alone.`,
      affects: `${plan?.dirs?.toLocaleString()} folders, ${plan?.files?.toLocaleString()} files, ${formatBytes(plan?.bytes)}`, reversible: false, confirmLabel: 'Delete leftovers', tone: 'danger', typeToConfirm: 'delete',
    });
    if (!ok) return;
    setRunning(true);
    try {
      const r = await runCleanup(libraryId);
      toast({
        title: 'Cleanup done',
        description: `Removed ${r.dirs} folder${r.dirs === 1 ? '' : 's'} and ${r.files} file${r.files === 1 ? '' : 's'} (${formatBytes(r.bytes)}).`,
        tone: r.errors?.length ? 'warning' : 'success',
      });
      if (r.errors?.length) notifyError({ message: r.errors.slice(0, 3).join('\n') }, `${r.errors.length} item(s) could not be removed`);
    } catch (err) { notifyError(err, 'Cleanup failed'); }
    finally { setRunning(false); scan(); }
  }

  const manyDisks = disks.length > 1;
  const cat = settings?.categorizedDir || 'categorized';
  const uncat = settings?.uncategorizedDir || 'uncategorized';

  return <section className="org-card org-preview">
    <header className="org-card-head org-preview-head">
      <div>
        <h2><Trash2 size={17} aria-hidden="true" /> Clean up leftovers</h2>
        <p>Remove folders that hold no videos, and the stray files in them. <b>{cat}/</b> and <b>{uncat}/</b> are never touched.</p>
      </div>
      <div className="org-preview-actions">
        <Button size="sm" variant="secondary" onClick={scan} disabled={loading || running || !disks.length}><Search size={15} className={loading ? 'rs-spin' : ''} /> Find leftovers</Button>
        <Button size="sm" variant="destructive" onClick={run} disabled={running || !plan?.items?.length}>{running ? <><RefreshCw size={15} className="rs-spin" /> Cleaning…</> : <><Trash2 size={15} /> Delete {plan?.items?.length ? plan.dirs.toLocaleString() : ''}</>}</Button>
      </div>
    </header>

    {!disks.length ? <EmptyState compact icon={Folder} title="No folders yet" message="Add folders to this library under Libraries first." />
      : loading && !plan ? <div className="org-picker-state"><Spinner /> Looking…</div>
        : !plan ? <div className="org-picker-state">Find leftovers to see what would be removed. Nothing is deleted until you confirm.</div>
          : plan.items.length === 0 ? <div className="org-picker-state">No leftovers in {libraryName}.</div>
            : <>
              <div className="org-stats">
                <div className="is-accent"><strong>{plan.dirs.toLocaleString()}</strong><span>folders</span></div>
                <div><strong>{plan.files.toLocaleString()}</strong><span>files</span></div>
                <div><strong>{formatBytes(plan.bytes)}</strong><span>freed</span></div>
              </div>
              <ul className="org-moves">
                {plan.items.map(it => <li key={`${it.storageLocationId}:${it.path}`}>
                  <span className="org-move-from" title={it.path}>{manyDisks && <Badge size="sm" variant="neutral">{it.storageLocation}</Badge>} {it.path}/</span>
                  <span className="org-move-to">{it.dirs ? `${it.dirs} subfolder${it.dirs === 1 ? '' : 's'}, ` : ''}{it.files} file{it.files === 1 ? '' : 's'}</span>
                </li>)}
              </ul>
            </>}

  </section>;
}

// ---------------------------------------------------------------------------

// Rules, layout and runs belong to one library; pick which one here.
export function OrganizerPage({ onUpdate }) {
  const disks = useResource(() => getStorageLocations().then(r => r.items || []), []).data || [];
  const admin = useAdminLibrary();
  const library = admin.library || admin.libraries[0] || null;
  if (!admin.loaded) return <PageStack><div className="org-picker-state"><Spinner /> Loading…</div></PageStack>;
  if (!library) return <PageStack>
    <PageHeader title="Organizer" subtitle="Sort videos into studio and performer folders." />
    <EmptyState icon={Folder} title="No libraries yet" message="Create a library first. The organizer's rules and folder layout are kept per library." />
  </PageStack>;
  const libraryDisks = disks.filter(d => (library.roots || []).some(r => r.storageLocationId === d.id));
  return <OrganizerLibrary key={library.id} library={library} disks={libraryDisks} onUpdate={onUpdate} />;
}

function OrganizerLibrary({ library, disks, onUpdate }) {
  const libraryId = library.id;
  const [rules, setRules] = useState([]);
  const [settings, setSettings] = useState(null);
  const [loading, setLoading] = useState(true);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [editing, setEditing] = useState(null);
  const [folderName, setFolderName] = useState('');
  const [version, setVersion] = useState(0);
  const notifyError = useErrorToast();
  const confirm = useConfirm();
  const { toast } = useToast();

  const loadRules = useCallback(async () => {
    try { setRules((await getOrganizerRules(libraryId)).items || []); }
    catch (err) { notifyError(err, 'Could not load organizer rules'); }
  }, [libraryId]);

  useEffect(() => {
    Promise.all([loadRules(), getOrganizerSettings(libraryId).then(setSettings).catch(err => notifyError(err, 'Could not load layout'))])
      .finally(() => setLoading(false));
  }, [loadRules]);

  const changed = () => { setVersion(v => v + 1); onUpdate?.(); };

  async function addRule(kind, item) {
    try {
      await createOrganizerRule(libraryId, { kind, entityId: item.id });
      await loadRules();
      changed();
    } catch (err) { notifyError(err, 'Could not add rule'); }
  }

  async function reorder(next) {
    const previous = rules;
    setRules(next);
    try { await reorderOrganizerRules(libraryId, next.map(r => r.id)); changed(); }
    catch (err) { setRules(previous); notifyError(err, 'Could not reorder rules'); }
  }

  function kindFirst(kind) {
    const next = [...rules.filter(r => r.kind === kind), ...rules.filter(r => r.kind !== kind)];
    if (next.some((r, i) => r.id !== rules[i].id)) reorder(next);
  }

  async function toggle(rule) {
    setRules(current => current.map(r => (r.id === rule.id ? { ...r, enabled: !r.enabled } : r)));
    try { await updateOrganizerRule(libraryId, rule.id, { folderName: rule.folderName, enabled: !rule.enabled }); changed(); }
    catch (err) { loadRules(); notifyError(err, 'Could not update rule'); }
  }

  async function saveFolder(event) {
    event.preventDefault();
    try {
      await updateOrganizerRule(libraryId, editing.id, { folderName, enabled: editing.enabled });
      setEditing(null);
      await loadRules();
      changed();
    } catch (err) { notifyError(err, 'Could not rename folder'); }
  }

  async function remove(rule) {
    const ok = await confirm({ title: `Remove the rule for ${rule.name}?`, consequence: `Files already moved stay where they are. The next organizer run sends this ${rule.kind}'s videos to the next matching rule, or to the uncategorized folder.`, affects: `${rule.mediaCount} videos`, reversible: 'You can add the rule again.', confirmLabel: 'Remove rule', tone: 'danger' });
    if (!ok) return;
    try {
      await deleteOrganizerRule(libraryId, rule.id);
      toast({ title: 'Rule removed', tone: 'success' });
      await loadRules();
      changed();
    } catch (err) { notifyError(err, 'Could not remove rule'); }
  }

  const counts = useMemo(() => ({
    performer: rules.filter(r => r.kind === 'performer').length,
    studio: rules.filter(r => r.kind === 'studio').length,
  }), [rules]);

  return <PageStack className="org-page">
    <PageHeader title="Organizer" subtitle={`Sort ${library.name} videos into studio and performer folders. Rules and layout are saved per library; the rule at the top wins when a video matches more than one.`} actions={<LibraryScopeSelect allowAll={false} />} />
    <SectionTabs entries={tabsOf('organizer')} activePath="/admin/organizer" label="Organizer sections" />

    {loading ? <div className="org-picker-state"><Spinner /> Loading…</div> : <div className="org-grid">
      <div className="org-col">
        <section className="org-card">
          <header className="org-card-head org-rules-head">
            <div>
              <h2>Rules <span className="org-count">{rules.length}</span></h2>
              <p>Drag to set precedence. {counts.performer} performer{counts.performer === 1 ? '' : 's'}, {counts.studio} studio{counts.studio === 1 ? '' : 's'}.</p>
            </div>
            <div className="org-rules-actions">
              {counts.performer > 0 && counts.studio > 0 && <>
                <Button size="sm" variant="ghost" onClick={() => kindFirst('performer')}><UserRound size={15} /> Performers first</Button>
                <Button size="sm" variant="ghost" onClick={() => kindFirst('studio')}><Building2 size={15} /> Studios first</Button>
              </>}
              <Button size="sm" onClick={() => setPickerOpen(true)}><Plus size={16} /> Add rule</Button>
            </div>
          </header>
          {rules.length === 0
            ? <EmptyState compact icon={FolderTree} title="No rules yet" message="Pick studios and performers that should get their own folder. Everything else goes to the uncategorized folder." action={<Button size="sm" onClick={() => setPickerOpen(true)}>Add rule</Button>} />
            : <RuleList rules={rules} settings={settings} onReorder={reorder} onToggle={toggle}
              onEdit={rule => { setEditing(rule); setFolderName(rule.folderName || ''); }} onDelete={remove} />}
        </section>
        <PreviewCard libraryId={libraryId} libraryName={library.name} disks={disks} rulesVersion={version} />
        <CleanupCard libraryId={libraryId} libraryName={library.name} disks={disks} settings={settings} />
      </div>
      <div className="org-col org-col-side">
        <LayoutCard libraryId={libraryId} settings={settings} exampleFolder={(rules[0]?.folderName || rules[0]?.name || '').replace(/[\\/:*?"<>|]/g, '').trim()} onSaved={saved => { setSettings(saved); changed(); }} />
      </div>
    </div>}

    <RulePicker open={pickerOpen} onClose={() => setPickerOpen(false)} rules={rules} onAdd={addRule} />

    <Dialog open={Boolean(editing)} onOpenChange={open => { if (!open) setEditing(null); }}>
      <DialogContent size="sm">
        <DialogHeader><DialogTitle>Folder for {editing?.name}</DialogTitle></DialogHeader>
        <form onSubmit={saveFolder}>
          <DialogBody>
            <FormField label="Folder name" description={`Leave empty to use “${editing?.name}”.`}>
              <Input autoFocus value={folderName} onChange={e => setFolderName(e.target.value)} placeholder={editing?.name} />
            </FormField>
          </DialogBody>
          <DialogFooter>
            <Button type="button" variant="secondary" onClick={() => setEditing(null)}>Cancel</Button>
            <Button type="submit">Save</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>

  </PageStack>;
}
