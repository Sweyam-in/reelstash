import React, { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Button, Input, Switch } from '@spatika/react';
import { BellRing, History, Pencil, Plus, Send, Trash2 } from 'lucide-react';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@spatika/react';
import { createWebhook, deleteWebhook, getWebhookDeliveries, getWebhooks, testWebhook, updateWebhook } from '../../api';
import { DetailDrawer, EmptyState, ErrorState, Field, FormDialog, MoreMenu, PageHeader, PageStack, Panel, SkeletonRows, Status, Tag, useConfirm } from '../components';
import { describeError } from '../lib/errors';
import { timeAgo } from '../lib/format';
import { useNotify } from '../lib/notify';
import { useResource } from '../lib/useResource';

const blank = { name: '', url: '', secret: '', events: [], clearSecret: false };

function WebhookDialog({ open, onOpenChange, kinds, hook, onSaved }) {
  const notify = useNotify();
  const initial = hook ? { ...blank, name: hook.name, url: hook.url, events: hook.events } : blank;
  const [form, setForm] = useState(initial);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  useEffect(() => { if (open) { setForm(hook ? { ...blank, name: hook.name, url: hook.url, events: hook.events } : blank); setError(null); } }, [open, hook]);
  const set = patch => setForm(c => ({ ...c, ...patch }));
  const toggleEvent = kind => set({ events: form.events.includes(kind) ? form.events.filter(e => e !== kind) : [...form.events, kind] });
  const dirty = JSON.stringify({ ...form, secret: form.secret }) !== JSON.stringify(initial);
  let urlError = null;
  try { const u = new URL(form.url.trim()); if (!/^https?:$/.test(u.protocol)) urlError = 'Use an http:// or https:// address.'; } catch { urlError = form.url.trim() ? 'That doesn’t look like a web address.' : 'Enter the URL to send events to.'; }

  async function save() {
    setBusy(true); setError(null);
    try {
      if (hook) {
        const patch = { name: form.name || undefined, url: form.url.trim(), events: form.events };
        if (form.clearSecret) patch.secret = ''; else if (form.secret) patch.secret = form.secret;
        await updateWebhook(hook.id, patch);
      } else await createWebhook({ name: form.name, url: form.url.trim(), secret: form.secret, events: form.events });
      notify.success(hook ? 'Webhook updated' : 'Webhook added');
      onOpenChange(false);
      onSaved();
    } catch (err) { setError(describeError('save the webhook', err)); }
    finally { setBusy(false); }
  }

  return <FormDialog open={open} onOpenChange={onOpenChange} title={hook ? `Edit ${hook.name}` : 'Add webhook'} description="ReelStash posts a JSON message to this address when something happens. Discord and Slack incoming-webhook URLs work as they are."
    submitLabel={hook ? 'Save changes' : 'Add webhook'} dirty={dirty} canSubmit={!urlError} submitting={busy} error={error} onSubmit={save}>
    <Field label="Name" optional>{props => <Input {...props} value={form.name} onChange={e => set({ name: e.target.value })} placeholder="Home Assistant" maxLength={80} />}</Field>
    <Field label="URL" error={urlError}>{props => <Input {...props} type="url" value={form.url} onChange={e => set({ url: e.target.value })} placeholder="https://example.com/hooks/reelstash" />}</Field>
    <Field label="Signing secret" optional hint={hook?.hasSecret ? 'Leave blank to keep the current secret.' : 'Used to sign each request so the receiver can verify it.'}>{props => <Input {...props} type="password" autoComplete="off" value={form.secret} onChange={e => set({ secret: e.target.value, clearSecret: false })} />}</Field>
    {hook?.hasSecret && <label className="console-inline-check"><input type="checkbox" checked={form.clearSecret} onChange={e => set({ clearSecret: e.target.checked, secret: '' })} /> Remove the signing secret</label>}
    <fieldset className="console-fieldset"><legend>Send when <span className="console-muted console-small">none ticked = everything</span></legend>
      <div className="console-checks">{kinds.map(kind => <label key={kind.kind} className="console-check"><input type="checkbox" checked={form.events.includes(kind.kind)} onChange={() => toggleEvent(kind.kind)} /><span className="console-check__label">{kind.label}</span></label>)}</div>
    </fieldset>
  </FormDialog>;
}

// The latest 50 deliveries of one webhook, newest first.
function DeliveryDrawer({ hook, onClose }) {
  const log = useResource(() => getWebhookDeliveries(hook.id), [hook.id]);
  const items = log.data?.items || [];
  return <DetailDrawer open onClose={onClose} eyebrow="Webhook" title={hook.name} description={`The latest deliveries to ${hook.url}. ReelStash keeps the last 50.`}>
    {log.error && !log.data ? <ErrorState compact title="Couldn't load the deliveries" error={log.error} onRetry={log.reload} />
      : log.loading ? <SkeletonRows rows={4} />
        : items.length === 0 ? <EmptyState compact icon={History} title="Nothing delivered yet" message="Deliveries show up here as events happen, or send a test." />
          : <div className="console-table-responsive"><Table aria-label="Deliveries"><TableHeader><TableRow><TableHead>When</TableHead><TableHead>Event</TableHead><TableHead>Result</TableHead><TableHead className="console-num-cell">Took</TableHead></TableRow></TableHeader>
            <TableBody>{items.map(d => <TableRow key={d.id}>
              <TableCell className="console-nowrap" title={d.at}>{timeAgo(d.at)}</TableCell>
              <TableCell className="console-nowrap">{d.kind}</TableCell>
              <TableCell className="console-wrap">{d.error ? <Status tone="crit">{d.status ? `HTTP ${d.status}: ` : ''}{d.error}</Status> : <Status tone="ok">HTTP {d.status}</Status>}</TableCell>
              <TableCell className="console-num-cell">{d.durationMs} ms</TableCell>
            </TableRow>)}</TableBody></Table></div>}
    <div><Button size="sm" variant="outline" onClick={log.reload}>Refresh</Button></div>
  </DetailDrawer>;
}

