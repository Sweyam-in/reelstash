import React, { useCallback, useEffect, useState } from 'react';
import {
  Badge, Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, Input, Switch, useToast,
} from '@spatika/react';
import { ArrowDown, ArrowUp, FlaskConical, Folder, Pencil, Plus, Trash2, Workflow } from 'lucide-react';
import {
  createDownloadRule, deleteDownloadRule, getDownloadRules, getDownloadSettings, reorderDownloadRules, saveDownloadSettings,
  testDownloadRules, updateDownloadRule,
} from '../../api';
import FolderPicker from '../../components/FolderPicker';
import { folderLabel } from '../../downloadsModel';
import { useDownloads } from './context';

const blankRule = {
  name: '', enabled: true, matchCategory: '', matchTag: '', matchTracker: '', matchName: '', minSizeMb: 0, videoOnly: false,
  destStorageLocationId: 0, destPath: '', subfolder: '', scanAfter: true, removeAfter: 'never', removeRatio: 0, removeSeedMinutes: 0,
};

/** The rule's conditions in words. */
export function ruleConditions(r) {
  const parts = [];
  if (r.matchCategory) parts.push(`category is “${r.matchCategory}”`);
  if (r.matchTag) parts.push(`tagged “${r.matchTag}”`);
  if (r.matchTracker) parts.push(`tracker has “${r.matchTracker}”`);
  if (r.matchName) parts.push(`name matches “${r.matchName}”`);
  if (r.minSizeMb > 0) parts.push(`at least ${r.minSizeMb >= 1024 ? `${(r.minSizeMb / 1024).toFixed(1)} GB` : `${r.minSizeMb} MB`}`);
  if (r.videoOnly) parts.push('has a video file');
  return parts.length ? parts.join(', ') : 'every finished torrent';
}

/** What happens after the move, in words. */
export function ruleAfter(r) {
  const parts = [];
  if (r.scanAfter) parts.push('scan the library');
  if (r.removeAfter === 'moved') parts.push('remove from qBittorrent');
  if (r.removeAfter === 'seeded') {
    const limits = [r.removeRatio > 0 && `ratio ${r.removeRatio}`, r.removeSeedMinutes > 0 && `${r.removeSeedMinutes >= 60 ? `${Math.round(r.removeSeedMinutes / 60 * 10) / 10} h` : `${r.removeSeedMinutes} min`} seeding`].filter(Boolean);
    parts.push(`remove after ${limits.join(' or ')}`);
  }
  return parts.join(' · ') || 'keep seeding';
}

