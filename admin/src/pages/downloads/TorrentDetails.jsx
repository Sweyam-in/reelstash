import React, { useEffect, useState } from 'react';
import {
  Badge, Button, Input, Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle, Tabs, TabsContent, TabsList, TabsTrigger,
  useToast,
} from '@spatika/react';
import { ExternalLink, FolderInput, Pause, Pencil, Play, RefreshCw, Rocket, Trash2, Wand2 } from 'lucide-react';
import {
  applyTorrentRules, getTorrentDetails, getTorrentPeers, renameTorrent, setTorrentFilePriority, setTorrentLimits, torrentAction,
} from '../../api';
import { formatBytes } from '../../format';
import {
  filesHref, folderLabel, formatPercent, formatRatio, formatSpan, formatSpeed, fromKiB, isStopped, moveLabel, toKiB,
  torrentState, torrentTags,
} from '../../downloadsModel';

const PRIORITIES = [[0, 'Skip'], [1, 'Normal'], [6, 'High'], [7, 'Maximum']];
const TRACKER_STATUS = { 0: 'Disabled', 1: 'Not contacted', 2: 'Working', 3: 'Updating', 4: 'Not working' };

const when = unix => (unix > 0 ? new Date(unix * 1000).toLocaleString() : '—');

function Field({ label, children }) {
  return <div className="rs-dl-field"><dt>{label}</dt><dd>{children}</dd></div>;
}