function deliveryLine(hook) {
  if (!hook.lastDeliveryAt) return <Status tone="neutral">Not delivered yet</Status>;
  const when = timeAgo(hook.lastDeliveryAt);
  return hook.lastError ? <span title={hook.lastError}><Status tone="crit">Failed {when}</Status></span> : <Status tone="ok">Delivered {when}{hook.lastStatus ? ` (HTTP ${hook.lastStatus})` : ''}</Status>;
}

/** Admin: where ReelStash sends its notifications (scan finished, metadata matched, job failed, ...). */
export function NotificationsPage() {
  const [params, setParams] = useSearchParams();
  const notify = useNotify();
  const confirm = useConfirm();
  const data = useResource(() => getWebhooks(), []);
  const [editing, setEditing] = useState(null); // null closed, {} new, hook
  const [busy, setBusy] = useState(false);
  const [history, setHistory] = useState(null);
  const hooks = data.data?.items || [];
  const kinds = data.data?.kinds || [];
  const kindLabel = kind => kinds.find(k => k.kind === kind)?.label || kind;
  useEffect(() => { if (params.get('new') === '1') { setEditing({}); setParams(p => { const n = new URLSearchParams(p); n.delete('new'); return n; }, { replace: true }); } }, [params, setParams]);

  async function toggle(hook) {
    try { await updateWebhook(hook.id, { enabled: !hook.enabled }); notify.success(hook.enabled ? `${hook.name} turned off` : `${hook.name} turned on`); await data.reload(); }
    catch (err) { notify.error('update the webhook', err); }
  }
  async function test(hook) {
    setBusy(true);
    try { const r = await testWebhook(hook.id); if (r.ok) notify.success('Test delivered', `${hook.name} answered ${r.status}.`); else notify.error('deliver the test', new Error(r.error)); await data.reload(); }
    catch (err) { notify.error('send the test', err); }
    finally { setBusy(false); }
  }
  async function remove(hook) {
    const ok = await confirm({ title: `Remove ${hook.name}?`, consequence: 'ReelStash stops sending events to this address. Nothing already delivered is affected.', affects: hook.url, reversible: 'You can add it again.', confirmLabel: 'Remove webhook', tone: 'danger', reason: { label: 'Reason', placeholder: 'Why are you doing this? Recorded in the audit log.', required: false } });
    if (!ok) return;
    try { await deleteWebhook(hook.id, { reason: ok.reason }); notify.success('Webhook removed'); await data.reload(); }
    catch (err) { notify.error('remove the webhook', err); }
  }

  return <PageStack>
    <PageHeader title="Notifications" subtitle="Admins see scan, matching and failed-job notices in the app. Add a webhook to send the same events to another service."
      actions={<Button onClick={() => setEditing({})} leadingIcon={<Plus size={14} />}>Add webhook</Button>} />
    <Panel title="Webhooks" bodyless>
      {data.error && !data.data ? <ErrorState title="Couldn't load webhooks" error={data.error} onRetry={data.reload} />
        : data.loading ? <SkeletonRows rows={2} />
          : hooks.length === 0 ? <EmptyState icon={BellRing} title="No webhooks yet" message="Add one to send events to Discord, Slack, Home Assistant or your own service." action={<Button size="sm" onClick={() => setEditing({})}>Add webhook</Button>} />
            : <div className="console-table-responsive"><Table className="console-stack-table" data-actions>
              <TableHeader><TableRow><TableHead>Webhook</TableHead><TableHead>Events</TableHead><TableHead>Last delivery</TableHead><TableHead>On</TableHead><TableHead><span className="console-sr-only">Actions</span></TableHead></TableRow></TableHeader>
              <TableBody>{hooks.map(hook => <TableRow key={hook.id}>
                <TableCell className="console-wrap"><strong>{hook.name}</strong><div className="console-row__meta console-truncate" title={hook.url}>{hook.url}</div></TableCell>
                <TableCell className="console-wrap"><div className="console-tags">{hook.events.length ? hook.events.map(k => <Tag key={k}>{kindLabel(k)}</Tag>) : <Tag>All events</Tag>}{hook.hasSecret && <Tag>Signed</Tag>}</div></TableCell>
                <TableCell>{deliveryLine(hook)}</TableCell>
                <TableCell><Switch checked={hook.enabled} onCheckedChange={() => toggle(hook)} aria-label={`${hook.name} on`} /></TableCell>
                <TableCell><MoreMenu iconOnly label={`Actions for ${hook.name}`} items={[{ label: 'Delivery history', icon: History, onSelect: () => setHistory(hook) }, { label: 'Send a test', icon: Send, disabled: busy, onSelect: () => test(hook) }, { label: 'Edit…', icon: Pencil, onSelect: () => setEditing(hook) }, 'separator', { label: 'Remove…', icon: Trash2, danger: true, onSelect: () => remove(hook) }]} /></TableCell>
              </TableRow>)}</TableBody>
            </Table></div>}
    </Panel>
    {history && <DeliveryDrawer hook={history} onClose={() => setHistory(null)} />}
    <WebhookDialog open={!!editing} onOpenChange={next => { if (!next) setEditing(null); }} kinds={kinds} hook={editing?.id ? editing : null} onSaved={data.reload} />
  </PageStack>;
}
