import React, { useCallback, useEffect, useState } from 'react';
import { Badge, DropdownMenu, DropdownMenuCheckboxItem, DropdownMenuContent, DropdownMenuTrigger, useToast } from '@spatika/react';
import { ChevronDown, Download, ExternalLink, RefreshCw, Search, Trash2 } from 'lucide-react';
import { deleteSubtitle, downloadSubtitle, getPreferences, getSubtitleProvider, getTracks, refreshTracks, searchSubtitles } from '../api';
import { adminHref } from '../appLinks';

const PROVIDERS_PATH = '/admin/metadata/providers';

// Languages to search in; nothing checked falls back to the server's default (saved languages, else yours).
function LanguagePicker({ options, selected, onChange, disabled }) {
  const label = id => options.find(o => o.code === id)?.label || id.toUpperCase();
  const summary = !selected.length ? 'Default languages' : selected.length <= 2 ? selected.map(label).join(', ') : `${selected.length} languages`;
  const toggle = code => onChange(selected.includes(code) ? selected.filter(c => c !== code) : [...selected, code]);
  return <DropdownMenu>
    <DropdownMenuTrigger asChild><button type="button" className="rs-match-select" disabled={disabled} aria-label="Subtitle languages">{summary} <ChevronDown size={13} /></button></DropdownMenuTrigger>
    <DropdownMenuContent align="end" className="rs-pm-lang-menu">
      {options.map(o => <DropdownMenuCheckboxItem key={o.code} checked={selected.includes(o.code)} onSelect={e => { e.preventDefault(); toggle(o.code); }}>{o.label}</DropdownMenuCheckboxItem>)}
    </DropdownMenuContent>
  </DropdownMenu>;
}

function TrackList({ title, empty, children }) {
  const items = React.Children.toArray(children);
  return <section className="rs-pm-tracks-section">
    <h3>{title}</h3>
    {items.length ? <ul className="rs-pm-tracks">{items}</ul> : <p className="rs-pm-muted">{empty}</p>}
  </section>;
}

