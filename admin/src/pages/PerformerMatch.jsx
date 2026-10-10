import React, { useEffect, useState } from 'react';
import { useToast } from '@spatika/react';
import { Unlink } from 'lucide-react';
import { getPerformerMatchJob, getProviders, matchPerformerProvider, performerImageUrl, searchPerformerProviders } from '../api';

const STATUS = {
  unmatched: 'Not checked against your metadata providers yet.',
  matched: 'Matched.',
  no_match: 'None of your metadata providers had a match for this name.',
  ambiguous: 'More than one result. Choose a match.',
  rejected: 'Marked as a wrong match. Automatic matching skips this one.',
};

function waitForMatch(jobId, loadJob) {
  return new Promise((resolve, reject) => {
    const tick = () => {
      loadJob(jobId).then(job => {
        if (job.status === 'COMPLETED') resolve(job.result || {});
        else if (job.status === 'FAILED' || job.status === 'CANCELLED') reject(new Error(job.error || `Match ${job.status.toLowerCase()}`));
        else setTimeout(tick, 1500);
      }).catch(reject);
    };
    tick();
  });
}

function Candidate({ item, busy, showProvider, providerName, onMatch }) {
  return <div className="rs-performer-candidate">
    {item.image ? <img src={item.image} alt="" /> : <span className="rs-directory-mark">{item.name.slice(0, 2).toUpperCase()}</span>}
    <span>
      <strong>{item.name}</strong>
      {(showProvider || item.disambiguation) ? <small>{[showProvider && providerName(item.provider), item.disambiguation].filter(Boolean).join(' · ')}</small> : null}
      {item.rejected ? <small>You marked this as a wrong match before</small> : null}
    </span>
    <button type="button" className="rs-button rs-button-outline" disabled={busy} onClick={() => onMatch(item)}>Use this match</button>
  </div>;
}

/**
 * Match a performer or studio against the configured metadata providers.
 * `kind` ("performer" | "studio") decides which providers are offered. A record
 * can be linked to one entry per provider; each link is unlinked on its own.
 */
