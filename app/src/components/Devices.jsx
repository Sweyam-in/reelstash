import React, { useEffect, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { Button, Input, useToast } from '../ui';
import { ArrowLeft, ArrowRight, Check, CheckCircle2, KeyRound, LockKeyhole, MonitorSmartphone, ShieldCheck, Smartphone, Tablet, Tv } from 'lucide-react';
import { approveDevicePairing, getDevices, lookupDevicePairing, revokeDevice } from '../api';

// The ReelStash apps (Android now; iOS and TV later) sign in by pairing: the app shows a code,
// and someone already signed in here enters it. The app then holds its own session, listed
// under Settings → Devices where it can be signed out.

const PLATFORM_ICONS = { android: Smartphone, androidtv: Tv, ios: Smartphone, ipados: Tablet, tvos: Tv };
export const platformIcon = platform => PLATFORM_ICONS[platform] || MonitorSmartphone;

// Codes are 8 letters/digits shown as XXXX-XXXX; accept them typed any way.
export function formatPairingCode(raw) {
  const clean = String(raw || '').toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 8);
  return clean.length > 4 ? `${clean.slice(0, 4)}-${clean.slice(4)}` : clean;
}

export function LinkDeviceForm({ onLinked, standalone = false }) {
  const { toast } = useToast();
  const [params] = useSearchParams();
  const [code, setCode] = useState(formatPairingCode(params.get('code')));
  const [device, setDevice] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [linkedName, setLinkedName] = useState('');

  function reset() {
    setDevice(null);
    setLinkedName('');
    setCode('');
    setError('');
  }

  async function check(event) {
    event.preventDefault();
    setBusy(true);
    setError('');
    try { setDevice(await lookupDevicePairing(code)); } catch (err) { setError(err.message || 'That code did not work'); } finally { setBusy(false); }
  }

  async function approve() {
    setBusy(true);
    setError('');
    try {
      await approveDevicePairing(code);
      if (!standalone) toast({ title: 'Device signed in', description: `${device.deviceName} is now signed in to your account.`, tone: 'success' });
      setLinkedName(device.deviceName);
      setDevice(null);
      setCode('');
      onLinked?.();
    } catch (err) { setError(err.message || 'Could not sign the device in'); } finally { setBusy(false); }
  }

  if (standalone && linkedName) return <div className="rs-link-result" role="status">
    <span className="rs-link-result-icon"><Check size={30} strokeWidth={2.5} /></span>
    <p className="rs-link-step">All set</p>
    <h2>{linkedName} is connected</h2>
    <p>Your device is signed in. You can return to the ReelStash app and start watching.</p>
    <div className="rs-link-actions"><Button onClick={reset}>Link another device</Button><Link className="rs-link-text-action" to="/">Go to your library <ArrowRight size={16} /></Link></div>
  </div>;

  if (device) {
    const Icon = platformIcon(device.platform);
    return <div className={standalone ? 'rs-link-stage' : 'rs-user-settings-body'}>
      {standalone && <><div className="rs-link-stage-top"><span className="rs-link-stage-icon"><ShieldCheck size={21} /></span><span className="rs-link-step">Step 2 of 2</span></div><h2>Confirm this device</h2><p className="rs-link-stage-copy">Check that this is the device showing your code before giving it access to your account.</p></>}
      {error && <p role="alert" className="rs-user-settings-error">{error}</p>}
      {standalone ? <div className="rs-link-device-preview"><span className="rs-link-device-preview-icon"><Icon size={28} /></span><span><small>DEVICE REQUESTING ACCESS</small><strong>{device.deviceName}</strong><em>{code}</em></span><CheckCircle2 size={20} className="rs-link-device-preview-check" /></div> : <p className="rs-user-settings-note rs-link-device-who"><Icon size={18} /> Sign in <strong>{device.deviceName}</strong> as you?</p>}
      <div className={standalone ? 'rs-link-actions' : 'rs-user-settings-actions'}>
        <Button onClick={approve} loading={busy}>{standalone ? 'Approve and link device' : 'Sign it in'}</Button>
        <Button variant="ghost" onClick={() => { setDevice(null); setError(''); }} disabled={busy}>{standalone ? 'Use a different code' : 'Cancel'}</Button>
      </div>
      {standalone && <p className="rs-link-caution"><LockKeyhole size={15} /> Don’t recognize this device? Go back without approving it.</p>}
    </div>;
  }
  return <form className={standalone ? 'rs-link-stage rs-link-form' : 'rs-user-settings-form'} onSubmit={check}>
    {standalone && <><div className="rs-link-stage-top"><span className="rs-link-stage-icon"><KeyRound size={21} /></span><span className="rs-link-step">Step 1 of 2</span></div><h2>Enter your device code</h2><p className="rs-link-stage-copy">Open ReelStash on the device you want to connect. Enter the 8-character code you see there.</p></>}
    {error && <p role="alert" className="rs-user-settings-error">{error}</p>}
    <label>Code shown on your device <Input className="rs-link-device-code" value={code} onChange={e => { setCode(formatPairingCode(e.target.value)); if (error) setError(''); }} placeholder="XXXX-XXXX" autoComplete="off" autoCapitalize="characters" spellCheck={false} inputMode="text" maxLength={9} required /></label>
    <div><Button type="submit" loading={busy} disabled={code.length !== 9}>{standalone ? <>Continue <ArrowRight size={16} /></> : 'Continue'}</Button></div>
    {standalone && <p className="rs-link-help">The code expires after 10 minutes. If it stops working, request a new one in the app.</p>}
  </form>;
}

