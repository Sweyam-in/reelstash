import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('./api', () => ({ getLibraries: vi.fn() }));
import { getLibraries } from './api';
import { AdminHeaderProvider, AdminHeaderTitle } from './adminHeader';
import { LibraryScope, useAdminLibrary } from './adminLibraryFilter';

const films = { id: 1, name: 'Films', mode: 'entertainment' };
const vault = { id: 2, name: 'Vault', mode: 'private' };

function Probe() {
  const { libraryId } = useAdminLibrary();
  return <span data-testid="scope">{String(libraryId)}</span>;
}

// The scope renders into the top bar next to the title, so mount the title slot too.
const mount = props => render(<AdminHeaderProvider><AdminHeaderTitle fallback="People" /><LibraryScope {...props} /><Probe /></AdminHeaderProvider>);

beforeEach(() => {
  localStorage.clear();
  getLibraries.mockResolvedValue({ items: [films, vault] });
});

describe('LibraryScope', () => {
  it('sits beside the page title and switches the admin-wide library', async () => {
    const user = userEvent.setup();
    mount();
    const trigger = await screen.findByRole('button', { name: 'Library scope' });
    expect(trigger.closest('.rs-header-heading')).not.toBeNull();
    expect(trigger).toHaveTextContent('All libraries');
    await user.click(trigger);
    await user.click(await screen.findByRole('menuitemradio', { name: 'Vault' }));
    await waitFor(() => expect(screen.getByTestId('scope')).toHaveTextContent('2'));
    expect(screen.getByRole('button', { name: 'Library scope' })).toHaveTextContent('Vault');
  });

  it('shows one library and offers no "All libraries" on per-library pages', async () => {
    const user = userEvent.setup();
    mount({ allowAll: false });
    const trigger = await screen.findByRole('button', { name: 'Library scope' });
    expect(trigger).toHaveTextContent('Films');
    await user.click(trigger);
    await screen.findByRole('menuitemradio', { name: 'Films' });
    expect(screen.queryByRole('menuitemradio', { name: 'All libraries' })).toBeNull();
  });
});
