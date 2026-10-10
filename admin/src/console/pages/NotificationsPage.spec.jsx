import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../../api', () => ({ getWebhooks: vi.fn(), getWebhookDeliveries: vi.fn(), createWebhook: vi.fn(), updateWebhook: vi.fn(), deleteWebhook: vi.fn(), testWebhook: vi.fn() }));
import { deleteWebhook, getWebhookDeliveries, getWebhooks } from '../../api';
import { renderConsole } from '../testing';
import { NotificationsPage } from './NotificationsPage';

beforeEach(() => {
  vi.clearAllMocks();
  getWebhooks.mockResolvedValue({ kinds: [{ kind: 'job.failed', label: 'Job failed' }], items: [{ id: 4, name: 'Discord', url: 'https://discord.com/api/webhooks/1/a', events: [], enabled: true, hasSecret: false }] });
  getWebhookDeliveries.mockResolvedValue({ items: [
    { id: 2, at: new Date().toISOString(), kind: 'job.failed', error: 'connection refused', durationMs: 31 },
    { id: 1, at: new Date().toISOString(), kind: 'scan.completed', status: 204, durationMs: 12 },
  ] });
  deleteWebhook.mockResolvedValue({});
});

describe('NotificationsPage', () => {
  it('shows the delivery history of a webhook: what, result and how long', async () => {
    renderConsole(<NotificationsPage />, { route: '/admin/notifications' });
    await userEvent.click(await screen.findByRole('button', { name: 'Actions for Discord' }));
    await userEvent.click(await screen.findByRole('menuitem', { name: /Delivery history/ }));
    const drawer = await screen.findByRole('dialog');
    expect(getWebhookDeliveries).toHaveBeenCalledWith(4);
    expect(await within(drawer).findByText(/connection refused/)).toBeInTheDocument();
    expect(within(drawer).getByText('HTTP 204')).toBeInTheDocument();
    expect(within(drawer).getByText('31 ms')).toBeInTheDocument();
  });

  it('records the reason when a webhook is removed', async () => {
    renderConsole(<NotificationsPage />, { route: '/admin/notifications' });
    await userEvent.click(await screen.findByRole('button', { name: 'Actions for Discord' }));
    await userEvent.click(await screen.findByRole('menuitem', { name: /Remove/ }));
    const dialog = await screen.findByRole('alertdialog');
    await userEvent.type(within(dialog).getByLabelText(/Reason/), 'channel archived');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Remove webhook' }));
    await vi.waitFor(() => expect(deleteWebhook).toHaveBeenCalledWith(4, { reason: 'channel archived' }));
  });
});