function SubtitleSearch({ item, provider, languages, onDownloaded }) {
  const { toast } = useToast();
  const [query, setQuery] = useState(item.title);
  const [picked, setPicked] = useState([]);
  const [results, setResults] = useState(null);
  const [busy, setBusy] = useState(false);
  const [saving, setSaving] = useState(null); // fileId being downloaded

  async function search(event) {
    event?.preventDefault();
    setBusy(true);
    try {
      const params = { q: query.trim() };
      if (picked.length) params.languages = picked.join(',');
      const found = await searchSubtitles(item.id, params);
      setResults(found);
      if (!picked.length) setPicked(found.languages || []);
    } catch (err) {
      toast({ title: 'Subtitle search failed', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  async function download(result) {
    setSaving(result.fileId);
    try {
      const saved = await downloadSubtitle(item.id, { fileId: result.fileId, language: result.language, hearingImpaired: result.hearingImpaired, foreignPartsOnly: result.foreignPartsOnly });
      toast({ title: 'Subtitle saved', description: `Saved as ${saved.fileName} next to the video.${saved.remaining != null ? ` ${saved.remaining} OpenSubtitles downloads left today.` : ''}`, tone: 'success' });
      onDownloaded();
    } catch (err) {
      toast({ title: 'Could not download the subtitle', description: err.message, tone: 'danger' });
    } finally {
      setSaving(null);
    }
  }

  const langLabel = code => languages.find(l => l.code === code)?.label || code?.toUpperCase() || 'Unknown language';
  const canDownload = results ? results.canDownload : provider.canDownload;

  return <section className="rs-pm-tracks-section">
    <h3>Find subtitles on OpenSubtitles</h3>
    <form className="rs-match-search" onSubmit={search}>
      <div className="rs-match-bar">
        <Search size={15} className="rs-match-bar-icon" />
        <input aria-label="Search OpenSubtitles" value={query} onChange={e => setQuery(e.target.value)} placeholder="Title or file name" />
        <LanguagePicker options={languages} selected={picked} onChange={setPicked} disabled={busy} />
      </div>
      <button className="rs-button rs-button-primary" type="submit" disabled={busy}>{busy ? 'Searching…' : 'Search'}</button>
    </form>
    <p className="rs-pm-muted">Results made for this exact file come first. A downloaded subtitle is saved next to the video, so other players see it too and it moves with the file when it is organized.</p>
    {!canDownload && <p className="rs-pm-warning" role="status">Searching works with the API key alone. To download, add your OpenSubtitles username and password under <a href={adminHref(PROVIDERS_PATH)}>Providers</a>.</p>}
    {results && !results.items.length && <p className="rs-pm-muted">Nothing found{results.hashed ? '' : ' (the file could not be read to match it exactly)'}. Try the original title or fewer words.</p>}
    {!!results?.items.length && <ul className="rs-pm-tracks rs-pm-subtitle-results">
      {results.items.map(result => <li key={result.fileId}>
        <span className="rs-pm-track-main">
          <strong>{langLabel(result.language)}{result.hashMatch && <Badge variant="success">Made for this file</Badge>}</strong>
          <small title={result.release}>{result.release || result.fileName || result.title}</small>
          <small>{[
            `${result.downloads.toLocaleString()} downloads`,
            result.hearingImpaired && 'SDH',
            result.foreignPartsOnly && 'Foreign parts only',
            result.machineTranslated && 'Machine-translated',
            result.aiTranslated && 'AI-translated',
            result.trusted && 'Trusted uploader',
          ].filter(Boolean).join(' · ')}</small>
        </span>
        <span className="rs-pm-actions">
          {result.url && <a className="rs-pm-icon" href={result.url} target="_blank" rel="noreferrer" aria-label="Open on OpenSubtitles" title="Open on OpenSubtitles"><ExternalLink size={15} /></a>}
          <button type="button" className="rs-button rs-button-outline" disabled={!canDownload || saving !== null} onClick={() => download(result)}><Download size={15} /> {saving === result.fileId ? 'Saving…' : 'Download'}</button>
        </span>
      </li>)}
    </ul>}
  </section>;
}

/**
 * The Audio & subtitles tab of the title settings modal: the tracks of the file that plays, the subtitle
 * files beside it (which can be removed), and a search on OpenSubtitles that saves a subtitle next to the video.
 */
export default function MediaTracksTab({ item }) {
  const { toast } = useToast();
  const [tracks, setTracks] = useState(null);
  const [failed, setFailed] = useState('');
  const [busy, setBusy] = useState(false);
  const [removing, setRemoving] = useState(null);
  const [provider, setProvider] = useState(null);
  const [languages, setLanguages] = useState([]);
  const available = (item.files || []).some(f => f.available);

  const load = useCallback(() => getTracks(item.id).then(result => { setTracks(result); setFailed(''); }).catch(err => setFailed(err.message)), [item.id]);
  useEffect(() => {
    setTracks(null);
    if (available) load();
    getSubtitleProvider().then(setProvider).catch(() => setProvider({ enabled: false }));
    getPreferences().then(p => setLanguages(p.languages || [])).catch(() => {});
  }, [load, available]);

  async function reread() {
    setBusy(true);
    try {
      setTracks(await refreshTracks(item.id));
      toast({ title: 'Tracks read again', tone: 'success' });
    } catch (err) {
      toast({ title: 'Could not read the tracks', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  async function remove(track) {
    setBusy(true);
    try {
      await deleteSubtitle(item.id, track.id);
      toast({ title: 'Subtitle file deleted', description: track.fileName, tone: 'success' });
      setRemoving(null);
      await load();
    } catch (err) {
      toast({ title: 'Could not delete the subtitle file', description: err.message, tone: 'danger' });
    } finally {
      setBusy(false);
    }
  }

  if (!available) return <p className="rs-pm-muted">The video file isn’t available right now, so its tracks can’t be read.</p>;
  if (failed) return <p className="rs-pm-warning" role="status">Could not load the tracks: {failed}</p>;
  if (!tracks) return <p className="rs-pm-muted">Reading tracks…</p>;

  return <div className="rs-pm-merge rs-pm-tracks-tab">
    <div className="rs-pm-tracks-head">
      <p className="rs-pm-muted">The tracks of the file that plays. People choose among them in the player’s settings menu; their language defaults pick one automatically.</p>
      <button type="button" className="rs-button rs-button-outline" disabled={busy} onClick={reread}><RefreshCw size={15} /> Read again</button>
    </div>

    <TrackList title="Audio" empty="No audio tracks.">
      {tracks.audio.map(track => <li key={track.index}>
        <span className="rs-pm-track-main"><strong>{track.label}</strong>{track.title && track.title !== track.label && <small>{track.title}</small>}</span>
        {track.default && <Badge variant="neutral">Default</Badge>}
      </li>)}
    </TrackList>

    <TrackList title="Subtitles" empty="No subtitles the browser can show. Find some below, or put a file such as Movie.en.srt next to the video.">
      {tracks.subtitles.map(track => <li key={track.id}>
        <span className="rs-pm-track-main">
          <strong>{track.label}</strong>
          <small>{track.source === 'embedded' ? `Inside the video · ${(track.codec || '').toUpperCase()}` : track.fileName}</small>
        </span>
        <span className="rs-pm-actions">
          {track.default && <Badge variant="neutral">Default</Badge>}
          {track.source === 'external' && (removing === track.id
            ? <>
              <button type="button" className="rs-button rs-button-danger" disabled={busy} onClick={() => remove(track)}>{busy ? 'Deleting…' : 'Delete file'}</button>
              <button type="button" className="rs-button rs-button-outline" disabled={busy} onClick={() => setRemoving(null)}>Cancel</button>
            </>
            : <button type="button" className="rs-pm-icon" aria-label={`Delete ${track.fileName}`} title="Delete this subtitle file from disk" onClick={() => setRemoving(track.id)}><Trash2 size={15} /></button>)}
        </span>
      </li>)}
    </TrackList>
    {tracks.hiddenSubtitles > 0 && <p className="rs-pm-muted">{tracks.hiddenSubtitles} more subtitle {tracks.hiddenSubtitles === 1 ? 'track is' : 'tracks are'} stored as pictures (PGS or VobSub), which browsers can’t show.</p>}

    {provider && (provider.enabled
      ? <SubtitleSearch item={item} provider={provider} languages={languages} onDownloaded={load} />
      : <section className="rs-pm-tracks-section">
        <h3>Find subtitles on OpenSubtitles</h3>
        <p className="rs-pm-muted">Connect an OpenSubtitles.com account under <a href={adminHref(PROVIDERS_PATH)}>Providers</a> to search for subtitles and save them next to the video.</p>
      </section>)}
  </div>;
}
