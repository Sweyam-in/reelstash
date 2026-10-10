import React, { useEffect, useState } from 'react';
import { Button, Input, useToast } from '@spatika/react';
import { Clapperboard, Lock, LockKeyhole, Moon, Sun } from 'lucide-react';
import { changeMyPassword, exportLibrary, getPreferences, getSession, importLibrary, setAppLockSettings, updatePreferences } from '../api';
import { resetPreferenceCache } from './hooks/useMediaTracks';
import { useAppLock } from '../components/AppLock';
import { useLibrary } from '../libraryContext';
import { LIBRARY_MODES } from '../libraryModes';
import { MEDIA_DARK, MEDIA_LIGHT } from '../shell';
import { useCardStyle } from '../cardStyle';
import { DeviceList, LinkDeviceForm } from '../components/Devices';

// The media app's own settings, per user: which library it opens on, its theme, and what the PIN
// lock covers. (Admins manage libraries and metadata in the admin console.)

const THEME_KEY = 'reelstash-media-mode';
export const MEDIA_THEME_EVENT = 'reelstash:media-theme';
const IDLE_OPTIONS = [5, 15, 30, 60, 120, 240];

function themePreference() {
  let saved = null;
  try { saved = localStorage.getItem(THEME_KEY) || localStorage.getItem('reelstash-media-theme'); } catch { /* storage blocked */ }
  if (saved === 'system') return 'system';
  return saved === MEDIA_LIGHT || saved === 'usha' ? 'light' : 'dark';
}

// The theme to render now: the saved choice, following the OS when it is "system".
export function resolveMediaTheme() {
  const pref = themePreference();
  if (pref === 'system') return window.matchMedia?.('(prefers-color-scheme: light)').matches ? MEDIA_LIGHT : MEDIA_DARK;
  return pref === 'light' ? MEDIA_LIGHT : MEDIA_DARK;
}

function saveThemePreference(pref) {
  try { localStorage.setItem(THEME_KEY, pref === 'system' ? 'system' : pref === 'light' ? MEDIA_LIGHT : MEDIA_DARK); } catch { /* storage blocked */ }
  window.dispatchEvent(new Event(MEDIA_THEME_EVENT));
}

const idleLabel = m => m < 60 ? `${m} minutes` : `${m / 60} hour${m > 60 ? 's' : ''}`;
const modeIcon = mode => mode === 'private' ? Lock : Clapperboard;

function Section({ title, description, children }) {
  return <section className="rs-user-settings-section">
    <header><h2>{title}</h2>{description && <p>{description}</p>}</header>
    <div className="rs-user-settings-body">{children}</div>
  </section>;
}

function SettingsOptions({ label, value, onChange, options }) {
  return <div className="rs-user-settings-options" role="radiogroup" aria-label={label}>
    {options.map(opt => <button key={opt.value} type="button" role="radio" aria-checked={value === opt.value} data-option={opt.value} className={`rs-user-settings-option${value === opt.value ? ' is-selected' : ''}`} onClick={() => onChange(opt.value)}>
      <span className={`rs-user-settings-swatch${opt.split ? ' is-split' : ''}`} aria-hidden="true">{opt.split ? <><i /><i /></> : opt.icon}</span>
      <span><strong>{opt.label}</strong>{opt.hint ? <small>{opt.hint}</small> : null}</span>
    </button>)}
  </div>;
}

function DefaultLibrary() {
  const { libraries, defaultLibraryId, setDefault } = useLibrary();
  const { toast } = useToast();
  if (libraries.length < 2) return <p className="rs-user-settings-note">{libraries.length ? `You have one library, ${libraries[0].name}.` : 'You don’t have a library yet.'}</p>;
  const current = defaultLibraryId && libraries.some(l => l.id === defaultLibraryId) ? defaultLibraryId : libraries[0].id;
  async function choose(id) {
    try {
      await setDefault(id);
      toast({ title: 'Default library saved', description: 'New sessions open on it.', tone: 'success' });
    } catch (err) { toast({ title: 'Could not save', description: err.message, tone: 'danger' }); }
  }
  return <div className="rs-user-settings-choices" role="radiogroup" aria-label="Default library">
    {libraries.map(l => { const Icon = modeIcon(l.mode); return <label key={l.id} data-mode={l.mode} className={`rs-user-settings-choice${current === l.id ? ' is-selected' : ''}`}>
      <input type="radio" name="default-library" checked={current === l.id} onChange={() => choose(l.id)} />
      <Icon size={16} /><span><strong>{l.name}</strong><small>{LIBRARY_MODES.find(m => m.id === l.mode)?.label}</small></span>
    </label>; })}
  </div>;
}