export function PerformerMatch({ performer, kind = 'performer', onUpdated, imageUrl = performerImageUrl, searchRemote = searchPerformerProviders, matchRemote = matchPerformerProvider, loadJob = getPerformerMatchJob, imageField = 'profileImageId', hidePhoto = false, unmatchRemote, allowMarkWrong = true }) {
  const { toast } = useToast();
  const [query, setQuery] = useState(performer.name || '');
  const [results, setResults] = useState(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [allProviders, setAllProviders] = useState([]);
  const [scope, setScope] = useState('all');
  const [removing, setRemoving] = useState(null); // provider id being unlinked, '' = every link
  const [markWrong, setMarkWrong] = useState(true);
  const [removePhoto, setRemovePhoto] = useState(true);
  useEffect(() => {
    setQuery(performer.name || '');
    setResults(null);
    setMessage('');
    setRemoving(null);
  }, [performer.id]);
  useEffect(() => { getProviders().then(r => setAllProviders(r.items || [])).catch(() => setAllProviders([])); }, []);
  const providers = allProviders.filter(p => p.enabled && p.kinds.includes(kind));

  const providerName = id => allProviders.find(p => p.id === id)?.name || id;
  const links = performer.links || [];
  const metadata = performer.metadata || {};
  const storedCandidates = results || (performer.matchStatus === 'ambiguous' ? metadata.candidates : null) || [];
  const multi = providers.length > 1;
  const facts = [
    metadata.disambiguation,
    metadata.network,
    metadata.gender,
    metadata.birthdate,
    metadata.birthplace,
    metadata.nationality,
    metadata.height,
    metadata.url,
  ].filter(Boolean);

  async function runSearch(text, searchScope) {
    setBusy(true);
    setMessage('');
    try {
      const result = await searchRemote(performer.id, text, searchScope === 'all' ? [] : [searchScope]);
      setResults(result.items || []);
      if (!(result.items || []).length) setMessage('No results for that search.');
    } catch (err) {
      toast({ title: 'Search failed', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  function search(event) {
    event.preventDefault();
    if (query.trim()) runSearch(query.trim(), scope);
  }

  function chooseScope(next) {
    setScope(next);
    if (query.trim()) runSearch(query.trim(), next);
  }

  async function match(item) {
    setBusy(true);
    setMessage('');
    try {
      const queued = await matchRemote(performer.id, item.provider, item.id);
      const result = await waitForMatch(queued.backgroundJobId, loadJob);
      setResults(null);
      toast({ title: 'Match saved', description: result.photoError || undefined, tone: result.photoError ? 'warn' : 'success' });
      onUpdated();
    } catch (err) {
      toast({ title: 'Could not save match', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  async function unmatch(options) {
    setBusy(true);
    setMessage('');
    try {
      const result = await unmatchRemote(performer.id, options);
      setRemoving(null);
      setResults(null);
      toast({
        title: options.provider ? `Unlinked ${providerName(options.provider)}` : 'Match removed',
        description: result.linksRemaining > 0 ? 'The other provider links and their details were kept.' : undefined,
        tone: 'success',
      });
      onUpdated();
    } catch (err) {
      toast({ title: 'Could not remove match', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  const matched = performer.matchStatus === 'matched';
  const canRemove = unmatchRemote && ['matched', 'ambiguous', 'no_match'].includes(performer.matchStatus);
  const flagged = unmatchRemote && performer.matchStatus === 'rejected';
  const isLast = removing !== null && (removing === '' || links.length <= 1);
  const startRemoving = provider => { setMarkWrong(allowMarkWrong); setRemovePhoto(!!performer[imageField]); setRemoving(provider); };

  return <section className={`rs-performer-match${hidePhoto ? ' is-text' : ''}`}>
    {!hidePhoto && (performer[imageField]
      ? <img className="rs-performer-photo" src={imageUrl(performer.id, performer[imageField])} alt="" />
      : <span className="rs-performer-photo rs-directory-mark">{(performer.name || '?').slice(0, 2).toUpperCase()}</span>)}
    <div>
      <p className="rs-eyebrow">METADATA PROVIDERS</p>
      <p className="rs-performer-status">{STATUS[performer.matchStatus] || STATUS.unmatched}</p>
      {canRemove && removing === null && (matched && links.length
        ? <ul className="rs-match-links">{links.map(link => <li key={link.provider}>
            <span>Linked to <strong>{providerName(link.provider)}</strong></span>
            <button type="button" className="rs-button rs-button-danger" onClick={() => startRemoving(link.provider)}><Unlink size={15} /> Unlink {providerName(link.provider)}</button>
          </li>)}</ul>
        : <button type="button" className="rs-button rs-button-danger" onClick={() => startRemoving('')}><Unlink size={15} /> {matched ? 'Unlink match' : 'Dismiss and stop matching'}</button>)}
      {flagged && <button type="button" className="rs-button rs-button-danger" disabled={busy} onClick={() => unmatch({ markWrong: false })}>Clear the wrong-match flag</button>}
      {removing !== null && <div className="rs-pm-confirm">
        <p>{!matched
          ? 'Discard the provider suggestions for this record.'
          : isLast
            ? 'Remove the match, its details, and the aliases that came with it.'
            : `Only the ${providerName(removing)} link, and the aliases only it supplied, are removed. Details from the other providers stay.`}</p>
        {allowMarkWrong && <label className="rs-pm-check"><input type="checkbox" checked={markWrong} onChange={e => setMarkWrong(e.target.checked)} /> Mark as a wrong match, so automatic jobs never match it again</label>}
        {isLast && !!performer[imageField] && <label className="rs-pm-check"><input type="checkbox" checked={removePhoto} onChange={e => setRemovePhoto(e.target.checked)} /> Also remove the {hidePhoto ? 'image' : 'photo'}</label>}
        <div>
          <button type="button" className="rs-button rs-button-primary" disabled={busy} onClick={() => unmatch({ markWrong, removePhoto: isLast && removePhoto, provider: removing || undefined })}>{busy ? 'Removing…' : removing ? `Unlink ${providerName(removing)}` : 'Remove match'}</button>
          <button type="button" className="rs-button rs-button-outline" disabled={busy} onClick={() => setRemoving(null)}>Cancel</button>
        </div>
      </div>}
      {(metadata.bio || metadata.description) && <p className="rs-performer-bio">{metadata.bio || metadata.description}</p>}
      {!!facts.length && <p className="rs-performer-facts">{facts.join(' · ')}</p>}
      {!!metadata.aliases?.length && <p className="rs-performer-facts">Also known as {metadata.aliases.join(', ')}</p>}
      {multi && <div className="rs-match-providers" role="group" aria-label="Search providers">
        {[{ id: 'all', name: 'All' }, ...providers].map(p => <button key={p.id} type="button" className={`rs-button ${scope === p.id ? 'rs-button-primary' : 'rs-button-outline'}`} aria-pressed={scope === p.id} disabled={busy} onClick={() => chooseScope(p.id)}>{p.name}</button>)}
      </div>}
      <form className="rs-match-search" onSubmit={search}>
        <input aria-label="Search metadata providers" value={query} onChange={event => setQuery(event.target.value)} placeholder="Search metadata providers by name" />
        <button className="rs-button rs-button-outline" type="submit" disabled={busy || !query.trim()}>Search</button>
      </form>
      {!results && !!storedCandidates.length && <p className="rs-pm-muted">Suggested by the last match job. Nothing was applied; pick the right record or search.</p>}
      {!!storedCandidates.length && <div className="rs-performer-candidates">
        {storedCandidates.map(item => <Candidate key={`${item.provider}:${item.id}`} item={item} busy={busy} showProvider={multi} providerName={providerName} onMatch={match} />)}
      </div>}
      {message && <p className="rs-pm-muted">{message}</p>}
    </div>
  </section>;
}
