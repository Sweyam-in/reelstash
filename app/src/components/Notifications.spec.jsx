import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../api', () => ({ getNotifications: vi.fn(), markNotificationsRead: vi.fn() }));
// Capture the handler the provider registers for the live stream.
const live = { handler: null, name: undefined };
vi.mock('../events', () => ({ useServerEvent: (name, handler) => { live.name = name; live.handler = handler; } }));

import { getNotifications, markNotificationsRead } from '../api';
import { NotificationsProvider, useNotifications } from './Notifications';
import { renderApp } from '../testing/render';

function Probe() {
  const { items, unread, markRead } = useNotifications();
  return <div>
    <span data-testid="unread">{unread}</span>
    <ul>{items.map(n => <li key={n.id}>{n.title}</li>)}</ul>
    <button onClick={markRead}>mark read</button>
  </div>;
}
const renderProvider = enabled => renderApp(<NotificationsProvider enabled={enabled}><Probe /></NotificationsProvider>);

beforeEach(() => {
  vi.clearAllMocks();
  live.handler = null; live.name = undefined;
  getNotifications.mockResolvedValue({ items: [{ id: 1, kind: 'scan.completed', title: 'Scan finished' }], unread: 3 });
  markNotificationsRead.mockResolvedValue({});
});

describe('NotificationsProvider', () => {
  it('loads the latest notifications and the unread count', async () => {
    renderProvider(true);
    expect(await screen.findByText('Scan finished')).toBeInTheDocument();
    expect(screen.getByTestId('unread')).toHaveTextContent('3');
    expect(getNotifications).toHaveBeenCalledWith(15);
  });

  it('does nothing while disabled (e.g. signed out)', async () => {
    renderProvider(false);
    await new Promise(r => setTimeout(r, 20));
    expect(getNotifications).not.toHaveBeenCalled();
    expect(screen.getByTestId('unread')).toHaveTextContent('0');
    expect(live.name).toBeNull();
  });

  it('survives a failed load', async () => {
    getNotifications.mockRejectedValue(new Error('offline'));
    renderProvider(true);
    await waitFor(() => expect(getNotifications).toHaveBeenCalled());
    expect(screen.getByTestId('unread')).toHaveTextContent('0');
  });

  it('subscribes to the live "notification" stream when enabled', async () => {
    renderProvider(true);
    await screen.findByText('Scan finished');
    expect(live.name).toBe('notification');
  });

  it('adds a pushed notification to the top, bumps unread, and toasts it', async () => {
    renderProvider(true);
    await screen.findByText('Scan finished');
    act(() => live.handler({ id: 2, kind: 'metadata.matched', title: 'Matching finished', body: '4 of 5 matched' }));
    const titles = screen.getAllByRole('listitem').map(li => li.textContent);
    expect(titles).toEqual(['Matching finished', 'Scan finished']);
    expect(screen.getByTestId('unread')).toHaveTextContent('4');
    expect(await screen.findAllByText('Matching finished')).not.toHaveLength(0);
  });

  it('does not list the same notification twice', async () => {
    renderProvider(true);
    await screen.findByText('Scan finished');
    act(() => live.handler({ id: 1, kind: 'scan.completed', title: 'Scan finished' }));
    expect(screen.getAllByRole('listitem')).toHaveLength(1);
  });

  it('keeps only the newest 15', async () => {
    getNotifications.mockResolvedValue({ items: Array.from({ length: 15 }, (_, i) => ({ id: i + 1, kind: 'job.completed', title: `N${i + 1}` })), unread: 0 });
    renderProvider(true);
    await screen.findByText('N1');
    act(() => live.handler({ id: 99, kind: 'job.completed', title: 'Newest' }));
    expect(screen.getAllByRole('listitem')).toHaveLength(15);
    expect(screen.queryByText('N15')).not.toBeInTheDocument();
    expect(within(screen.getByRole('list')).getByText('Newest')).toBeInTheDocument();
  });

  it('marks everything read once, then ignores repeat calls', async () => {
    renderProvider(true);
    await screen.findByText('Scan finished');
    await userEvent.click(screen.getByRole('button', { name: 'mark read' }));
    expect(markNotificationsRead).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('unread')).toHaveTextContent('0');
    await userEvent.click(screen.getByRole('button', { name: 'mark read' }));
    expect(markNotificationsRead).toHaveBeenCalledTimes(1);
  });
});
