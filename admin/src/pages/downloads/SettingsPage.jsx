import React, { useCallback, useEffect, useState } from 'react';
import { Badge, Button, Input, Switch, useToast } from '@spatika/react';
import { CheckCircle2, CircleAlert, Folder, Layers, Lock, PlugZap } from 'lucide-react';
import {
  getDownloadSettings, getTorrentPreferences, saveDownloadFolders, saveDownloadSettings, saveTorrentPreferences, testDownloadSettings,
} from '../../api';
import FolderPicker from '../../components/FolderPicker';
import { folderLabel, fromKiB, toKiB } from '../../downloadsModel';
import { useDownloads } from './context';

function ConnectionCard({ settings, live, onSaved }) {
  const { toast } = useToast();
  const [form, setForm] = useState({ url: '', username: '', password: '' });
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState(null);
  useEffect(() => { setForm({ url: settings.url || '', username: settings.username || '', password: '' }); }, [settings.url, settings.username]);
  const set = patch => { setForm(current => ({ ...current, ...patch })); setResult(null); };
  const body = () => ({ url: form.url, username: form.username, ...(form.password ? { password: form.password } : {}) });

  async function test() {
    setBusy(true);
    try { setResult(await testDownloadSettings(body())); }
    catch (err) { setResult({ ok: false, error: err.message }); }
    finally { setBusy(false); }
  }
  async function save(event) {
    event.preventDefault();
    setBusy(true);
    try { onSaved(await saveDownloadSettings(body())); setForm(f => ({ ...f, password: '' })); toast({ title: 'Connection saved', tone: 'success' }); }
    catch (err) { toast({ title: 'Could not save', description: err.message, tone: 'danger' }); }
    finally { setBusy(false); }
  }
  async function clearPassword() {
    try { onSaved(await saveDownloadSettings({ password: '' })); toast({ title: 'Saved password removed', tone: 'success' }); }
    catch (err) { toast({ title: 'Could not remove it', description: err.message, tone: 'danger' }); }
  }

  // The live feed knows first when qBittorrent becomes reachable.
  const st = live || settings.status || {};
  return <section className="rs-dl-card">
    <header className="rs-dl-card-head">
      <div><h2><PlugZap size={18} /> qBittorrent</h2><p>Where ReelStash finds qBittorrent. Empty fields use the server’s environment (REELSTASH_QBIT_*), which the Compose stack sets.</p></div>
      <div className="rs-dl-card-actions">{st.connected
        ? <Badge variant="success"><CheckCircle2 size={12} /> Connected{st.version ? ` · ${st.version}` : ''}</Badge>
        : <Badge variant={st.configured ? 'danger' : 'neutral'} title={st.error || ''}><CircleAlert size={12} /> {st.configured ? 'Not reachable' : 'Not set up'}</Badge>}</div>
    </header>
    {!st.connected && st.error && <p className="rs-dl-warn"><CircleAlert size={15} /><span>{st.error}</span></p>}
    <form className="rs-dl-form rs-dl-grid3" onSubmit={save}>
      <label className="rs-dl-label">Address <Input value={form.url} onChange={e => set({ url: e.target.value })} placeholder={settings.envUrl || 'http://qbittorrent:8080'} /></label>
      <label className="rs-dl-label">Username <Input autoComplete="off" value={form.username} onChange={e => set({ username: e.target.value })} placeholder={settings.envUsername || 'admin'} /></label>
      <label className="rs-dl-label">Password
        <Input type="password" autoComplete="new-password" value={form.password} onChange={e => set({ password: e.target.value })}
          placeholder={settings.passwordSet ? 'Saved; type to replace' : settings.envPasswordSet ? 'From the environment' : ''} />
      </label>
      <div className="rs-dl-form-actions">
        <Button type="submit" size="sm" disabled={busy}>Save</Button>
        <Button type="button" size="sm" variant="secondary" disabled={busy} onClick={test}>Test connection</Button>
        {settings.passwordSet && <Button type="button" size="sm" variant="ghost" onClick={clearPassword}>Forget saved password</Button>}
        {result && (result.ok
          ? <span className="rs-dl-ok"><CheckCircle2 size={14} /> Signed in to qBittorrent {result.version}</span>
          : <span className="rs-dl-bad"><CircleAlert size={14} /> {result.error}</span>)}
      </div>
    </form>
  </section>;
}

