import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Button, Input } from '@spatika/react';
import { Check, Clapperboard, FolderSearch, Lock, Rocket } from 'lucide-react';
import { checkSetupPath, completeSetup, createLibrary, createStorageLocation, getLibraries, getScanJobs, getSetupStatus, getStorageLocations, saveProviderConfig, startScan, testProvider } from '../../api';
import { mediaHref } from '../../appLinks';
import { SETUP_MODES, SETUP_STEPS, diskNameFromPath, folderProblem, folderSummary, modeInfo, scanPercent, scanSummary } from '../../setupFlow';
import { Callout, Field, PageHeader, PageStack, Panel, ProgressBar, Status } from '../components';
import { describeError } from '../lib/errors';
import { useNotify } from '../lib/notify';

const MODE_ICON = { entertainment: Clapperboard, private: Lock };
const POLL_MS = 1500;

function Stepper({ index }) {
  return <ol className="console-stepper" aria-label="Setup progress">
    {SETUP_STEPS.map((step, i) => <li key={step.id} data-state={i === index ? 'current' : i < index ? 'done' : 'todo'} aria-current={i === index ? 'step' : undefined}>
      <span className="console-stepper__n">{i < index ? <Check size={12} aria-hidden="true" /> : i + 1}</span>{step.title}
    </li>)}
  </ol>;
}

function ModeStep({ mode, setMode, name, setName }) {
  return <div className="console-form">
    <p className="console-muted console-small">This decides the words and pages ReelStash uses. You can add more libraries of either kind later.</p>
    <div className="console-modes" role="radiogroup" aria-label="Library type">
      {Object.entries(SETUP_MODES).map(([id, info]) => { const Icon = MODE_ICON[id]; return <button key={id} type="button" role="radio" aria-checked={mode === id} className="console-mode" onClick={() => { if (name === modeInfo(mode).defaultName) setName(info.defaultName); setMode(id); }}>
        <span className="console-mode__head"><Icon size={16} aria-hidden="true" /><strong>{info.label}</strong></span><small>{info.blurb}</small>
      </button>; })}
    </div>
    <Field label="Library name" error={name.trim() ? null : 'Give the library a name.'}>{props => <Input {...props} value={name} onChange={e => setName(e.target.value)} maxLength={80} />}</Field>
  </div>;
}

function FolderStep({ path, setPath, check, setCheck }) {
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState('');
  async function run(event) {
    event?.preventDefault();
    setBusy(true); setProblem('');
    try { setCheck(await checkSetupPath(path.trim())); }
    catch (err) { setCheck(null); setProblem(describeError('check that folder', err)); }
    finally { setBusy(false); }
  }
  const blocked = check ? folderProblem(check) : '';
  return <form className="console-form" onSubmit={run} noValidate>
    <p className="console-muted console-small">Enter the folder as ReelStash sees it. With Docker that is the path <em>inside</em> the container, the right-hand side of the volume in your compose file, such as <code className="console-code">/media</code>.</p>
    <div className="console-inline-form console-inline-form--two">
      <Field label="Media folder">{props => <Input {...props} value={path} placeholder="/media" spellCheck={false} onChange={e => { setPath(e.target.value); setCheck(null); setProblem(''); }} />}</Field>
      <div className="console-inline-form__actions"><Button type="submit" variant="outline" disabled={!path.trim() || busy} leadingIcon={<FolderSearch size={14} />}>{busy ? 'Checking…' : 'Check folder'}</Button></div>
    </div>
    {problem && <p className="console-field__error" role="alert">{problem}</p>}
    {check && (blocked ? <p className="console-field__error" role="alert">{blocked}</p>
      : <Callout icon={<Check />} title={folderSummary(check)}>{check.existingName ? `This folder is already added as “${check.existingName}” and will be reused.` : undefined}
        {check.samples?.length > 0 && <ul className="console-plain-list">{check.samples.map(s => <li key={s}><code className="console-code">{s}</code></li>)}</ul>}</Callout>)}
  </form>;
}

