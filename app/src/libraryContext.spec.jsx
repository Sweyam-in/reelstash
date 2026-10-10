import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('./api', () => ({ getMyLibraries: vi.fn(), setCurrentLibrary: vi.fn(), setDefaultLibrary: vi.fn() }));
import { getMyLibraries, setCurrentLibrary, setDefaultLibrary } from './api';
import { LibraryProvider, useLibrary } from './libraryContext';
import { LEAVE_LOCKED_LIBRARY_EVENT } from './components/AppLock';

const films = { id: 1, name: 'Films', mode: 'entertainment' };
const vault = { id: 2, name: 'Vault', mode: 'private' };

function Probe() {
  const { library, mode, terms, isAdmin, defaultLibraryId, select, setDefault, libraries } = useLibrary();
  return <div>
    <span data-testid="library">{library?.name ?? 'none'}</span>
    <span data-testid="mode">{mode}</span>
    <span data-testid="person">{terms.person}</span>
    <span data-testid="admin">{String(isAdmin)}</span>
    <span data-testid="default">{String(defaultLibraryId)}</span>
    <span data-testid="count">{libraries.length}</span>
    <button onClick={() => select(2)}>pick vault</button>
    <button onClick={() => setDefault(2)}>default vault</button>
  </div>;
}
const mount = (props = {}) => render(<LibraryProvider isAdmin={false} {...props}><Probe /></LibraryProvider>);

beforeEach(() => {
  vi.clearAllMocks();
  sessionStorage.clear();
  getMyLibraries.mockResolvedValue({ items: [films, vault], defaultLibraryId: null });
});

describe('LibraryProvider', () => {
  it('shows a loading screen until the libraries arrive', async () => {
    mount();
    expect(screen.getByText('Loading libraries…')).toBeInTheDocument();
    expect(await screen.findByTestId('library')).toBeInTheDocument();
  });

  it('opens on the first library and takes its words from that library mode', async () => {
    mount();
    expect(await screen.findByTestId('library')).toHaveTextContent('Films');
    expect(screen.getByTestId('mode')).toHaveTextContent('entertainment');
    expect(screen.getByTestId('person')).toHaveTextContent('Actor');
  });

  it('tells the API layer which library is active', async () => {
    mount();
    await screen.findByTestId('library');
    expect(setCurrentLibrary).toHaveBeenLastCalledWith(1);
  });

  it('prefers the account default library', async () => {
    getMyLibraries.mockResolvedValue({ items: [films, vault], defaultLibraryId: 2 });
    mount();
    expect(await screen.findByTestId('library')).toHaveTextContent('Vault');
    expect(screen.getByTestId('person')).toHaveTextContent('Performer');
  });

  it('remembers a switch for the rest of the tab session', async () => {
    mount();
    await userEvent.click(await screen.findByRole('button', { name: 'pick vault' }));
    expect(screen.getByTestId('library')).toHaveTextContent('Vault');
    expect(sessionStorage.getItem('reelstash-library')).toBe('2');
    expect(setCurrentLibrary).toHaveBeenLastCalledWith(2);
  });

  it('restores the tab-session choice on the next mount, over the default', async () => {
    sessionStorage.setItem('reelstash-library', '2');
    mount();
    expect(await screen.findByTestId('library')).toHaveTextContent('Vault');
  });

  it('ignores a remembered library that no longer exists', async () => {
    sessionStorage.setItem('reelstash-library', '99');
    mount();
    expect(await screen.findByTestId('library')).toHaveTextContent('Films');
  });

  it('saves a new default through the API and reflects it', async () => {
    setDefaultLibrary.mockResolvedValue({});
    mount();
    await userEvent.click(await screen.findByRole('button', { name: 'default vault' }));
    await waitFor(() => expect(screen.getByTestId('default')).toHaveTextContent('2'));
    expect(setDefaultLibrary).toHaveBeenCalledWith(2);
  });

  it('carries the admin flag and defaults to entertainment when there are no libraries', async () => {
    getMyLibraries.mockResolvedValue({ items: [] });
    mount({ isAdmin: true });
    expect(await screen.findByTestId('library')).toHaveTextContent('none');
    expect(screen.getByTestId('admin')).toHaveTextContent('true');
    expect(screen.getByTestId('mode')).toHaveTextContent('entertainment');
    expect(setCurrentLibrary).toHaveBeenLastCalledWith(undefined);
  });

  it('treats a failed load as no libraries', async () => {
    getMyLibraries.mockRejectedValue(new Error('offline'));
    mount();
    expect(await screen.findByTestId('count')).toHaveTextContent('0');
  });

  it('moves to an open library when the lock screen asks to leave a locked one', async () => {
    getMyLibraries.mockResolvedValue({ items: [{ ...films, locked: true }, { ...vault, locked: false }], defaultLibraryId: 1 });
    mount();
    expect(await screen.findByTestId('library')).toHaveTextContent('Films');
    act(() => { window.dispatchEvent(new Event(LEAVE_LOCKED_LIBRARY_EVENT)); });
    expect(screen.getByTestId('library')).toHaveTextContent('Vault');
  });
});
