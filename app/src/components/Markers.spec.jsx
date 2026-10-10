import React, { useState } from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../api', () => ({ createMarker: vi.fn(), updateMarker: vi.fn(), deleteMarker: vi.fn() }));
import { createMarker, deleteMarker, updateMarker } from '../api';
import Markers from './Markers';
import { renderApp } from '../testing/render';

const stored = [
  { id: 2, seconds: 125, title: 'Later scene' },
  { id: 1, seconds: 65, title: 'Opening' },
];

function Harness({ initial = stored, currentTime = 42.34, onJump = vi.fn() }) {
  const [markers, setMarkers] = useState(initial);
  const videoRef = { current: { currentTime } };
  return <Markers mediaId={9} videoRef={videoRef} markers={markers} onChange={setMarkers} onJump={onJump} />;
}

beforeEach(() => vi.clearAllMocks());

describe('Markers', () => {
  it('lists markers in time order with clock labels', () => {
    renderApp(<Harness />);
    const items = screen.getAllByRole('listitem');
    expect(items[0]).toHaveTextContent('Opening');
    expect(items[0]).toHaveTextContent('1:05');
    expect(items[1]).toHaveTextContent('Later scene');
    expect(items[1]).toHaveTextContent('2:05');
  });

  it('shows an empty state', () => {
    renderApp(<Harness initial={[]} />);
    expect(screen.getByText(/No markers yet/)).toBeInTheDocument();
  });

  it('jumps to the marker time', async () => {
    const onJump = vi.fn();
    renderApp(<Harness onJump={onJump} />);
    await userEvent.click(screen.getByRole('button', { name: 'Play from 2:05' }));
    expect(onJump).toHaveBeenCalledWith(125);
  });

  it('adds a marker at the current playback position, rounded to a tenth', async () => {
    createMarker.mockResolvedValue({ id: 3, seconds: 42.3, title: 'My moment' });
    renderApp(<Harness />);
    await userEvent.type(screen.getByRole('textbox', { name: 'Marker name' }), 'My moment');
    await userEvent.click(screen.getByRole('button', { name: /Add at current time/ }));
    await waitFor(() => expect(createMarker).toHaveBeenCalledWith(9, 42.3, 'My moment'));
    expect(await screen.findByText('My moment')).toBeInTheDocument();
    expect(screen.getByRole('textbox', { name: 'Marker name' })).toHaveValue('');
  });

  it('treats an unreadable playback position as the start', async () => {
    createMarker.mockResolvedValue({ id: 3, seconds: 0, title: '' });
    renderApp(<Harness currentTime={NaN} />);
    await userEvent.click(screen.getByRole('button', { name: /Add at current time/ }));
    await waitFor(() => expect(createMarker).toHaveBeenCalledWith(9, 0, ''));
  });

  it('reports a failed add and keeps the list unchanged', async () => {
    createMarker.mockRejectedValue(new Error('this title already has the maximum number of markers'));
    renderApp(<Harness />);
    await userEvent.click(screen.getByRole('button', { name: /Add at current time/ }));
    expect(await screen.findByText('Could not add marker')).toBeInTheDocument();
    expect(screen.getByText(/maximum number of markers/)).toBeInTheDocument();
    expect(screen.getAllByRole('listitem')).toHaveLength(2);
  });

  it('renames a marker', async () => {
    updateMarker.mockResolvedValue({ id: 1, seconds: 65, title: 'Intro' });
    renderApp(<Harness />);
    await userEvent.click(screen.getByRole('button', { name: 'Rename Opening' }));
    const field = screen.getAllByRole('textbox', { name: 'Marker name' }).find(el => el.value === 'Opening');
    await userEvent.clear(field);
    await userEvent.type(field, 'Intro{Enter}');
    await waitFor(() => expect(updateMarker).toHaveBeenCalledWith(9, 1, { title: 'Intro' }));
    expect(await screen.findByText('Intro')).toBeInTheDocument();
  });

  it('does not call the server when the name is unchanged, and Escape cancels', async () => {
    renderApp(<Harness />);
    await userEvent.click(screen.getByRole('button', { name: 'Rename Opening' }));
    await userEvent.click(screen.getByRole('button', { name: 'Save name' }));
    expect(updateMarker).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole('button', { name: 'Rename Opening' }));
    const field = screen.getAllByRole('textbox', { name: 'Marker name' }).find(el => el.value === 'Opening');
    await userEvent.type(field, 'x{Escape}');
    expect(updateMarker).not.toHaveBeenCalled();
    expect(screen.getByText('Opening')).toBeInTheDocument();
  });

  it('deletes a marker', async () => {
    deleteMarker.mockResolvedValue(null);
    renderApp(<Harness />);
    await userEvent.click(screen.getByRole('button', { name: 'Delete Opening' }));
    await waitFor(() => expect(deleteMarker).toHaveBeenCalledWith(9, 1));
    await waitFor(() => expect(screen.queryByText('Opening')).not.toBeInTheDocument());
  });

  it('keeps the marker and says why when deleting fails', async () => {
    deleteMarker.mockRejectedValue(new Error('marker not found'));
    renderApp(<Harness />);
    await userEvent.click(screen.getByRole('button', { name: 'Delete Opening' }));
    expect(await screen.findByText('Could not delete marker')).toBeInTheDocument();
    expect(screen.getByText('Opening')).toBeInTheDocument();
  });
});
