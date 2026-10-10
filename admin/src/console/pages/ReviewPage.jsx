import React, { useEffect, useRef, useState } from 'react';
import { Button } from '@spatika/react';
import { Check, ClipboardCheck, ExternalLink, Search, Sparkles } from 'lucide-react';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@spatika/react';
import { artworkUrl, confirmReview, dismissMatchSuggestion, getMatchSuggestions, getReviewQueue, matchMediaProvider } from '../../api';
import { mediaHref } from '../../appLinks';
import { EmptyState, ErrorState, PageHeader, PageStack, Panel, SegmentedTabs, SkeletonRows, Status, TableToolbar, useConfirm } from '../components';
import { formatNumber, plural } from '../lib/format';
import { LibraryScopeSelect, useAdminLibrary } from '../lib/libraryScope';
import { useNotify } from '../lib/notify';
import { useUrlState } from '../lib/useUrlState';

const PAGE = 50;
const scoreTone = score => (score >= 90 ? 'ok' : score >= 70 ? 'warn' : 'neutral');

// Keys for working through the queue without the mouse: J/K move, 1–9 pick a candidate, X rejects them all.
function isTyping(event) {
  const el = event.target;
  return event.metaKey || event.ctrlKey || event.altKey || el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName);
}

function Candidate({ candidate, number, busy, onAccept }) {
  const score = Math.round(candidate.score || 0);
  return <li className="console-candidate">
    <div className="console-candidate__main">
      <span className="console-candidate__title" title={candidate.title}>{candidate.title}</span>
      <span className="console-row__meta">{[candidate.site, candidate.date].filter(Boolean).join(' · ') || 'No details'}{candidate.performers?.length ? ` · ${candidate.performers.slice(0, 4).join(', ')}${candidate.performers.length > 4 ? ` +${candidate.performers.length - 4}` : ''}` : ''}</span>
    </div>
    <div className="console-candidate__flags">
      <Status tone={scoreTone(score)}>{score}% match</Status>
      <span className="console-tag">{candidate.provider}</span>
      {candidate.siteMatch && <span className="console-tag" data-tone="accent">Studio</span>}
      {candidate.dateMatch && <span className="console-tag" data-tone="accent">Date</span>}
    </div>
    <Button size="sm" disabled={busy} onClick={() => onAccept(candidate)} leadingIcon={<Check size={14} />}>Use this{number <= 9 && <kbd className="console-kbd">{number}</kbd>}</Button>
  </li>;
}

const MatchRow = React.forwardRef(function MatchRow({ item, busy, focused, onFocus, onAccept, onDismiss }, ref) {
  return <section ref={ref} className="console-panel console-match" data-focused={focused || undefined} onClick={onFocus} aria-label={`Matches for ${item.fileName || item.title}`}>
    <header className="console-panel__head">
      <div className="console-match__source">
        <img className="console-thumb" src={artworkUrl(item.mediaItemId, 'thumbnail', item.posterImageId)} alt="" loading="lazy" onError={e => { e.currentTarget.style.visibility = 'hidden'; }} />
        <div><p className="console-panel__sub">Your file</p><h3 title={item.fileName}>{item.fileName || item.title}</h3></div>
      </div>
      <div className="console-panel__actions">
        <Button size="sm" variant="outline" asChild><a href={mediaHref(`/library/${item.mediaItemId}`)}><Search size={14} /> Search manually</a></Button>
        <Button size="sm" variant="ghost" disabled={busy} onClick={() => onDismiss(item)}>None of these<kbd className="console-kbd">X</kbd></Button>
      </div>
    </header>
    <ul className="console-candidates">
      {(item.candidates || []).map((candidate, index) => <Candidate key={`${candidate.provider}:${candidate.externalId}`} candidate={candidate} number={index + 1} busy={busy} onAccept={c => onAccept(item, c)} />)}
    </ul>
  </section>;
});