function FolderRow({ icon: Icon = Folder, title, text, value, empty, onChange, onClear }) {
  return <div className="rs-dl-folder-setting">
    <div><strong><Icon size={15} /> {title}</strong><p>{text}</p></div>
    <div className="rs-dl-folder-row">
      <span className="rs-dl-folder"><Folder size={14} />{value ? folderLabel(value) : empty}</span>
      <Button type="button" size="sm" variant="secondary" onClick={onChange}>{value ? 'Change…' : 'Choose…'}</Button>
      {value && onClear && <Button type="button" size="sm" variant="ghost" onClick={onClear}>Remove</Button>}
    </div>
  </div>;
}

function FoldersCard({ settings, onSaved, connected }) {
  const { toast } = useToast();
  const [picking, setPicking] = useState('');
  async function save(body, done) {
    try {
      const res = await saveDownloadFolders(body);
      onSaved(res);
      if (res.warning) toast({ title: 'Saved in ReelStash only', description: res.warning, tone: 'warning' });
      else toast({ title: done, tone: 'success' });
    } catch (err) { toast({ title: 'Could not save the folder', description: err.message, tone: 'danger' }); }
  }
  return <section className="rs-dl-card">
    <header className="rs-dl-card-head"><div><h2><Folder size={18} /> Folders</h2><p>Both apps see the same paths when qBittorrent shares ReelStash’s media mount.</p></div></header>
    <FolderRow icon={Lock} title="In-progress folder" value={settings.incomplete} empty="Not set: unfinished files are written straight to their final folder"
      text="qBittorrent keeps unfinished downloads here and moves them out when done. ReelStash never scans, watches, organizes or changes anything inside it, even inside a library folder."
      onChange={() => setPicking('incomplete')} onClear={() => save({ clearIncomplete: true }, 'In-progress folder removed')} />
    <FolderRow title="Default save folder" value={settings.savePath?.storageLocationId ? settings.savePath : null}
      empty={settings.savePath?.path ? `${settings.savePath.path} (not on a ReelStash disk)` : connected ? 'Unknown' : 'Connect qBittorrent first'}
      text="Where finished downloads go when the torrent has no category folder. Move rules then take them on into your libraries."
      onChange={() => setPicking('savePath')} />
    <FolderPicker open={picking === 'incomplete'} title="In-progress folder" description="Pick or create a folder for unfinished downloads, on the same disk as your libraries if you can."
      initial={settings.incomplete} confirmLabel="Protect this folder" allowLocked
      onPick={loc => { setPicking(''); save({ incomplete: { storageLocationId: loc.storageLocationId, path: loc.path } }, 'In-progress folder protected'); }} onClose={() => setPicking('')} />
    <FolderPicker open={picking === 'savePath'} title="Default save folder" initial={settings.savePath?.storageLocationId ? settings.savePath : settings.incomplete}
      onPick={loc => { setPicking(''); save({ savePath: { storageLocationId: loc.storageLocationId, path: loc.path } }, 'Save folder changed'); }} onClose={() => setPicking('')} />
  </section>;
}

// Parallel downloads: qBittorrent's queue. The numbers only apply while queueing is on.
const QUEUE_GROUPS = [
  { title: 'Queue', text: 'With queueing on, qBittorrent runs at most this many torrents at once and queues the rest.', fields: [
    ['queueing_enabled', 'Queue torrents', 'bool'], ['add_to_top_of_queue', 'New torrents go to the top', 'bool'],
  ] },
  { title: 'At the same time', text: 'Seeding torrents count as uploads. Total caps both together.', needsQueue: true, fields: [
    ['max_active_downloads', 'Downloads', 'int'], ['max_active_uploads', 'Uploads (seeding)', 'int'],
    ['max_active_torrents', 'Total torrents', 'int'], ['max_active_checking_torrents', 'Checks', 'int'],
  ] },
  { title: 'Slow torrents', text: 'Don’t let stalled torrents take a slot: below these speeds (KiB/s) for the given seconds they stop counting.', needsQueue: true, fields: [
    ['dont_count_slow_torrents', 'Don’t count slow torrents', 'bool'], ['slow_torrent_dl_rate_threshold', 'Download below', 'int'],
    ['slow_torrent_ul_rate_threshold', 'Upload below', 'int'], ['slow_torrent_inactive_timer', 'For (seconds)', 'int'],
  ] },
];

