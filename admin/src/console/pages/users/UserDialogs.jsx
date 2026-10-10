import React, { useEffect, useState } from 'react';
import { Input, NativeSelect } from '@spatika/react';
import { createUser, resetUserPassword } from '../../../api';
import { Field, FormDialog } from '../../components';
import { describeError } from '../../lib/errors';
import { useNotify } from '../../lib/notify';

const blank = { username: '', displayName: '', password: '', role: 'USER' };

export function AddUserDialog({ open, onOpenChange, onAdded }) {
  const notify = useNotify();
  const [form, setForm] = useState(blank);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  useEffect(() => { if (open) { setForm(blank); setError(null); } }, [open]);
  const set = patch => setForm(current => ({ ...current, ...patch }));
  const problems = {
    username: form.username.trim() ? (/^[A-Za-z0-9._@-]+$/.test(form.username.trim()) ? null : 'Use letters, digits and . _ @ - only.') : 'Enter a username.',
    password: form.password.length >= 8 ? null : 'Use at least 8 characters.',
  };
  const valid = !problems.username && !problems.password;

  async function submit() {
    setBusy(true); setError(null);
    try {
      await createUser({ username: form.username.trim(), displayName: form.displayName.trim(), password: form.password, role: form.role });
      notify.success(`${form.username.trim()} added`, 'They can sign in with that username and password.');
      onOpenChange(false);
      onAdded();
    } catch (err) { setError(describeError('add the user', err)); }
    finally { setBusy(false); }
  }

  return <FormDialog open={open} onOpenChange={onOpenChange} title="Add a user" description="Creates an account that signs in with a username and password kept by ReelStash."
    submitLabel="Add user" dirty={form.username !== '' || form.password !== '' || form.displayName !== ''} canSubmit={valid} submitting={busy} error={error} onSubmit={submit}>
    <div className="console-form__grid">
      <Field label="Username" error={problems.username} hint="Letters, digits and . _ @ -">{props => <Input {...props} autoFocus autoComplete="off" value={form.username} onChange={e => set({ username: e.target.value })} />}</Field>
      <Field label="Display name" optional>{props => <Input {...props} autoComplete="off" value={form.displayName} onChange={e => set({ displayName: e.target.value })} />}</Field>
      <Field label="Password" error={problems.password} hint="At least 8 characters. Share it with them privately.">{props => <Input {...props} type="password" autoComplete="new-password" value={form.password} onChange={e => set({ password: e.target.value })} />}</Field>
      <Field label="Role" hint="Administrators open every library and manage everything in this console.">{props => <NativeSelect {...props} value={form.role} onChange={e => set({ role: e.target.value })}><option value="USER">User</option><option value="ADMIN">Administrator</option></NativeSelect>}</Field>
    </div>
  </FormDialog>;
}

export function ResetPasswordDialog({ user, open, onOpenChange }) {
  const notify = useNotify();
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  useEffect(() => { if (open) { setPassword(''); setConfirm(''); setReason(''); setError(null); } }, [open]);
  const problems = {
    password: password.length >= 8 ? null : 'Use at least 8 characters.',
    confirm: confirm === password ? null : 'The two passwords don’t match.',
  };

  async function submit() {
    setBusy(true); setError(null);
    try {
      await resetUserPassword(user.id, password, { reason: reason.trim() });
      notify.success('Password changed', `${user.username} is signed out everywhere and must use the new password.`);
      onOpenChange(false);
    } catch (err) { setError(describeError('change the password', err)); }
    finally { setBusy(false); }
  }

  return <FormDialog open={open} onOpenChange={onOpenChange} title={`Reset password for ${user?.username || 'this user'}`}
    description="They are signed out on every device and need the new password to get back in. This can’t be undone, but you can set another password at any time."
    size="sm" submitLabel="Change password" dirty={password !== ''} canSubmit={!problems.password && !problems.confirm} submitting={busy} error={error} onSubmit={submit}>
    <Field label="New password" error={problems.password} hint="At least 8 characters.">{props => <Input {...props} type="password" autoFocus autoComplete="new-password" value={password} onChange={e => setPassword(e.target.value)} />}</Field>
    <Field label="Repeat the password" error={problems.confirm}>{props => <Input {...props} type="password" autoComplete="new-password" value={confirm} onChange={e => setConfirm(e.target.value)} />}</Field>
    <Field label="Reason" optional hint="Recorded in the audit log.">{props => <Input {...props} value={reason} maxLength={500} onChange={e => setReason(e.target.value)} />}</Field>
  </FormDialog>;
}