function Matches({ libraryId, onCount }) {
  const notify = useNotify();
  const [items, setItems] = useState(null);
  const [error, setError] = useState(null);
  const [total, setTotal] = useState(0);
  const [busyId, setBusyId] = useState(null);
  const [focus, setFocus] = useState(0);
  const [loadingMore, setLoadingMore] = useState(false);
  const rowRefs = useRef([]);

  const load = () => getMatchSuggestions({ limit: PAGE, ...(libraryId ? { libraryId } : {}) })
    .then(result => { setError(null); setItems(result.items || []); setTotal(result.total || 0); onCount?.(result.total || 0); })
    .catch(err => { setError(err); setItems(current => current || []); });
  useEffect(() => { setItems(null); load(); }, [libraryId]); // eslint-disable-line react-hooks/exhaustive-deps

  async function loadMore() {
    setLoadingMore(true);
    try {
      const result = await getMatchSuggestions({ limit: PAGE, offset: items.length, ...(libraryId ? { libraryId } : {}) });
      setItems(list => { const seen = new Set(list.map(e => e.mediaItemId)); return [...list, ...(result.items || []).filter(e => !seen.has(e.mediaItemId))]; });
      setTotal(result.total || 0);
    } catch (err) { notify.error('load more matches', err); }
    finally { setLoadingMore(false); }
  }
  const remove = item => { setItems(list => list.filter(e => e.mediaItemId !== item.mediaItemId)); setTotal(count => { onCount?.(count - 1); return count - 1; }); };
  async function accept(item, candidate) {
    setBusyId(item.mediaItemId);
    try { await matchMediaProvider(item.mediaItemId, candidate.provider, candidate.externalId); notify.success('Matched', candidate.title); remove(item); }
    catch (err) { notify.error('apply the match', err); }
    finally { setBusyId(null); }
  }
  async function dismiss(item) {
    setBusyId(item.mediaItemId);
    try { await dismissMatchSuggestion(item.mediaItemId); remove(item); }
    catch (err) { notify.error('dismiss the suggestions', err); }
    finally { setBusyId(null); }
  }

  useEffect(() => { if (items && focus >= items.length) setFocus(Math.max(0, items.length - 1)); }, [items, focus]);
  useEffect(() => { rowRefs.current[focus]?.scrollIntoView({ block: 'nearest', behavior: 'smooth' }); }, [focus]);
  useEffect(() => {
    const onKey = event => {
      if (!items?.length || isTyping(event) || document.querySelector('[role=dialog]')) return;
      const item = items[focus];
      const key = event.key.toLowerCase();
      if (key === 'j') setFocus(i => Math.min(items.length - 1, i + 1));
      else if (key === 'k') setFocus(i => Math.max(0, i - 1));
      else if (key === 'x' && item && busyId == null) dismiss(item);
      else if (/^[1-9]$/.test(key) && item && busyId == null) { const candidate = item.candidates?.[Number(key) - 1]; if (candidate) accept(item, candidate); else return; }
      else return;
      event.preventDefault();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }); // re-bound each render so the handler sees current items and focus

  if (error && !items?.length) return <Panel bodyless><ErrorState title="Couldn't load matches" error={error} onRetry={load} /></Panel>;
  if (!items) return <Panel bodyless><SkeletonRows rows={4} label="Loading matches" /></Panel>;
  if (!items.length) return <Panel bodyless><EmptyState icon={Sparkles} title="No matches to review" message="After a scan, titles the providers found candidates for show up here, unless automatic title matching applied them." /></Panel>;
  return <>
    <p className="console-muted console-small">{plural(total, 'title')} waiting. Pick the right record, or say none fit. <span className="console-keys"><kbd className="console-kbd">J</kbd>/<kbd className="console-kbd">K</kbd> move · <kbd className="console-kbd">1</kbd>–<kbd className="console-kbd">9</kbd> use a match · <kbd className="console-kbd">X</kbd> none of these</span></p>
    {items.map((item, index) => <MatchRow key={item.mediaItemId} ref={el => { rowRefs.current[index] = el; }} item={item} focused={index === focus} onFocus={() => setFocus(index)} busy={busyId === item.mediaItemId} onAccept={accept} onDismiss={dismiss} />)}
    {items.length < total && <div className="console-center"><Button variant="outline" size="sm" disabled={loadingMore} onClick={loadMore}>{loadingMore ? 'Loading…' : `Show more (${formatNumber(total - items.length)} left)`}</Button></div>}
  </>;
}

function FilenameGuesses({ libraryId }) {
  const notify = useNotify();
  const confirm = useConfirm();
  const [items, setItems] = useState(null);
  const [error, setError] = useState(null);
  const [busyId, setBusyId] = useState(null);
  const load = () => getReviewQueue(libraryId ? { libraryId } : {}).then(r => { setError(null); setItems(r.items || []); }).catch(err => { setError(err); setItems(c => c || []); });
  useEffect(() => { setItems(null); load(); }, [libraryId]); // eslint-disable-line react-hooks/exhaustive-deps

  async function confirmAsIs(item) {
    const ok = await confirm({ title: `Confirm “${item.title}” as it is?`, consequence: 'The title and details guessed from its filename are accepted as final, and it leaves the review queue.', affects: item.title, reversible: 'Partly. You can still edit the title afterwards.', confirmLabel: 'Confirm as it is' });
    if (!ok) return;
    setBusyId(item.mediaItemId);
    try { await confirmReview(item.mediaItemId); await load(); }
    catch (err) { notify.error('confirm the title', err); }
    finally { setBusyId(null); }
  }

  if (error && !items?.length) return <Panel bodyless><ErrorState title="Couldn't load the review queue" error={error} onRetry={load} /></Panel>;
  return <Panel title="Filename guesses" subtitle="Titles whose metadata was only guessed from a filename. Confirm or edit before it counts as final." bodyless>
    {!items ? <SkeletonRows rows={4} /> : items.length === 0 ? <EmptyState icon={ClipboardCheck} title="Nothing to review" message="Nothing needs review right now." />
      : <div className="console-table-responsive"><Table className="console-stack-table" data-actions>
        <TableHeader><TableRow><TableHead>Title</TableHead><TableHead className="console-num-cell">Files</TableHead><TableHead><span className="console-sr-only">Actions</span></TableHead></TableRow></TableHeader>
        <TableBody>{items.map(item => <TableRow key={item.mediaItemId}>
          <TableCell><a className="console-link" href={mediaHref(`/library/${item.mediaItemId}`)}>{item.title}</a></TableCell>
          <TableCell className="console-num-cell">{item.fileCount}</TableCell>
          <TableCell><span className="console-nowrap">
            <Button size="sm" variant="ghost" asChild><a href={mediaHref(`/library/${item.mediaItemId}`)}><ExternalLink size={14} /> Edit</a></Button>
            <Button size="sm" variant="outline" disabled={busyId === item.mediaItemId} onClick={() => confirmAsIs(item)}>Confirm as it is</Button>
          </span></TableCell>
        </TableRow>)}</TableBody>
      </Table></div>}
  </Panel>;
}

export function ReviewPage() {
  const [filters, setFilters] = useUrlState({ tab: 'matches' });
  const [count, setCount] = useState(null);
  const admin = useAdminLibrary();
  return <PageStack>
    <PageHeader title="Review queue" subtitle="Matches the providers suggested that nothing has applied yet. Confirm each one here, or let confident title matches apply on their own under Providers › Automatic matching." />
    <SegmentedTabs page label="Review sections" value={filters.tab} onChange={tab => setFilters({ tab })} options={[{ value: 'matches', label: 'Metadata matches', count }, { value: 'filenames', label: 'Filename guesses' }]} />
    {admin.libraries.length > 1 && <TableToolbar><LibraryScopeSelect /></TableToolbar>}
    {filters.tab === 'matches' ? <Matches libraryId={admin.libraryId} onCount={setCount} /> : <FilenameGuesses libraryId={admin.libraryId} />}
  </PageStack>;
}