function RuleDialog({ open, rule, categories, onClose, onSaved }) {
  const { toast } = useToast();
  const [form, setForm] = useState(blankRule);
  const [picking, setPicking] = useState(false);
  const [busy, setBusy] = useState(false);
  const set = patch => setForm(current => ({ ...current, ...patch }));
  useEffect(() => { if (open) setForm(rule ? { ...blankRule, ...rule } : blankRule); }, [open, rule]);

  async function save(event) {
    event.preventDefault();
    setBusy(true);
    const body = { ...form, minSizeMb: Number(form.minSizeMb) || 0, removeRatio: Number(form.removeRatio) || 0, removeSeedMinutes: Number(form.removeSeedMinutes) || 0 };
    try {
      if (rule?.id) await updateDownloadRule(rule.id, body);
      else await createDownloadRule(body);
      toast({ title: rule?.id ? 'Rule saved' : 'Rule added', tone: 'success' });
      onClose();
      onSaved();
    } catch (err) { toast({ title: 'Could not save the rule', description: err.message, tone: 'danger' }); }
    finally { setBusy(false); }
  }

  const dest = form.destStorageLocationId ? { storageLocationId: form.destStorageLocationId, storageLocation: form.destStorageLocation, path: form.destPath } : null;
  return <>
    <Dialog open={open && !picking} onOpenChange={next => { if (!next) onClose(); }}>
      <DialogContent className="rs-dl-rule-dialog">
        <DialogHeader>
          <DialogTitle>{rule?.id ? 'Edit move rule' : 'New move rule'}</DialogTitle>
          <DialogDescription>When a torrent finishes and matches every condition you set, qBittorrent moves it to the destination and keeps seeding.</DialogDescription>
        </DialogHeader>
        <form id="rs-dl-rule-form" className="rs-dl-form" onSubmit={save}>
          <label className="rs-dl-label">Name <Input value={form.name} onChange={e => set({ name: e.target.value })} placeholder="Movies" required /></label>
          <fieldset className="rs-dl-fieldset">
            <legend>Conditions <span>leave empty to match anything</span></legend>
            <div className="rs-dl-grid2">
              <label className="rs-dl-label">Category
                <Input list="rs-dl-categories" value={form.matchCategory} onChange={e => set({ matchCategory: e.target.value })} placeholder="Any" />
                <datalist id="rs-dl-categories">{categories.map(c => <option key={c.name} value={c.name} />)}</datalist>
              </label>
              <label className="rs-dl-label">Tag <Input value={form.matchTag} onChange={e => set({ matchTag: e.target.value })} placeholder="Any" /></label>
              <label className="rs-dl-label">Tracker contains <Input value={form.matchTracker} onChange={e => set({ matchTracker: e.target.value })} placeholder="tracker.example.org" /></label>
              <label className="rs-dl-label">Name <Input value={form.matchName} onChange={e => set({ matchName: e.target.value })} placeholder="1080p or *S01E*" /></label>
              <label className="rs-dl-label">At least (MB) <Input inputMode="numeric" value={form.minSizeMb || ''} onChange={e => set({ minSizeMb: e.target.value.replace(/\D/g, '') })} placeholder="0" /></label>
              <label className="rs-dl-check rs-dl-check-inline"><input type="checkbox" checked={form.videoOnly} onChange={e => set({ videoOnly: e.target.checked })} /> Only torrents with a video file</label>
            </div>
          </fieldset>
          <fieldset className="rs-dl-fieldset">
            <legend>Destination</legend>
            <div className="rs-dl-folder-row">
              <span className="rs-dl-folder"><Folder size={14} />{dest ? folderLabel(dest) : 'Choose a folder'}</span>
              <Button type="button" size="sm" variant="secondary" onClick={() => setPicking(true)}>{dest ? 'Change…' : 'Choose…'}</Button>
            </div>
            <label className="rs-dl-label">Subfolder <span>optional: {'{name}'}, {'{category}'}, {'{tag}'}</span>
              <Input value={form.subfolder} onChange={e => set({ subfolder: e.target.value })} placeholder="{name}" />
            </label>
          </fieldset>
          <fieldset className="rs-dl-fieldset">
            <legend>After the move</legend>
            <label className="rs-dl-check"><input type="checkbox" checked={form.scanAfter} onChange={e => set({ scanAfter: e.target.checked })} /> Scan the library it lands in</label>
            <div className="rs-dl-radios" role="radiogroup" aria-label="Remove from qBittorrent">
              <label className="rs-dl-check"><input type="radio" name="removeAfter" checked={form.removeAfter === 'never'} onChange={() => set({ removeAfter: 'never' })} /> Keep seeding</label>
              <label className="rs-dl-check"><input type="radio" name="removeAfter" checked={form.removeAfter === 'moved'} onChange={() => set({ removeAfter: 'moved' })} /> Remove from qBittorrent once moved</label>
              <label className="rs-dl-check"><input type="radio" name="removeAfter" checked={form.removeAfter === 'seeded'} onChange={() => set({ removeAfter: 'seeded' })} /> Remove after seeding enough</label>
            </div>
            {form.removeAfter === 'seeded' && <div className="rs-dl-grid2">
              <label className="rs-dl-label">Ratio reaches <Input inputMode="decimal" value={form.removeRatio || ''} onChange={e => set({ removeRatio: e.target.value.replace(/[^\d.]/g, '') })} placeholder="2.0" /></label>
              <label className="rs-dl-label">or seeded for (minutes) <Input inputMode="numeric" value={form.removeSeedMinutes || ''} onChange={e => set({ removeSeedMinutes: e.target.value.replace(/\D/g, '') })} placeholder="10080" /></label>
            </div>}
            <small className="rs-dl-hint">Removing never deletes the files; they stay in the library.</small>
          </fieldset>
          <label className="rs-dl-check"><input type="checkbox" checked={form.enabled} onChange={e => set({ enabled: e.target.checked })} /> Rule is on</label>
        </form>
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>Cancel</Button>
          <Button type="submit" form="rs-dl-rule-form" disabled={busy || !form.name.trim() || !form.destStorageLocationId}>Save rule</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
    <FolderPicker open={picking} title="Move matching torrents to" description="Usually a library folder. Pick one on the same disk as your downloads so moves are instant."
      initial={dest} onPick={loc => { set({ destStorageLocationId: loc.storageLocationId, destStorageLocation: loc.storageLocation, destPath: loc.path }); setPicking(false); }} onClose={() => setPicking(false)} />
  </>;
}