function Theme() {
  const [pref, setPref] = useState(themePreference);
  useEffect(() => {
    const sync = () => setPref(themePreference());
    window.addEventListener(MEDIA_THEME_EVENT, sync);
    return () => window.removeEventListener(MEDIA_THEME_EVENT, sync);
  }, []);
  return <SettingsOptions label="Theme" value={pref} onChange={value => { setPref(value); saveThemePreference(value); }} options={[
    { value: 'dark', label: 'Dark', hint: 'Always dark', icon: <Moon size={16} /> },
    { value: 'light', label: 'Light', hint: 'Always light', icon: <Sun size={16} /> },
    { value: 'system', label: 'Match system', hint: 'Follows this device', split: true },
  ]} />;
}

function TitleArtwork() {
  const [style, setStyle] = useCardStyle();
  return <SettingsOptions label="Title artwork" value={style} onChange={setStyle} options={[
    { value: 'poster', label: 'Posters', hint: 'Tall artwork', icon: <span className="rs-user-settings-shape is-poster" /> },
    { value: 'thumbnail', label: 'Thumbnails', hint: 'Wide artwork', icon: <span className="rs-user-settings-shape is-thumb" /> },
  ]} />;
}

function PinLock() {
  const appLock = useAppLock();
  const { libraries } = useLibrary();
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  const [scope, setScope] = useState(appLock.scope || 'app');
  const [picked, setPicked] = useState(appLock.libraryIds || []);
  useEffect(() => { setScope(appLock.scope || 'app'); setPicked(appLock.libraryIds || []); }, [appLock.scope, appLock.libraryIds]);

  async function save(next) {
    setBusy(true);
    try {
      await setAppLockSettings(next);
      await appLock.refresh();
      toast({ title: 'PIN lock updated', tone: 'success' });
    } catch (err) { toast({ title: 'Could not update the PIN lock', description: err.message, tone: 'danger' }); }
    finally { setBusy(false); }
  }
  const togglePicked = id => {
    const next = picked.includes(id) ? picked.filter(x => x !== id) : [...picked, id];
    setPicked(next);
    if (next.length) save({ scope: 'libraries', libraryIds: next });
  };

  if (!appLock.hasPin) return <div className="rs-user-settings-row">
    <p className="rs-user-settings-note">Ask for a 6-digit PIN before ReelStash — or only some libraries — can be opened. It locks again when you’re idle.</p>
    <Button onClick={appLock.openSettings}><LockKeyhole size={15} /> Set a PIN</Button>
  </div>;

  return <>
    <div className="rs-user-settings-row">
      <p className="rs-user-settings-note">PIN lock is on{appLock.scope === 'libraries' ? ` for ${appLock.libraryIds.length} ${appLock.libraryIds.length === 1 ? 'library' : 'libraries'}` : ' for the whole app'}.</p>
      <span className="rs-user-settings-actions">
        <Button variant="secondary" onClick={appLock.lockNow}>Lock now</Button>
        <Button variant="secondary" onClick={appLock.openSettings}>Change or turn off…</Button>
      </span>
    </div>
    <fieldset className="rs-user-settings-fieldset" disabled={busy}>
      <legend>Lock</legend>
      <label data-option="app" className="rs-user-settings-radio"><input type="radio" name="lock-scope" checked={scope === 'app'} onChange={() => { setScope('app'); save({ scope: 'app' }); }} /><span><strong>The whole app</strong><small>Nothing opens until the PIN is entered.</small></span></label>
      <label data-option="libraries" className="rs-user-settings-radio"><input type="radio" name="lock-scope" checked={scope === 'libraries'} disabled={!libraries.length} onChange={() => setScope('libraries')} /><span><strong>Only selected libraries</strong><small>Other libraries stay open; the PIN is asked when you switch to a locked one.</small></span></label>
      {scope === 'libraries' && <div className="rs-user-settings-libraries">
        {libraries.map(l => { const Icon = modeIcon(l.mode); return <label key={l.id}><input type="checkbox" checked={picked.includes(l.id)} onChange={() => togglePicked(l.id)} /><Icon size={15} /> {l.name}</label>; })}
        {!picked.length && <small className="rs-user-settings-hint">Pick at least one library to lock.</small>}
      </div>}
    </fieldset>
    <label className="rs-user-settings-select">Lock again after
      <select value={appLock.idleMinutes} disabled={busy} onChange={e => save({ idleMinutes: Number(e.target.value) })}>
        {IDLE_OPTIONS.map(m => <option key={m} value={m}>{idleLabel(m)} idle</option>)}
      </select>
    </label>
  </>;
}

