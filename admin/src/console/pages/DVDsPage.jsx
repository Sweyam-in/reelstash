import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import { Button } from '@spatika/react';
import { Disc } from 'lucide-react';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@spatika/react';
import { convertDVDs, getDVDs } from '../../api';
import { BulkBar, EmptyState, ErrorState, PageHeader, PageStack, Panel, SkeletonRows, Status, useConfirm } from '../components';
import { formatBytes, plural } from '../lib/format';
import { useNotify } from '../lib/notify';
import { useResource } from '../lib/useResource';

const canConvert = disc => disc.onDisk && disc.mp4Total > 0 && disc.mp4Ready < disc.mp4Total;

function discStatus(disc) {
  if (!disc.onDisk) return <Status tone="crit">Missing</Status>;
  if (disc.mp4Total > 0 && disc.mp4Ready >= disc.mp4Total) return <Status tone="ok">MP4 ready</Status>;
  if (disc.mp4Ready > 0) return <Status tone="warn">{disc.mp4Ready} of {disc.mp4Total} MP4s</Status>;
  return <Status tone="neutral">Not converted</Status>;
}

export function DVDsPage() {
  const notify = useNotify();
  const confirm = useConfirm();
  const discs = useResource(() => getDVDs().then(r => r.items || []), []);
  const [selected, setSelected] = useState([]);
  const [busy, setBusy] = useState(false);
  const list = discs.data || [];
  const chosen = list.filter(disc => selected.includes(disc.id) && canConvert(disc));

  async function convert(targets) {
    if (!targets.length || busy) return;
    const one = targets.length === 1 ? targets[0] : null;
    const ok = await confirm({
      title: one ? `Convert ${one.name} to MP4?` : `Convert ${targets.length} DVDs to MP4?`,
      consequence: 'ReelStash re-encodes the disc so a browser can play it. The longest title uses the movie folder\'s name, and shorter titles get a number. Menus and picture subtitles are left out, and only the first audio track is kept. This can take a long time.',
      affects: one ? one.path : plural(targets.length, 'DVD'),
      reversible: 'The VOB files stay on disk. Delete an MP4 if you want to convert that title again.',
      confirmLabel: 'Convert',
    });
    if (!ok) return;
    setBusy(true);
    try {
      await convertDVDs(targets.map(disc => disc.id));
      notify.success(one ? `Converting ${one.name}` : `Converting ${targets.length} DVDs`, 'Follow it in Jobs. The VOB files stay where they are.');
      setSelected([]);
    } catch (err) {
      notify.error('start the conversion', err);
    } finally {
      setBusy(false);
    }
  }

  return <PageStack>
    <PageHeader title="DVDs" subtitle="A library scan notices VIDEO_TS folders. Converting one is a separate step: ReelStash writes an MP4 next to the folder and leaves the VOB files in place." />
    {chosen.length > 0 && <BulkBar count={chosen.length} noun={chosen.length === 1 ? 'DVD' : 'DVDs'} onClear={() => setSelected([])}>
      <Button size="sm" disabled={busy} onClick={() => convert(chosen)}>Convert to MP4</Button>
    </BulkBar>}
    {discs.error && !discs.data ? <Panel bodyless><ErrorState title="Couldn't load DVDs" error={discs.error} onRetry={discs.reload} /></Panel>
      : discs.loading ? <Panel bodyless><SkeletonRows rows={4} /></Panel>
        : list.length === 0 ? <Panel bodyless><EmptyState icon={Disc} title="No DVDs found" message="Scan a library that contains VIDEO_TS folders. Detected discs show up here, and you choose which ones to convert."
          action={<Button size="sm" asChild><Link to="/admin/storage">Open storage</Link></Button>} /></Panel>
          : <Panel bodyless><div className="console-table-responsive"><Table className="console-stack-table" data-actions>
            <TableHeader><TableRow>
              <TableHead className="console-table-check"><input type="checkbox" aria-label="Select all DVDs that can be converted" checked={list.some(canConvert) && list.filter(canConvert).every(disc => selected.includes(disc.id))} onChange={e => setSelected(e.target.checked ? list.filter(canConvert).map(disc => disc.id) : [])} /></TableHead>
              <TableHead>Movie</TableHead>
              <TableHead>Disk</TableHead>
              <TableHead>Path</TableHead>
              <TableHead className="console-num-cell">Titles</TableHead>
              <TableHead className="console-num-cell">Size</TableHead>
              <TableHead>MP4</TableHead>
              <TableHead><span className="console-sr-only">Actions</span></TableHead>
            </TableRow></TableHeader>
            <TableBody>{list.map(disc => <TableRow key={disc.id}>
              <TableCell className="console-table-check"><input type="checkbox" aria-label={`Select ${disc.name}`} disabled={!canConvert(disc)} checked={selected.includes(disc.id)} onChange={e => setSelected(s => e.target.checked ? [...s, disc.id] : s.filter(id => id !== disc.id))} /></TableCell>
              <TableCell>{disc.name}</TableCell>
              <TableCell className="console-nowrap">{disc.storageLocation}</TableCell>
              <TableCell className="console-truncate-cell" title={disc.path}>{disc.path}</TableCell>
              <TableCell className="console-num-cell">{disc.titles}</TableCell>
              <TableCell className="console-num-cell">{formatBytes(disc.size)}</TableCell>
              <TableCell>{discStatus(disc)}</TableCell>
              <TableCell>{canConvert(disc) && <Button size="sm" variant="outline" disabled={busy} onClick={() => convert([disc])}>Convert</Button>}</TableCell>
            </TableRow>)}</TableBody>
          </Table></div></Panel>}
  </PageStack>;
}
