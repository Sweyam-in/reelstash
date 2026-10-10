import React from 'react';
import { describe, it, expect, beforeEach } from 'vitest';
import { act, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import QueuePanel from './QueuePanel';
import { __resetQueueForTests, startQueue, syncPlaying } from '../queue';
import { renderApp } from '../testing/render';

const t = (id, studio = '') => ({ id, title: `Title ${id}`, studioName: studio });

beforeEach(() => { localStorage.clear(); __resetQueueForTests(); });

describe('QueuePanel', () => {
  it('renders nothing while the queue is empty', () => {
    const { container } = renderApp(<QueuePanel currentId="1" />);
    expect(container.querySelector('.rs-queue')).toBeNull();
  });

  it('lists the queue, highlights the playing title and counts what is left', () => {
    startQueue([t(1, 'Acme'), t(2), t(3)]);
    renderApp(<QueuePanel currentId="1" />);
    const list = screen.getByRole('list');
    const items = within(list).getAllByRole('listitem');
    expect(items).toHaveLength(3);
    expect(items[0]).toHaveClass('is-playing');
    expect(within(items[0]).getByRole('link')).toHaveAttribute('aria-current', 'true');
    expect(within(items[1]).getByRole('link')).not.toHaveAttribute('aria-current');
    expect(screen.getByText('Acme')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: /Up next/ })).toHaveTextContent('2');
  });

  it('links each title to its player page', () => {
    startQueue([t(7)]);
    renderApp(<QueuePanel currentId="7" />);
    expect(screen.getByRole('link', { name: /Title 7/ })).toHaveAttribute('href', '/library/7');
  });

  it('removes a title from the queue', async () => {
    startQueue([t(1), t(2)]);
    renderApp(<QueuePanel currentId="1" />);
    await userEvent.click(screen.getByRole('button', { name: 'Remove Title 2 from the queue' }));
    expect(screen.queryByText('Title 2')).not.toBeInTheDocument();
    expect(screen.getByText('Title 1')).toBeInTheDocument();
  });

  it('clears the whole queue and then disappears', async () => {
    startQueue([t(1), t(2)]);
    const { container } = renderApp(<QueuePanel currentId="1" />);
    await userEvent.click(screen.getByRole('button', { name: /Clear/ }));
    expect(container.querySelector('.rs-queue')).toBeNull();
  });

  it('toggles autoplay', async () => {
    startQueue([t(1), t(2)]);
    renderApp(<QueuePanel currentId="1" />);
    const box = screen.getByRole('checkbox', { name: /Autoplay next/ });
    expect(box).toBeChecked();
    await userEvent.click(box);
    expect(box).not.toBeChecked();
    expect(JSON.parse(localStorage.getItem('reelstash-queue-v1')).autoplay).toBe(false);
  });

  it('follows the title being played', () => {
    startQueue([t(1), t(2), t(3)]);
    renderApp(<QueuePanel currentId="2" />);
    act(() => syncPlaying(2));
    expect(screen.getByRole('heading', { name: /Up next/ })).toHaveTextContent('1');
  });
});