function FilesTab({ hash, files, onChanged }) {
  const { toast } = useToast();
  async function setPriority(index, priority) {
    try { await setTorrentFilePriority(hash, [index], priority); onChanged(); }
    catch (err) { toast({ title: 'Could not change the file', description: err.message, tone: 'danger' }); }
  }
  if (!files) return <p className="rs-move-empty"><RefreshCw size={14} className="rs-spin" /></p>;
  return <ul className="rs-dl-files">
    {files.map(f => <li key={f.index ?? f.name}>
      <span className="rs-dl-file-name" title={f.name}>{f.name}</span>
      <span className="rs-dl-file-meta">{formatBytes(f.size)} · {formatPercent(f.progress)}</span>
      <select value={f.priority} aria-label={`Priority of ${f.name}`} onChange={e => setPriority(f.index, Number(e.target.value))}>
        {PRIORITIES.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
      </select>
    </li>)}
  </ul>;
}

function TrackersTab({ trackers }) {
  if (!trackers) return <p className="rs-move-empty"><RefreshCw size={14} className="rs-spin" /></p>;
  const real = trackers.filter(t => !String(t.url).startsWith('** ['));
  if (!real.length) return <p className="rs-move-empty">No trackers (DHT and peer exchange only).</p>;
  return <ul className="rs-dl-trackers">
    {real.map(t => <li key={t.url}>
      <code title={t.url}>{t.url}</code>
      <span>{TRACKER_STATUS[t.status] || '—'}{t.num_peers >= 0 ? ` · ${t.num_peers} peers` : ''}{t.msg ? ` · ${t.msg}` : ''}</span>
    </li>)}
  </ul>;
}

function PeersTab({ hash, active }) {
  const [peers, setPeers] = useState(null);
  useEffect(() => {
    if (!active) return undefined;
    let live = true;
    const load = () => getTorrentPeers(hash).then(res => live && setPeers(res.peers || [])).catch(() => live && setPeers([]));
    load();
    const id = setInterval(load, 3000);
    return () => { live = false; clearInterval(id); };
  }, [hash, active]);
  if (!peers) return <p className="rs-move-empty"><RefreshCw size={14} className="rs-spin" /></p>;
  if (!peers.length) return <p className="rs-move-empty">No peers connected.</p>;
  return <table className="rs-dl-peers">
    <thead><tr><th>Peer</th><th>Client</th><th>Has</th><th>Down</th><th>Up</th></tr></thead>
    <tbody>{peers.map(p => <tr key={p.address}>
      <td title={p.country || ''}>{p.address}</td><td>{p.client || '—'}</td><td>{formatPercent(p.progress)}</td>
      <td>{formatSpeed(p.dl_speed)}</td><td>{formatSpeed(p.up_speed)}</td>
    </tr>)}</tbody>
  </table>;
}

function LimitsForm({ torrent, onChanged }) {
  const { toast } = useToast();
  const [down, setDown] = useState('');
  const [up, setUp] = useState('');
  useEffect(() => {
    setDown(torrent.dl_limit > 0 ? String(toKiB(torrent.dl_limit)) : '');
    setUp(torrent.up_limit > 0 ? String(toKiB(torrent.up_limit)) : '');
  }, [torrent.hash, torrent.dl_limit, torrent.up_limit]);
  async function save(event) {
    event.preventDefault();
    try { await setTorrentLimits(torrent.hash, { download: fromKiB(down), upload: fromKiB(up) }); toast({ title: 'Limits saved', tone: 'success' }); onChanged(); }
    catch (err) { toast({ title: 'Could not save limits', description: err.message, tone: 'danger' }); }
  }
  return <form className="rs-dl-limits" onSubmit={save}>
    <label>Download limit <span>KiB/s, empty = none</span><Input inputMode="numeric" value={down} onChange={e => setDown(e.target.value.replace(/\D/g, ''))} placeholder="Unlimited" /></label>
    <label>Upload limit <span>KiB/s, empty = none</span><Input inputMode="numeric" value={up} onChange={e => setUp(e.target.value.replace(/\D/g, ''))} placeholder="Unlimited" /></label>
    <Button type="submit" size="sm" variant="secondary">Save limits</Button>
  </form>;
}

/** Right-hand sheet with one torrent's details, files, trackers and peers. */
export default function TorrentDetails({ torrent, onClose, onChanged, onMove, onDelete }) {
  const { toast } = useToast();
  const [details, setDetails] = useState(null);
  const [tab, setTab] = useState('general');
  const [renaming, setRenaming] = useState(false);
  const [name, setName] = useState('');
  const hash = torrent?.hash;

  useEffect(() => { setTab('general'); setRenaming(false); }, [hash]);
  useEffect(() => {
    if (!hash) { setDetails(null); return undefined; }
    let live = true;
    const load = () => getTorrentDetails(hash).then(res => live && setDetails(res)).catch(() => {});
    setDetails(null);
    load();
    const id = setInterval(load, 4000);
    return () => { live = false; clearInterval(id); };
  }, [hash]);

  if (!torrent) return <Sheet open={false} onOpenChange={() => {}}><SheetContent side="right" /></Sheet>;
  const st = torrentState(torrent.state);
  const move = moveLabel(torrent.move);
  const href = filesHref(torrent.folder);
  const refresh = () => { onChanged(); getTorrentDetails(hash).then(setDetails).catch(() => {}); };

  async function act(action, label) {
    try { await torrentAction([hash], action); onChanged(); }
    catch (err) { toast({ title: `Could not ${label}`, description: err.message, tone: 'danger' }); }
  }
  async function applyRules() {
    try {
      const res = await applyTorrentRules(hash);
      const ml = moveLabel(res);
      toast({ title: res.status === 'MOVING' ? `Moving with “${res.ruleName}”` : ml?.label || 'Done', description: res.message || (res.dest ? `To ${res.dest}` : ''), tone: res.status === 'FAILED' ? 'danger' : 'success' });
      onChanged();
    } catch (err) { toast({ title: 'Could not run the rules', description: err.message, tone: 'danger' }); }
  }
  async function saveName(event) {
    event.preventDefault();
    try { await renameTorrent(hash, name); setRenaming(false); onChanged(); }
    catch (err) { toast({ title: 'Could not rename', description: err.message, tone: 'danger' }); }
  }

  return <Sheet open={!!torrent} onOpenChange={next => { if (!next) onClose(); }}>
    <SheetContent side="right" className="rs-dl-sheet">
      <SheetHeader>
        {renaming
          ? <form className="rs-dl-rename" onSubmit={saveName}><Input autoFocus value={name} onChange={e => setName(e.target.value)} aria-label="Torrent name" /><Button type="submit" size="sm">Save</Button><Button type="button" size="sm" variant="ghost" onClick={() => setRenaming(false)}>Cancel</Button></form>
          : <SheetTitle className="rs-dl-sheet-title">{torrent.name}<button type="button" className="rs-fm-navbtn" aria-label="Rename" onClick={() => { setName(torrent.name); setRenaming(true); }}><Pencil size={14} /></button></SheetTitle>}
        <SheetDescription><Badge variant={st.tone}>{st.label}</Badge> {formatPercent(torrent.progress)} of {formatBytes(torrent.size)}</SheetDescription>
      </SheetHeader>
      <div className="rs-dl-sheet-actions">
        {isStopped(torrent)
          ? <Button size="sm" onClick={() => act('start', 'start')}><Play size={14} /> Start</Button>
          : <Button size="sm" variant="secondary" onClick={() => act('stop', 'stop')}><Pause size={14} /> Stop</Button>}
        <Button size="sm" variant="secondary" onClick={() => act(torrent.force_start ? 'noForceStart' : 'forceStart', 'force start')}><Rocket size={14} /> {torrent.force_start ? 'Stop forcing' : 'Force start'}</Button>
        <Button size="sm" variant="secondary" onClick={() => act('recheck', 'recheck')}><RefreshCw size={14} /> Recheck</Button>
        <Button size="sm" variant="secondary" onClick={() => onMove(torrent)}><FolderInput size={14} /> Move…</Button>
        <Button size="sm" variant="destructive-soft" onClick={() => onDelete(torrent)}><Trash2 size={14} /> Remove…</Button>
      </div>
      <Tabs className="rs-dl-tabs" value={tab} onValueChange={setTab}>
        <TabsList>
          <TabsTrigger value="general">General</TabsTrigger>
          <TabsTrigger value="files">Files{details?.files ? ` (${details.files.length})` : ''}</TabsTrigger>
          <TabsTrigger value="trackers">Trackers</TabsTrigger>
          <TabsTrigger value="peers">Peers</TabsTrigger>
        </TabsList>
        <TabsContent value="general">
          <dl className="rs-dl-fields">
            <Field label="Folder">{torrent.folder ? <>{folderLabel(torrent.folder)}{href && <a className="rs-dl-link" href={href}><ExternalLink size={12} /> Open in Files</a>}</> : torrent.save_path}</Field>
            {torrent.library && <Field label="Library">{torrent.library.name}</Field>}
            <Field label="Downloaded">{formatBytes(torrent.downloaded)}{torrent.dlspeed ? ` · ${formatSpeed(torrent.dlspeed)}` : ''}</Field>
            <Field label="Uploaded">{formatBytes(torrent.uploaded)}{torrent.upspeed ? ` · ${formatSpeed(torrent.upspeed)}` : ''}</Field>
            <Field label="Ratio">{formatRatio(torrent.ratio)}</Field>
            <Field label="Seeding time">{formatSpan(torrent.seeding_time)}</Field>
            <Field label="Seeds / peers">{torrent.num_seeds ?? 0} ({torrent.num_complete ?? 0}) / {torrent.num_leechs ?? 0} ({torrent.num_incomplete ?? 0})</Field>
            <Field label="Added">{when(torrent.added_on)}</Field>
            <Field label="Completed">{when(torrent.completion_on)}</Field>
            <Field label="Category">{torrent.category || 'None'}</Field>
            <Field label="Tags">{torrentTags(torrent).join(', ') || 'None'}</Field>
            {details?.properties?.comment && <Field label="Comment">{details.properties.comment}</Field>}
          </dl>
          <div className="rs-dl-sheet-section">
            <h3>Move rules</h3>
            <p>{move ? <><Badge variant={move.tone}>{move.label}</Badge> {torrent.move?.message && torrent.move.status !== 'SKIPPED' ? torrent.move.message : ''}{torrent.move?.dest ? <> · {torrent.move.dest}</> : null}</> : torrent.progress >= 1 ? 'Not looked at by the move rules yet.' : 'The move rules run once it finishes.'}</p>
            <Button size="sm" variant="secondary" disabled={torrent.progress < 1 || torrent.move?.status === 'MOVING'} onClick={applyRules}><Wand2 size={14} /> Run move rules now</Button>
          </div>
          <div className="rs-dl-sheet-section">
            <h3>Downloading</h3>
            <div className="rs-dl-toggles">
              <label className="rs-dl-check"><input type="checkbox" checked={!!torrent.seq_dl} onChange={() => act('toggleSequential', 'change the download order')} /> Download in order</label>
              <label className="rs-dl-check"><input type="checkbox" checked={!!torrent.f_l_piece_prio} onChange={() => act('toggleFirstLast', 'change piece priority')} /> First and last pieces first</label>
            </div>
            <LimitsForm torrent={torrent} onChanged={onChanged} />
          </div>
        </TabsContent>
        <TabsContent value="files"><FilesTab hash={hash} files={details?.files} onChanged={refresh} /></TabsContent>
        <TabsContent value="trackers"><TrackersTab trackers={details?.trackers} /></TabsContent>
        <TabsContent value="peers"><PeersTab hash={hash} active={tab === 'peers'} /></TabsContent>
      </Tabs>
    </SheetContent>
  </Sheet>;
}