function TestResults({ plans, onClose }) {
  return <Dialog open={!!plans} onOpenChange={next => { if (!next) onClose(); }}>
    <DialogContent className="rs-dl-test">
      <DialogHeader>
        <DialogTitle>What the rules would do now</DialogTitle>
        <DialogDescription>For every finished torrent. Nothing is moved.</DialogDescription>
      </DialogHeader>
      {plans?.length
        ? <ul className="rs-dl-plans">{plans.map(p => <li key={p.hash}>
          <strong title={p.name}>{p.name}</strong>
          {p.skip ? <span className="is-muted">{p.ruleName ? `${p.ruleName}: ` : ''}{p.skip}</span> : <span><Badge variant="info">{p.ruleName}</Badge> → {p.dest}</span>}
        </li>)}</ul>
        : <p className="rs-move-empty">No finished torrents.</p>}
      <DialogFooter><Button onClick={onClose}>Close</Button></DialogFooter>
    </DialogContent>
  </Dialog>;
}

export default function MoveRulesPage() {
  const { toast } = useToast();
  const { snap, reload } = useDownloads();
  const [rules, setRules] = useState([]);
  const [settings, setSettings] = useState(null);
  const [editing, setEditing] = useState(null); // null closed, {} new, rule
  const [plans, setPlans] = useState(null);
  const [tag, setTag] = useState('');
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => Promise.all([
    getDownloadRules().then(res => setRules(res.items || [])),
    getDownloadSettings().then(res => { setSettings(res); setTag(res.moveTag || ''); }),
  ]).catch(err => toast({ title: 'Could not load move rules', description: err.message, tone: 'danger' })), [toast]);
  useEffect(() => { load(); }, [load]);

  async function setAutoMove(on) {
    try { setSettings(await saveDownloadSettings({ autoMove: on })); reload(); toast({ title: on ? 'Move rules are on' : 'Move rules are off', description: on ? 'Torrents that finish from now on are moved by the first matching rule.' : 'Finished torrents stay where qBittorrent put them.', tone: 'success' }); }
    catch (err) { toast({ title: 'Could not change it', description: err.message, tone: 'danger' }); }
  }
  async function saveTag(event) {
    event.preventDefault();
    try { setSettings(await saveDownloadSettings({ moveTag: tag })); toast({ title: 'Tag saved', tone: 'success' }); }
    catch (err) { toast({ title: 'Could not save the tag', description: err.message, tone: 'danger' }); }
  }
  async function toggle(rule) {
    try { await updateDownloadRule(rule.id, { ...rule, enabled: !rule.enabled }); load(); }
    catch (err) { toast({ title: 'Could not change the rule', description: err.message, tone: 'danger' }); }
  }
  async function moveRule(index, step) {
    const ids = rules.map(r => r.id);
    const [id] = ids.splice(index, 1);
    ids.splice(index + step, 0, id);
    try { const res = await reorderDownloadRules(ids); setRules(res.items || []); }
    catch (err) { toast({ title: 'Could not reorder', description: err.message, tone: 'danger' }); }
  }
  async function remove(rule) {
    try { await deleteDownloadRule(rule.id); toast({ title: 'Rule deleted', tone: 'success' }); load(); }
    catch (err) { toast({ title: 'Could not delete the rule', description: err.message, tone: 'danger' }); }
  }
  async function test() {
    setBusy(true);
    try { setPlans((await testDownloadRules()).items || []); }
    catch (err) { toast({ title: 'Could not test the rules', description: err.message, tone: 'danger' }); }
    finally { setBusy(false); }
  }

  return <div className="rs-dl-page">
    <section className="rs-dl-card rs-dl-automove">
      <div>
        <h2><Workflow size={18} /> Move finished torrents</h2>
        <p>When a torrent finishes, the first matching rule moves it into place, usually a library folder. qBittorrent does the move, so the torrent keeps seeding. Torrents that finished before you switch this on are left alone; run the rules on one from its details.</p>
      </div>
      <Switch checked={!!settings?.autoMove} disabled={!settings} onCheckedChange={setAutoMove} aria-label="Move finished torrents by rule" />
    </section>

    <section className="rs-dl-card">
      <header className="rs-dl-card-head">
        <div><h2>Rules</h2><p>Checked from top to bottom; the first match wins.</p></div>
        <div className="rs-dl-card-actions">
          <Button size="sm" variant="secondary" disabled={busy || !snap?.connected || !rules.length} onClick={test}><FlaskConical size={14} /> Test rules</Button>
          <Button size="sm" onClick={() => setEditing({})}><Plus size={14} /> Add rule</Button>
        </div>
      </header>
      {rules.length
        ? <ol className="rs-dl-rules">{rules.map((r, i) => <li key={r.id} className={r.enabled ? '' : 'is-off'}>
          <span className="rs-dl-rule-pos">{i + 1}</span>
          <div className="rs-dl-rule-main">
            <strong>{r.name}</strong>
            <span>When {ruleConditions(r)}</span>
            <span>Move to <b>{folderLabel({ storageLocationId: r.destStorageLocationId, storageLocation: r.destStorageLocation, path: r.destPath })}{r.subfolder ? `/${r.subfolder}` : ''}</b>, then {ruleAfter(r)}</span>
          </div>
          <div className="rs-dl-rule-actions">
            <button type="button" className="rs-fm-navbtn" aria-label={`Move ${r.name} up`} disabled={i === 0} onClick={() => moveRule(i, -1)}><ArrowUp size={15} /></button>
            <button type="button" className="rs-fm-navbtn" aria-label={`Move ${r.name} down`} disabled={i === rules.length - 1} onClick={() => moveRule(i, 1)}><ArrowDown size={15} /></button>
            <Switch checked={r.enabled} onCheckedChange={() => toggle(r)} aria-label={`${r.enabled ? 'Turn off' : 'Turn on'} ${r.name}`} />
            <Button size="sm" variant="ghost" aria-label={`Edit ${r.name}`} onClick={() => setEditing(r)}><Pencil size={14} /></Button>
            <Button size="sm" variant="ghost" aria-label={`Delete ${r.name}`} onClick={() => remove(r)}><Trash2 size={14} /></Button>
          </div>
        </li>)}</ol>
        : <div className="rs-dl-empty"><strong>No rules yet</strong><span>Add one, for example: category “movies” → your Movies library folder.</span></div>}
    </section>

    <section className="rs-dl-card">
      <header className="rs-dl-card-head"><div><h2>Tag moved torrents</h2><p>Added in qBittorrent after a move, so you can tell them apart there too.</p></div></header>
      <form className="rs-dl-inline-form" onSubmit={saveTag}>
        <Input value={tag} onChange={e => setTag(e.target.value)} placeholder="reelstash" aria-label="Tag added after a move" />
        <Button type="submit" size="sm" variant="secondary">Save</Button>
      </form>
    </section>

    <RuleDialog open={!!editing} rule={editing?.id ? editing : null} categories={snap?.categories || []} onClose={() => setEditing(null)} onSaved={load} />
    <TestResults plans={plans} onClose={() => setPlans(null)} />
  </div>;
}