// The other qBittorrent settings offered here; kib fields are shown in KiB/s.
const PREF_GROUPS = [
  { title: 'Speed', text: 'Limits in KiB/s; 0 means unlimited. The turtle in the top bar switches to the alternative limits.', fields: [
    ['dl_limit', 'Download limit', 'kib'], ['up_limit', 'Upload limit', 'kib'], ['alt_dl_limit', 'Alternative download limit', 'kib'], ['alt_up_limit', 'Alternative upload limit', 'kib'],
  ] },
  { title: 'Seeding limits', text: 'qBittorrent’s own limits for every torrent. Move rules can also remove torrents after seeding.', fields: [
    ['max_ratio_enabled', 'Stop at ratio', 'bool'], ['max_ratio', 'Ratio', 'float'], ['max_seeding_time_enabled', 'Stop after seeding time', 'bool'], ['max_seeding_time', 'Minutes', 'int'],
    ['max_ratio_act', 'Then', 'act'],
  ] },
  { title: 'Network', text: 'Forward the listening port on your router for better speeds.', fields: [
    ['listen_port', 'Listening port', 'int'], ['upnp', 'UPnP / NAT-PMP', 'bool'], ['dht', 'DHT', 'bool'], ['pex', 'Peer exchange', 'bool'], ['lsd', 'Local peer discovery', 'bool'],
    ['max_connec', 'Max connections', 'int'], ['max_connec_per_torrent', 'Connections per torrent', 'int'], ['max_uploads', 'Max upload slots', 'int'], ['max_uploads_per_torrent', 'Upload slots per torrent', 'int'],
  ] },
  { title: 'Files', text: '', fields: [
    ['incomplete_files_ext', 'Add .!qB to unfinished files', 'bool'], ['preallocate_all', 'Allocate full size up front', 'bool'],
  ] },
];

/** A card of qBittorrent settings: edits are kept as a draft until saved. */
function PreferencesCard({ title, description, icon: Icon, groups }) {
  const { toast } = useToast();
  const [values, setValues] = useState(null);
  const [draft, setDraft] = useState({});
  const [error, setError] = useState('');
  const load = useCallback(() => getTorrentPreferences().then(res => { setValues(res.values || {}); setDraft({}); setError(''); }).catch(err => setError(err.message)), []);
  useEffect(() => { load(); }, [load]);
  if (error) return <section className="rs-dl-card"><header className="rs-dl-card-head"><div><h2>{title}</h2><p>{error}</p></div></header></section>;
  if (!values) return null;

  const current = key => (key in draft ? draft[key] : values[key]);
  const set = (key, value) => setDraft(d => ({ ...d, [key]: value }));
  async function save() {
    try { const res = await saveTorrentPreferences(draft); setValues(res.values || {}); setDraft({}); toast({ title: `${title} saved`, tone: 'success' }); }
    catch (err) { toast({ title: 'Could not save', description: err.message, tone: 'danger' }); }
  }
  const queueOff = 'queueing_enabled' in values && !current('queueing_enabled');
  const field = (disabled) => ([key, label, kind]) => {
    if (!(key in values)) return null;
    const v = current(key);
    if (kind === 'bool') return <label key={key} className="rs-dl-switch"><Switch checked={!!v} disabled={disabled} onCheckedChange={next => set(key, next)} aria-label={label} /> {label}</label>;
    if (kind === 'act') return <label key={key} className="rs-dl-label">{label}
      <select value={v === 1 ? 1 : 0} disabled={disabled} onChange={e => set(key, Number(e.target.value))}><option value={0}>Stop the torrent</option><option value={1}>Remove the torrent</option></select>
    </label>;
    const shown = kind === 'kib' ? toKiB(v) : v;
    return <label key={key} className="rs-dl-label">{label}
      <Input inputMode={kind === 'float' ? 'decimal' : 'numeric'} disabled={disabled} value={shown ?? ''} onChange={e => {
        const raw = e.target.value.replace(kind === 'float' ? /[^\d.-]/g : /[^\d-]/g, '');
        set(key, kind === 'kib' ? fromKiB(raw) : kind === 'float' ? Number(raw) : Math.round(Number(raw)));
      }} />
    </label>;
  };
  const dirty = Object.keys(draft).length > 0;
  return <section className="rs-dl-card">
    <header className="rs-dl-card-head">
      <div><h2>{Icon && <Icon size={18} />} {title}</h2><p>{description}</p></div>
      <div className="rs-dl-card-actions">
        {dirty && <Button size="sm" variant="ghost" onClick={() => setDraft({})}>Undo</Button>}
        <Button size="sm" disabled={!dirty} onClick={save}>Save changes</Button>
      </div>
    </header>
    {groups.map(group => <div key={group.title} className={`rs-dl-pref-group${group.needsQueue && queueOff ? ' is-off' : ''}`}>
      <div><strong>{group.title}</strong>{group.text && <p>{group.text}</p>}{group.needsQueue && queueOff && <p className="rs-dl-hint">Turn on queueing for these to apply.</p>}</div>
      <div className="rs-dl-pref-fields">{group.fields.map(field(group.needsQueue && queueOff))}</div>
    </div>)}
  </section>;
}