function MetadataStep({ mode, token, setToken, result, setResult }) {
  const notify = useNotify();
  const info = modeInfo(mode).provider;
  const [busy, setBusy] = useState(false);
  if (info.plugin) return <div className="console-form"><p className="console-muted console-small">Posters, descriptions and performers for private videos come from a metadata provider plugin. Install one under <Link className="console-link" to="/admin/plugins">Plugins</Link> and enable it for this library under Libraries. You can skip this and do it later.</p></div>;
  async function test() {
    setBusy(true); setResult(null);
    try {
      await saveProviderConfig(info.id, { token: token.trim() });
      const res = await testProvider(info.id);
      setResult(res.ok ? { ok: true } : { ok: false, error: res.error });
      if (res.ok) notify.success(`${info.name} connected`);
    } catch (err) { setResult({ ok: false, error: describeError('save the key', err) }); }
    finally { setBusy(false); }
  }
  return <div className="console-form">
    <p className="console-muted console-small">With a {info.name} key, ReelStash can look up posters, descriptions and {mode === 'private' ? 'performers' : 'cast'} for your videos. {info.help} You can skip this and add it later under Providers.</p>
    <Field label={info.keyLabel}>{props => <Input {...props} type="password" autoComplete="off" value={token} placeholder="Paste it here" onChange={e => { setToken(e.target.value); setResult(null); }} />}</Field>
    <div><Button type="button" variant="outline" disabled={!token.trim() || busy} onClick={test}>{busy ? 'Testing…' : 'Save and test'}</Button></div>
    {result?.ok && <Status tone="ok">{info.name} accepted the key.</Status>}
    {result && !result.ok && <p className="console-field__error" role="alert">{result.error || 'The key was not accepted.'}</p>}
  </div>;
}

function ScanStep({ state, onRetry, onOpenMedia, onOpenAdmin }) {
  const { phase, scan, error, name } = state;
  const percent = scanPercent(scan);
  const done = phase === 'done';
  return <div className="console-form">
    {error ? <><p className="console-field__error" role="alert">{error}</p><div><Button onClick={onRetry}>Try again</Button></div></> : <>
      <p className="console-muted console-small">{done ? `“${name}” is ready.` : 'Creating the library and reading your videos. You can leave this page; the scan keeps going.'}</p>
      <ProgressBar percent={done ? 100 : percent} label="First scan" />
      <p role="status" className="console-small">{phase === 'working' ? 'Creating the library…' : scanSummary(scan)}</p>
      {done && <div className="console-actions-row"><Button onClick={onOpenMedia} leadingIcon={<Rocket size={14} />}>Open ReelStash</Button><Button variant="outline" onClick={onOpenAdmin}>Go to the dashboard</Button></div>}
    </>}
  </div>;
}

