import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../../api', () => ({ getPluginCatalog: vi.fn(), installFromPluginCatalog: vi.fn(), setPluginCatalogs: vi.fn() }));
import { getPluginCatalog, installFromPluginCatalog, setPluginCatalogs } from '../../api';
import { renderConsole } from '../testing';
import PluginCatalog from './PluginCatalog';

const SOURCE = 'https://plugins.example/dist/index.json';

beforeEach(() => {
  vi.clearAllMocks();
  getPluginCatalog.mockResolvedValue({
    enabled: true,
    sources: [
      { address: SOURCE, items: [
        { source: SOURCE, id: 'tmdb-extra', name: 'Extra metadata', kind: 'provider', latest: '1.2.0', description: 'More details.' },
        { source: SOURCE, id: 'oidc', name: 'Single sign-on', kind: 'auth', latest: '2.0.0', installed: '1.0.0', update: true },
        { source: SOURCE, id: 'done', name: 'Current one', kind: 'provider', latest: '1.0.0', installed: '1.0.0' },
      ] },
      { address: 'https://down.example/index.json', error: 'the server answered HTTP 500', items: [] },
    ],
  });
  installFromPluginCatalog.mockResolvedValue({ id: 'tmdb-extra' });
  setPluginCatalogs.mockResolvedValue({ sources: [] });
});

describe('PluginCatalog', () => {
  it('lists each catalog plugin with what is installed, and reports a catalog it cannot read', async () => {
    renderConsole(<PluginCatalog enabled />);
    expect(await screen.findByText('Extra metadata')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Install: Extra metadata' })).toBeEnabled();
    expect(screen.getByRole('button', { name: 'Update to 2.0.0: Single sign-on' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Current one/ })).not.toBeInTheDocument();
    expect(screen.getByText(/Couldn't read https:\/\/down.example/)).toBeInTheDocument();
  });

  it('installs only after confirming', async () => {
    const onInstalled = vi.fn();
    renderConsole(<PluginCatalog enabled onInstalled={onInstalled} />);
    await userEvent.click(await screen.findByRole('button', { name: 'Install: Extra metadata' }));
    const dialog = await screen.findByRole('alertdialog').catch(() => screen.findByRole('dialog'));
    expect(installFromPluginCatalog).not.toHaveBeenCalled();
    await userEvent.click(within(dialog).getByRole('button', { name: 'Install' }));
    await waitFor(() => expect(installFromPluginCatalog).toHaveBeenCalledWith(SOURCE, 'tmdb-extra'));
    await waitFor(() => expect(onInstalled).toHaveBeenCalled());
  });

  it('offers to add a catalog when there is none', async () => {
    getPluginCatalog.mockResolvedValue({ enabled: true, sources: [] });
    renderConsole(<PluginCatalog enabled />);
    expect(await screen.findByText('No catalogs yet')).toBeInTheDocument();
  });
});