const SUBTITLE_MODES = [
  ['off', 'Off', 'Never turn subtitles on by themselves.'],
  ['foreign', 'When the audio is in another language', 'Show them only if the sound is not in your subtitle language.'],
  ['on', 'Always', 'Show them whenever the title has your language.'],
];

export function PlaybackLanguages() {
  const { toast } = useToast();
  const [prefs, setPrefs] = useState(null);
  const [languages, setLanguages] = useState([]);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let live = true;
    getPreferences().then(p => {
      if (!live) return;
      setLanguages(p.languages || []);
      setPrefs({ audioLanguage: p.audioLanguage || '', subtitleLanguage: p.subtitleLanguage || '', subtitleMode: p.subtitleMode || 'off' });
    }).catch(err => toast({ title: 'Could not load your language settings', description: err.message, tone: 'danger' }));
    return () => { live = false; };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  if (!prefs) return <p className="rs-user-settings-note">Loading…</p>;

  async function save(patch) {
    const next = { ...prefs, ...patch };
    // Turning subtitles on needs a language to show: start from the audio language.
    if (next.subtitleMode !== 'off' && !next.subtitleLanguage) next.subtitleLanguage = next.audioLanguage || 'en';
    setPrefs(next);
    setBusy(true);
    try {
      await updatePreferences(next);
      resetPreferenceCache();
      toast({ title: 'Language settings saved', description: 'They apply the next time you open a title.', tone: 'success' });
    } catch (err) {
      setPrefs(prefs);
      toast({ title: 'Could not save', description: err.message, tone: 'danger' });
    } finally { setBusy(false); }
  }
  const options = languages.map(l => <option key={l.code} value={l.code}>{l.label}</option>);
  return <>
    <label className="rs-user-settings-select">Audio language
      <select value={prefs.audioLanguage} disabled={busy} onChange={e => save({ audioLanguage: e.target.value })}>
        <option value="">The file’s own default</option>{options}
      </select>
    </label>
    <fieldset className="rs-user-settings-fieldset" disabled={busy}>
      <legend>Subtitles</legend>
      {SUBTITLE_MODES.map(([value, label, hint]) => <label key={value} data-option={value} className="rs-user-settings-radio"><input type="radio" name="subtitle-mode" checked={prefs.subtitleMode === value} onChange={() => save({ subtitleMode: value })} /><span><strong>{label}</strong><small>{hint}</small></span></label>)}
    </fieldset>
    <label className="rs-user-settings-select">Subtitle language
      <select value={prefs.subtitleLanguage} disabled={busy || prefs.subtitleMode === 'off'} onChange={e => save({ subtitleLanguage: e.target.value })}>
        {prefs.subtitleMode === 'off' && <option value="">—</option>}{options}
      </select>
    </label>
  </>;
}

