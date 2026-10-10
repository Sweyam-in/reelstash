import React, { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Button, Input, useToast } from '@spatika/react';
import { MonitorSmartphone, Smartphone, Tablet, Tv } from 'lucide-react';
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

export function LinkDeviceForm({ onLinked }) {
  const { toast } = useToast();
  const [params] = useSearchParams();
  const [code, setCode] = useState(formatPairingCode(params.get('code')));
  const [device, setDevice] = useState(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

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
      toast({ title: 'Device signed in', description: `${device.deviceName} is now signed in to your account.`, tone: 'success' });
      setDevice(null);
      setCode('');
      onLinked?.();
    } catch (err) { setError(err.message || 'Could not sign the device in'); } finally { setBusy(false); }
  }

  if (device) {
    const Icon = platformIcon(device.platform);
    return <div className="rs-user-settings-body">
      {error && <p role="alert" className="rs-user-settings-error">{error}</p>}
      <p className="rs-user-settings-note rs-link-device-who"><Icon size={18} /> Sign in <strong>{device.deviceName}</strong> as you?</p>
      <div className="rs-user-settings-actions">
        <Button size="sm" onClick={approve} loading={busy}>Sign it in</Button>
        <Button size="sm" variant="ghost" onClick={() => setDevice(null)} disabled={busy}>Cancel</Button>
      </div>
    </div>;
  }
  return <form className="rs-user-settings-form" onSubmit={check}>
    {error && <p role="alert" className="rs-user-settings-error">{error}</p>}
    <label>Code shown on the device <Input className="rs-link-device-code" value={code} onChange={e => setCode(formatPairingCode(e.target.value))} placeholder="XXXX-XXXX" autoComplete="off" autoCapitalize="characters" spellCheck={false} inputMode="text" required /></label>
    <div><Button type="submit" size="sm" loading={busy} disabled={code.length !== 9}>Continue</Button></div>
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
  return <div className="rs-page rs-user-settings">
    <header className="rs-page-header"><div><p className="rs-eyebrow">Your account</p><h1>Link a device</h1><p>Enter the code shown in the ReelStash app to sign it in as you.</p></div></header>
    <LinkDeviceForm />
  </div>;
}
