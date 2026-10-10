import React, { useEffect, useState } from 'react';
import { Button, Input, NativeSelect, Switch } from '@spatika/react';
import { ArrowDown, ArrowUp, ChevronRight, Clapperboard, Folder, FolderOpen, HardDrive, Lock, Plus, X } from 'lucide-react';
import { listFiles } from '../../../api';
import { LIBRARY_MODES } from '../../../libraryModes';
import { Field, Status } from '../../components';
import { useNotify } from '../../lib/notify';
import { userLabel } from '../../lib/format';

// A new library starts with the providers that suit its mode; the admin can change them.
export const DEFAULT_PROVIDERS = { entertainment: ['tmdb'], private: ['tmdb'] };
export const EMPTY_LIBRARY = { name: '', mode: 'entertainment', defaultAccess: true, roots: [], providers: DEFAULT_PROVIDERS.entertainment, userIds: [] };

export const rootLabel = (root, disks) => ({ disk: disks.find(d => d.id === root.storageLocationId)?.name || `Disk ${root.storageLocationId}`, path: root.path || 'Whole disk' });

// Browses a disk's folders so the admin can pick one as a library folder.
function FolderPicker({ disks, taken, onPick, onCancel }) {
  const notify = useNotify();
  const [diskId, setDiskId] = useState(disks[0]?.id ? String(disks[0].id) : '');
  const [path, setPath] = useState('');
  const [dirs, setDirs] = useState(null);
  useEffect(() => {
    if (!diskId) return undefined;
    let live = true;
    setDirs(null);
    listFiles(diskId, path)
      .then(result => { if (live) setDirs((result.items || []).filter(item => item.isDir).map(item => item.name)); })
      .catch(err => { if (live) { setDirs([]); notify.error('read that folder', err); } });
    return () => { live = false; };
  }, [diskId, path]); // eslint-disable-line react-hooks/exhaustive-deps
  const parts = path ? path.split('/') : [];
  const isTaken = taken.some(root => String(root.storageLocationId) === diskId && root.path === path);
  return <div className="console-folderpicker">
    <div className="console-folderpicker__head">
      <NativeSelect aria-label="Disk" value={diskId} onChange={e => { setDiskId(e.target.value); setPath(''); }}>{disks.map(d => <option key={d.id} value={d.id}>{d.name}</option>)}</NativeSelect>
      <nav className="console-folderpicker__crumbs" aria-label="Folder">
        <button type="button" onClick={() => setPath('')}><HardDrive size={13} aria-hidden="true" /> Disk root</button>
        {parts.map((part, i) => <React.Fragment key={i}><ChevronRight size={12} aria-hidden="true" /><button type="button" onClick={() => setPath(parts.slice(0, i + 1).join('/'))}>{part}</button></React.Fragment>)}
      </nav>
    </div>
    <ul className="console-folderpicker__list">
      {dirs === null && <li className="console-muted console-small">Loading…</li>}
      {dirs?.length === 0 && <li className="console-muted console-small">No subfolders.</li>}
      {dirs?.map(name => <li key={name}><button type="button" onClick={() => setPath(path ? `${path}/${name}` : name)}><Folder size={14} aria-hidden="true" /> {name}</button></li>)}
    </ul>
    <div className="console-folderpicker__actions">
      <Button type="button" variant="ghost" size="sm" onClick={onCancel}>Cancel</Button>
      <Button type="button" size="sm" disabled={!diskId || isTaken} onClick={() => onPick({ storageLocationId: Number(diskId), path })} leadingIcon={<FolderOpen size={14} />}>
        Use {path ? `“${parts[parts.length - 1]}”` : 'the whole disk'}
      </Button>
    </div>
  </div>;
}