// Only accounts that sign in with a ReelStash password have one to change; the rest sign in elsewhere.
export function ChangePassword() {
  const { toast } = useToast();
  const [eligible, setEligible] = useState(false);
  const [form, setForm] = useState({ current: '', password: '', confirm: '' });
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const set = patch => setForm(current => ({ ...current, ...patch }));
  useEffect(() => { getSession().then(me => setEligible(!!me?.canChangePassword)).catch(() => {}); }, []);
  if (!eligible) return null;

  async function submit(event) {
    event.preventDefault();
    if (form.password !== form.confirm) { setError('The two new passwords do not match.'); return; }
    setBusy(true);
    setError('');
    try {
      await changeMyPassword(form.current, form.password);
      setForm({ current: '', password: '', confirm: '' });
      toast({ title: 'Password changed', description: 'You were signed out of your other devices.', tone: 'success' });
    } catch (err) { setError(err.message || 'Could not change the password'); }
    finally { setBusy(false); }
  }

  return <Section title="Password" description="Change the password you sign in with. Other devices are signed out.">
    <form className="rs-user-settings-form" onSubmit={submit}>
      {error && <p role="alert" className="rs-user-settings-error">{error}</p>}
      <label>Current password <Input type="password" autoComplete="current-password" value={form.current} onChange={e => set({ current: e.target.value })} required /></label>
      <label>New password <Input type="password" autoComplete="new-password" minLength={8} value={form.password} onChange={e => set({ password: e.target.value })} required /></label>
      <label>Confirm new password <Input type="password" autoComplete="new-password" value={form.confirm} onChange={e => set({ confirm: e.target.value })} required /></label>
      <div><Button type="submit" size="sm" loading={busy}>Change password</Button></div>
    </form>
  </Section>;
}

function Devices() {
  const [refreshKey, setRefreshKey] = useState(0);
  return <Section title="Devices" description="ReelStash apps signed in to your account. To sign in a new one, enter the code it shows.">
    <DeviceList refreshKey={refreshKey} />
    <LinkDeviceForm onLinked={() => setRefreshKey(k => k + 1)} />
  </Section>;
}

function LibraryData() {
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  async function download() {
    setBusy(true);
    try {
      const data = await exportLibrary();
      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = 'reelstash-library.json';
      link.click();
      URL.revokeObjectURL(url);
    } catch (err) { toast({ title: 'Could not export', description: err.message, tone: 'danger' }); }
    finally { setBusy(false); }
  }
  async function onFile(event) {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (!file) return;
    setBusy(true);
    try {
      const data = JSON.parse(await file.text());
      const result = await importLibrary(data);
      toast({ title: 'Library data imported', description: result.skipped ? `${result.skipped} titles were not on this server, so those rows were skipped.` : 'Favorites, ratings, history and playlists were merged.', tone: 'success' });
    } catch (err) { toast({ title: 'Could not import', description: err.message, tone: 'danger' }); }
    finally { setBusy(false); }
  }
  return <Section title="Your library data" description="Download favorites, ratings, watch history, Watch later and playlists, or merge a file from another ReelStash server. Titles are matched by the id stored in each video's NFO.">
    <div className="rs-user-settings-form">
      <div><Button type="button" size="sm" loading={busy} onClick={download}>Download</Button></div>
      <label>Import a file <input type="file" accept="application/json,.json" disabled={busy} onChange={onFile} /></label>
    </div>
  </Section>;
}

export default function UserSettings() {
  return <div className="rs-page rs-user-settings">
    <header className="rs-page-header"><div><p className="rs-eyebrow">Your account</p><h1>Settings</h1><p>These apply only to you.</p></div></header>
    <Section title="Default library" description="The library ReelStash opens on. You can still switch any time from the bottom of the sidebar."><DefaultLibrary /></Section>
    <Section title="Theme"><Theme /></Section>
    <Section title="Title artwork" description="Posters or thumbnails on title cards, on every page."><TitleArtwork /></Section>
    <Section title="Audio and subtitles" description="Which language to play in, and when to show subtitles. You can still change both from the player."><PlaybackLanguages /></Section>
    <Section title="PIN lock" description="Keep the app, or just some libraries, behind a PIN on this account."><PinLock /></Section>
    <LibraryData />
    <Devices />
    <ChangePassword />
  </div>;
}
