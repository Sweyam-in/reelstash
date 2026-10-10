import React, { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { Delete, Lock, LogOut, X } from 'lucide-react';
import { Button, useToast } from '@spatika/react';
import { APP_LOCKED_EVENT, getAppLockStatus, lockApp as lockAppRequest, logout, removeAppLockPin, setAppLockIdleMinutes, setAppLockPin, verifyAppLockPin } from '../api';

const STATUS_POLL_MS = 30_000;
const IDLE_OPTIONS = [5, 15, 30, 60, 120, 240];
const PIN_LENGTH = 6;

const AppLockContext = createContext(null);
// Fired by the lock screen of a library-scoped lock: the library app switches to an open library.
export const LEAVE_LOCKED_LIBRARY_EVENT = 'reelstash:leave-locked-library';
const sameStatus = (a, b) => a.hasPin === b.hasPin && a.locked === b.locked && a.idleMinutes === b.idleMinutes && a.scope === b.scope
  && a.librariesLocked === b.librariesLocked && String(a.libraryIds) === String(b.libraryIds);
export const useAppLock = () => useContext(AppLockContext);

/**
 * Tracks the server-enforced PIN lock. The server is the boundary (it answers 423
 * to everything but /api/me and /api/applock); this provider only decides when to
 * show the lock screen: on load, on any 423, and by polling for idle expiry.
 */
export function AppLockProvider({ children }) {
  // scope 'libraries': only libraryIds lock; the lock screen then shows when one of them is opened
  // (its requests answer 423), never over the whole app.
  const [status, setStatus] = useState({ hasPin: false, locked: false, idleMinutes: 30, scope: 'app', libraryIds: [], librariesLocked: false });
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [unlockNonce, setUnlockNonce] = useState(0);

  const refresh = useCallback(async () => {
    try {
      let next = await getAppLockStatus();
      setStatus(prev => {
        // A library lock that just engaged (idle): refetch, so a locked library on screen asks for the PIN.
        if (next.scope === 'libraries' && next.librariesLocked && !prev.librariesLocked && prev.hasPin) setUnlockNonce(n => n + 1);
        // The server never reports a library lock as `locked`; keep a lock screen raised by a 423 up.
        if (prev.locked && next.scope === 'libraries' && next.librariesLocked) next = { ...next, locked: true };
        return sameStatus(prev, next) ? prev : next;
      });
    } catch { /* signed out or offline: nothing to lock */ }
  }, []);

  useEffect(() => { refresh(); }, [refresh]);
  useEffect(() => {
    const onLocked = () => setStatus(prev => ({ ...prev, hasPin: true, locked: true }));
    window.addEventListener(APP_LOCKED_EVENT, onLocked);
    return () => window.removeEventListener(APP_LOCKED_EVENT, onLocked);
  }, []);
  useEffect(() => {
    if (!status.hasPin || status.locked || (status.scope === 'libraries' && status.librariesLocked)) return undefined;
    const id = setInterval(refresh, STATUS_POLL_MS);
    const onVisible = () => { if (document.visibilityState === 'visible') refresh(); };
    document.addEventListener('visibilitychange', onVisible);
    return () => { clearInterval(id); document.removeEventListener('visibilitychange', onVisible); };
  }, [status.hasPin, status.locked, status.scope, status.librariesLocked, refresh]);

  const lockNow = useCallback(async () => {
    await lockAppRequest().catch(() => {});
    if (status.scope === 'libraries') {
      setStatus(prev => ({ ...prev, librariesLocked: true }));
      setUnlockNonce(n => n + 1); // refetch: a locked library on screen now asks for the PIN
    } else setStatus(prev => ({ ...prev, hasPin: true, locked: true }));
  }, [status.scope]);

  const leaveLibrary = useCallback(() => {
    window.dispatchEvent(new Event(LEAVE_LOCKED_LIBRARY_EVENT));
    setStatus(prev => ({ ...prev, locked: false }));
    setUnlockNonce(n => n + 1);
  }, []);

  const unlock = useCallback(async pin => {
    await verifyAppLockPin(pin);
    setStatus(prev => ({ ...prev, locked: false, librariesLocked: false }));
    setUnlockNonce(n => n + 1);
  }, []);

  const value = useMemo(() => ({ ...status, refresh, lockNow, openSettings: () => setSettingsOpen(true) }), [status, refresh, lockNow]);

  return <AppLockContext.Provider value={value}>
    {/* Remounting on unlock refetches everything that failed with 423 while locked. */}
    <React.Fragment key={unlockNonce}>{children}</React.Fragment>
    {status.hasPin && status.locked && <LockScreen onUnlock={unlock} onLeave={status.scope === 'libraries' ? leaveLibrary : null} />}
    {settingsOpen && !(status.hasPin && status.locked) && <LockSettings status={status} onClose={() => setSettingsOpen(false)} onChanged={refresh} />}
  </AppLockContext.Provider>;
}

function PinEntry({ onComplete, busy, error, autoFocus = true }) {
  const [pin, setPin] = useState('');
  const submit = useRef(onComplete);
  submit.current = onComplete;

  const push = useCallback(digit => setPin(prev => (prev.length < PIN_LENGTH ? prev + digit : prev)), []);
  useEffect(() => {
    if (pin.length !== PIN_LENGTH) return;
    Promise.resolve(submit.current(pin)).finally(() => setPin(''));
  }, [pin]);
  useEffect(() => {
    if (!autoFocus) return undefined;
    const onKey = event => {
      if (busy || event.metaKey || event.ctrlKey || event.altKey) return;
      if (/^\d$/.test(event.key)) push(event.key);
      else if (event.key === 'Backspace') setPin(prev => prev.slice(0, -1));
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [busy, push, autoFocus]);

  return <div className="pin-entry">
    <div className="pin-dots" aria-label={`${pin.length} of ${PIN_LENGTH} digits entered`}>
      {Array.from({ length: PIN_LENGTH }, (_, i) => <span key={i} className={i < pin.length ? 'filled' : ''} />)}
    </div>
    <p className={`pin-error${error ? ' visible' : ''}`} role="alert">{error || ' '}</p>
    <div className="pin-pad">
      {['1', '2', '3', '4', '5', '6', '7', '8', '9'].map(d => <button type="button" key={d} disabled={busy} onClick={() => push(d)}>{d}</button>)}
      <span />
      <button type="button" disabled={busy} onClick={() => push('0')}>0</button>
      <button type="button" aria-label="Delete" disabled={busy} onClick={() => setPin(prev => prev.slice(0, -1))}><Delete size={20} /></button>
    </div>
  </div>;
}

function LockScreen({ onUnlock, onLeave }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const attempt = async pin => {
    setBusy(true);
    setError('');
    try { await onUnlock(pin); } catch (err) { setError(err.message || 'Incorrect PIN'); } finally { setBusy(false); }
  };
  return <div className="lock-screen" role="dialog" aria-modal="true" aria-label="ReelStash is locked">
    <div className="lock-panel">
      <div className="lock-icon"><Lock size={28} /></div>
      <h1>{onLeave ? 'Library locked' : 'Locked'}</h1>
      <p className="lock-subtitle">Enter your PIN to continue</p>
      <PinEntry onComplete={attempt} busy={busy} error={error} />
      {onLeave
        ? <button type="button" className="lock-signout" onClick={onLeave}><X size={14} /> Go to another library</button>
        : <button type="button" className="lock-signout" onClick={() => logout()}><LogOut size={14} /> Sign out</button>}
    </div>
  </div>;
}

function LockSettings({ status, onClose, onChanged }) {
  // step: 'menu' | 'new' | 'confirm' | 'current-change' | 'current-remove'
  const [step, setStep] = useState('menu');
  const [firstPin, setFirstPin] = useState('');
  const [currentPin, setCurrentPin] = useState('');
  const [busy, setBusy] = useState(false);
  const { toast } = useToast();

  const back = message => { setStep('menu'); setFirstPin(''); setCurrentPin(''); if (message) toast({ title: message, tone: 'success' }); };
  const fail = message => toast({ title: message, tone: 'danger' });
  const run = async fn => {
    setBusy(true);
    try { await fn(); } catch (err) { fail(err.message || 'Something went wrong'); } finally { setBusy(false); }
  };

  const prompts = {
    'current-change': 'Enter your current PIN',
    'current-remove': 'Enter your current PIN to turn the lock off',
    new: 'Choose a new 6-digit PIN',
    confirm: 'Enter the PIN again to confirm',
  };

  const onPin = pin => {
    if (step === 'current-change') { setCurrentPin(pin); setStep('new'); return; }
    if (step === 'current-remove') return run(async () => { await removeAppLockPin(pin); await onChanged(); back('PIN lock turned off.'); });
    if (step === 'new') { setFirstPin(pin); setStep('confirm'); return; }
    if (pin !== firstPin) { fail('PINs did not match. Try again.'); setFirstPin(''); setStep('new'); return; }
    return run(async () => { await setAppLockPin(pin, currentPin || undefined); await onChanged(); back(status.hasPin ? 'PIN changed.' : 'PIN lock turned on.'); });
  };

  const changeIdle = event => run(async () => { await setAppLockIdleMinutes(Number(event.target.value)); await onChanged(); toast({ title: 'Auto-lock updated', tone: 'success' }); });

  return <div className="lock-modal-backdrop" onPointerDown={event => { if (event.target === event.currentTarget) onClose(); }}>
    <div className="lock-modal" role="dialog" aria-modal="true" aria-label="PIN lock">
      <button type="button" className="lock-modal-close" aria-label="Close" onClick={onClose}><X size={18} /></button>
      <h2>PIN lock</h2>
      {step === 'menu' ? <>
        <p className="lock-modal-copy">{status.hasPin ? 'A 6-digit PIN is required to open ReelStash after you have been idle.' : 'Require a 6-digit PIN to open ReelStash. It locks automatically when idle, and the server refuses all library requests while locked.'}</p>
        {status.hasPin ? <>
          <label className="lock-modal-field">Auto-lock after
            <select value={status.idleMinutes} onChange={changeIdle} disabled={busy}>
              {IDLE_OPTIONS.map(m => <option key={m} value={m}>{m < 60 ? `${m} minutes` : `${m / 60} hour${m > 60 ? 's' : ''}`}</option>)}
            </select>
          </label>
          <div className="lock-modal-actions">
            <Button variant="outline" onClick={() => setStep('current-change')}>Change PIN</Button>
            <Button variant="outline" onClick={() => setStep('current-remove')}>Turn off</Button>
          </div>
        </> : <div className="lock-modal-actions"><Button onClick={() => setStep('new')}>Set a PIN</Button></div>}
      </> : <>
        <p className="lock-modal-copy">{prompts[step]}</p>
        <PinEntry key={step} onComplete={onPin} busy={busy} />
        <button type="button" className="lock-signout" onClick={() => back()}>Cancel</button>
      </>}
    </div>
  </div>;
}