/** The library settings form, used to create a library (in a dialog) and to edit one (on its page). */
export function LibraryForm({ form, setForm, disks, providers, users, isNew }) {
  const [picking, setPicking] = useState(false);
  const [providersTouched, setProvidersTouched] = useState(!isNew);
  const set = patch => setForm(current => ({ ...current, ...patch }));
  const setMode = mode => set({ mode, ...(providersTouched ? {} : { providers: DEFAULT_PROVIDERS[mode].filter(id => providers.some(p => p.id === id)) }) });
  const setProviders = next => { setProvidersTouched(true); set({ providers: next }); };
  const toggleProvider = id => setProviders(form.providers.includes(id) ? form.providers.filter(p => p !== id) : [...form.providers, id]);
  const moveProvider = (id, delta) => {
    const next = [...form.providers];
    const i = next.indexOf(id);
    const j = i + delta;
    if (i < 0 || j < 0 || j >= next.length) return;
    [next[i], next[j]] = [next[j], next[i]];
    setProviders(next);
  };
  const toggleUser = id => set({ userIds: form.userIds.includes(id) ? form.userIds.filter(u => u !== id) : [...form.userIds, id] });
  const orderedProviders = [...form.providers.map(id => providers.find(p => p.id === id)).filter(Boolean), ...providers.filter(p => !form.providers.includes(p.id))];
  const members = users.filter(u => u.role !== 'ADMIN');

  return <div className="console-form">
    <Field label="Name" error={form.name.trim() ? null : 'Give the library a name.'}>{props => <Input {...props} value={form.name} onChange={e => set({ name: e.target.value })} placeholder="e.g. Movies & TV" />}</Field>

    <fieldset className="console-fieldset">
      <legend>Mode</legend>
      <div className="console-modes" role="radiogroup" aria-label="Library mode">
        {LIBRARY_MODES.map(mode => <button type="button" role="radio" aria-checked={form.mode === mode.id} key={mode.id} className="console-mode" onClick={() => setMode(mode.id)}>
          <span className="console-mode__head">{mode.id === 'private' ? <Lock size={14} aria-hidden="true" /> : <Clapperboard size={14} aria-hidden="true" />}<strong>{mode.label}</strong></span>
          <small>{mode.description}</small>
        </button>)}
      </div>
    </fieldset>

    <fieldset className="console-fieldset">
      <legend>Folders</legend>
      {form.roots.length === 0 && !picking && <p className="console-field__hint">No folders yet. A folder belongs to one library; its subfolders can belong to another.</p>}
      {form.roots.length > 0 && <ul className="console-chips-list">
        {form.roots.map(root => { const label = rootLabel(root, disks); return <li key={`${root.storageLocationId}:${root.path}`}>
          <HardDrive size={14} aria-hidden="true" /><span><strong>{label.disk}</strong> <code className="console-code">{label.path}</code></span>
          <Button variant="ghost" size="icon-sm" aria-label={`Remove ${label.disk} ${label.path}`} onClick={() => set({ roots: form.roots.filter(r => r !== root) })}><X size={14} /></Button>
        </li>; })}
      </ul>}
      {picking ? <FolderPicker disks={disks} taken={form.roots} onCancel={() => setPicking(false)} onPick={root => { set({ roots: [...form.roots, root] }); setPicking(false); }} />
        : <div><Button type="button" variant="outline" size="sm" disabled={!disks.length} onClick={() => setPicking(true)} leadingIcon={<Plus size={14} />}>Add folder</Button></div>}
      {!disks.length && <p className="console-field__hint">Add a disk under Storage first.</p>}
    </fieldset>

    <fieldset className="console-fieldset">
      <legend>Metadata providers</legend>
      <p className="console-field__hint">Titles are matched with the ticked providers, top first. Credentials are set under Providers.</p>
      <ul className="console-chips-list">
        {orderedProviders.map(p => { const on = form.providers.includes(p.id); const index = form.providers.indexOf(p.id); return <li key={p.id} data-off={!on || undefined}>
          <label className="console-inline-check"><input type="checkbox" checked={on} onChange={() => toggleProvider(p.id)} /> <strong>{p.name}</strong></label>
          {!p.enabled && <Status tone="warn">Not configured</Status>}
          {on && <span className="console-chips-list__order">
            <Button variant="ghost" size="icon-sm" aria-label={`Move ${p.name} up`} disabled={index === 0} onClick={() => moveProvider(p.id, -1)}><ArrowUp size={14} /></Button>
            <Button variant="ghost" size="icon-sm" aria-label={`Move ${p.name} down`} disabled={index === form.providers.length - 1} onClick={() => moveProvider(p.id, 1)}><ArrowDown size={14} /></Button>
          </span>}
        </li>; })}
      </ul>
    </fieldset>

    <fieldset className="console-fieldset">
      <legend>Access</legend>
      <label className="console-inline-switch"><Switch checked={form.defaultAccess} onCheckedChange={value => set({ defaultAccess: value })} aria-label="Give new users access" /><span><strong>Give new users access</strong><small>People signing in for the first time can open this library.</small></span></label>
      {members.length > 0 ? <ul className="console-checks">
        {members.map(u => <li key={u.id}><label className="console-check"><input type="checkbox" checked={form.userIds.includes(u.id)} onChange={() => toggleUser(u.id)} /><span><span className="console-check__label">{userLabel(u)}</span>{u.email && <span className="console-check__desc">{u.email}</span>}</span></label></li>)}
      </ul> : <p className="console-field__hint">No other users have signed in yet.</p>}
      <p className="console-field__hint">Administrators can open every library.</p>
    </fieldset>
  </div>;
}