/** First-run wizard: library type → media folder → metadata key → library created and first scan run. */
export function SetupPage() {
  const navigate = useNavigate();
  const [index, setIndex] = useState(0);
  const [mode, setMode] = useState('entertainment');
  const [name, setName] = useState(modeInfo('entertainment').defaultName);
  const [path, setPath] = useState('');
  const [check, setCheck] = useState(null);
  const [token, setToken] = useState('');
  const [tokenResult, setTokenResult] = useState(null);
  const [run, setRun] = useState({ phase: 'idle', scan: null, error: '', name: '' });
  const [status, setStatus] = useState(null);
  const started = useRef(false);
  const timer = useRef(null);
  const step = SETUP_STEPS[index].id;

  useEffect(() => { getSetupStatus().then(setStatus).catch(() => {}); }, []);
  useEffect(() => () => clearTimeout(timer.current), []);

  const poll = useCallback((jobId, locationId) => {
    getScanJobs().then(result => {
      const scans = result.items || [];
      const scan = scans.find(s => s.backgroundJobId === jobId) || scans.find(s => s.storageLocationId === locationId);
      if (scan?.completedAt) { completeSetup().catch(() => {}); setRun(r => ({ ...r, phase: 'done', scan })); return; }
      setRun(r => ({ ...r, phase: 'scanning', scan }));
      timer.current = setTimeout(() => poll(jobId, locationId), POLL_MS);
    }).catch(() => { timer.current = setTimeout(() => poll(jobId, locationId), POLL_MS * 2); });
  }, []);

  const start = useCallback(async () => {
    setRun({ phase: 'working', scan: null, error: '', name });
    try {
      const disks = (await getStorageLocations()).items || [];
      let locationId = check.existingLocationId;
      if (!locationId) {
        const base = diskNameFromPath(check.path);
        let diskName = base;
        for (let n = 2; disks.some(d => d.name === diskName); n += 1) diskName = `${base} ${n}`;
        locationId = (await createStorageLocation({ name: diskName, type: 'LOCAL', mountPath: check.path })).id;
      }
      const libraries = (await getLibraries()).items || [];
      const existing = libraries.find(l => l.name === name && l.roots?.some(r => r.storageLocationId === locationId));
      if (!existing) await createLibrary({ name, mode, defaultAccess: true, roots: [{ storageLocationId: locationId, path: '' }], ...(tokenResult?.ok ? { providers: [modeInfo(mode).provider.id] } : {}) });
      let jobId;
      try { jobId = (await startScan(locationId)).backgroundJobId; }
      catch (err) { if (err.status !== 409) throw err; } // already scanning: follow that scan
      poll(jobId, locationId);
    } catch (err) { setRun(r => ({ ...r, phase: 'idle', error: describeError('set up the library', err) })); }
  }, [check, name, mode, tokenResult, poll]);

  useEffect(() => { if (step === 'scan' && !started.current) { started.current = true; start(); } }, [step, start]);

  const canNext = step === 'mode' ? name.trim().length > 0 : step === 'folder' ? check && !folderProblem(check) : true;
  const nextLabel = step === 'metadata' ? (tokenResult?.ok ? 'Create library and scan' : 'Skip and create library') : 'Next';

  return <PageStack>
    <PageHeader title="Set up ReelStash" subtitle="Four quick steps: choose a library type, point at your videos, add an optional metadata key, and run the first scan." />
    {status?.completed && index === 0 && <Callout icon={<Check />} title="You already have a library">This adds another one.</Callout>}
    <Stepper index={index} />
    <Panel title={SETUP_STEPS[index].title === 'Library type' ? 'What will you keep here?' : step === 'folder' ? 'Where are your videos?' : step === 'metadata' ? 'Add artwork and details (optional)' : run.phase === 'done' ? 'You’re all set' : 'Setting up your library'}
      footer={step !== 'scan' ? <><Button variant="ghost" onClick={() => (index === 0 ? navigate('/admin/storage') : setIndex(index - 1))}>{index === 0 ? 'Skip setup' : 'Back'}</Button><Button disabled={!canNext} onClick={() => setIndex(index + 1)}>{nextLabel}</Button></> : undefined}>
      {step === 'mode' && <ModeStep mode={mode} setMode={setMode} name={name} setName={setName} />}
      {step === 'folder' && <FolderStep path={path} setPath={setPath} check={check} setCheck={setCheck} />}
      {step === 'metadata' && <MetadataStep mode={mode} token={token} setToken={setToken} result={tokenResult} setResult={setTokenResult} />}
      {step === 'scan' && <ScanStep state={run} onRetry={() => { started.current = false; setRun({ phase: 'idle', scan: null, error: '', name }); start(); }} onOpenMedia={() => window.location.assign(mediaHref('/'))} onOpenAdmin={() => navigate('/admin')} />}
    </Panel>
  </PageStack>;
}