export function DeviceList({ refreshKey }) {
  const { toast } = useToast();
  const [devices, setDevices] = useState(null);
  const load = () => getDevices().then(setDevices).catch(() => setDevices([]));
  useEffect(() => { load(); }, [refreshKey]);

  async function signOut(device) {
    try {
      await revokeDevice(device.id);
      setDevices(list => list.filter(d => d.id !== device.id));
      toast({ title: 'Signed out', description: `${device.name} was signed out.`, tone: 'success' });
    } catch (err) { toast({ title: 'Could not sign out', description: err.message, tone: 'danger' }); }
  }

  if (!devices) return null;
  if (!devices.length) return <p className="rs-user-settings-note">No apps are signed in to this account.</p>;
  return <ul className="rs-device-list">
    {devices.map(device => {
      const Icon = platformIcon(device.platform);
      const seen = device.lastSeenAt || device.createdAt;
      return <li key={device.id}>
        <Icon size={18} aria-hidden />
        <div><strong>{device.name}</strong><span>Last used {new Date(seen).toLocaleDateString()}</span></div>
        <Button size="sm" variant="ghost" onClick={() => signOut(device)}>Sign out</Button>
      </li>;
    })}
  </ul>;
}

// /link: where the app sends people to enter its code.
export function LinkDevicePage() {
  return <div className="rs-page rs-link-page">
    <Link to="/settings" className="rs-link-back"><ArrowLeft size={16} /> Back to settings</Link>
    <div className="rs-link-layout">
      <header className="rs-link-intro"><p className="rs-link-kicker"><span /> REELSTASH CONNECT</p><h1>Bring your library to another screen.</h1><p>Link your phone or TV to your ReelStash account in two quick steps.</p>
        <ol className="rs-link-steps"><li><span>01</span><div><strong>Find your code</strong><p>Open the ReelStash app on the device and choose to connect with a code.</p></div></li><li><span>02</span><div><strong>Confirm it here</strong><p>Enter the code and check the device name before approving access.</p></div></li></ol>
        <div className="rs-link-security"><ShieldCheck size={20} /><p><strong>Your account stays in control.</strong><br />You can sign out linked devices any time in Settings.</p></div>
      </header>
      <div className="rs-link-card"><LinkDeviceForm standalone /></div>
    </div>
  </div>;
}