function PathsCard({ settings, onSaved }) {
  const { toast } = useToast();
  const [from, setFrom] = useState(settings.pathFrom || '');
  const [to, setTo] = useState(settings.pathTo || '');
  useEffect(() => { setFrom(settings.pathFrom || ''); setTo(settings.pathTo || ''); }, [settings.pathFrom, settings.pathTo]);
  async function save(event) {
    event.preventDefault();
    try { onSaved(await saveDownloadSettings({ pathFrom: from, pathTo: to })); toast({ title: 'Path translation saved', tone: 'success' }); }
    catch (err) { toast({ title: 'Could not save', description: err.message, tone: 'danger' }); }
  }
  return <section className="rs-dl-card">
    <header className="rs-dl-card-head"><div><h2>Path translation</h2><p>Only when qBittorrent sees the media under a different path than ReelStash, such as a qBittorrent running elsewhere. Leave empty when both share the /mnt mount.</p></div></header>
    <form className="rs-dl-form rs-dl-grid3" onSubmit={save}>
      <label className="rs-dl-label">qBittorrent path <Input value={from} onChange={e => setFrom(e.target.value)} placeholder="/downloads" /></label>
      <label className="rs-dl-label">ReelStash path <Input value={to} onChange={e => setTo(e.target.value)} placeholder="/mnt/disk1/downloads" /></label>
      <div className="rs-dl-form-actions"><Button type="submit" size="sm" variant="secondary">Save</Button></div>
    </form>
  </section>;
}

export default function SettingsPage() {
  const { toast } = useToast();
  const { snap, reload } = useDownloads();
  const [settings, setSettings] = useState(null);
  const load = useCallback(() => getDownloadSettings().then(setSettings).catch(err => toast({ title: 'Could not load settings', description: err.message, tone: 'danger' })), [toast]);
  useEffect(() => { load(); }, [load]);
  // The connection status catches up once the server has polled qBittorrent again.
  const saved = next => { setSettings(next); reload(); setTimeout(load, 2500); };
  if (!settings) return null;
  const connected = !!settings.status?.connected || !!snap?.connected;
  return <div className="rs-dl-page">
    <ConnectionCard settings={settings} live={snap} onSaved={saved} />
    <FoldersCard settings={settings} onSaved={saved} connected={connected} />
    {connected && <PreferencesCard title="Parallel downloads" icon={Layers} groups={QUEUE_GROUPS}
      description="How many torrents qBittorrent downloads and seeds at the same time." />}
    {connected && <PreferencesCard title="qBittorrent settings" groups={PREF_GROUPS}
      description="Speed, seeding and network. Everything else stays in qBittorrent’s own web UI." />}
    <PathsCard settings={settings} onSaved={saved} />
  </div>;
}
