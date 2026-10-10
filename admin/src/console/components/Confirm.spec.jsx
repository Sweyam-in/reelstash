import React from 'react';
import { describe, it, expect } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderConsole } from '../testing';
import { useConfirm } from './Confirm';

let outcome;
function Harness({ options }) {
  const confirm = useConfirm();
  return <button onClick={async () => { outcome = await confirm(options); }}>Go</button>;
}
const open = async options => { outcome = undefined; renderConsole(<Harness options={options} />); await userEvent.click(screen.getByRole('button', { name: 'Go' })); };

describe('useConfirm', () => {
  it('says what will happen, who is affected and whether it can be undone', async () => {
    await open({ title: 'Disable Asha?', consequence: 'Asha is signed out.', affects: 'Asha Nair', reversible: true, confirmLabel: 'Disable account' });
    expect(await screen.findByText('Disable Asha?')).toBeInTheDocument();
    expect(screen.getByText('Asha is signed out.')).toBeInTheDocument();
    expect(screen.getByText('Asha Nair')).toBeInTheDocument();
    expect(screen.getByText('Yes, this can be undone later.')).toBeInTheDocument();
  });

  it('states plainly when an action is permanent', async () => {
    await open({ title: 'Delete?', consequence: 'Gone.', reversible: false, confirmLabel: 'Delete' });
    expect(await screen.findByText('No. This is permanent and cannot be undone.')).toBeInTheDocument();
  });

  it('resolves with the reason once confirmed', async () => {
    await open({ title: 'Stop?', consequence: 'It stops.', confirmLabel: 'Stop', reason: { label: 'Why?' } });
    const confirmButton = await screen.findByRole('button', { name: 'Stop' });
    expect(confirmButton).toBeDisabled();
    await userEvent.type(screen.getByLabelText('Why?'), '  noisy ');
    await userEvent.click(confirmButton);
    await waitFor(() => expect(outcome).toEqual({ reason: 'noisy' }));
  });

  it('keeps an irreversible action locked until the exact text is typed', async () => {
    await open({ title: 'Delete the library?', consequence: 'No files are deleted.', confirmLabel: 'Delete library', tone: 'danger', typeToConfirm: 'Movies' });
    const confirmButton = await screen.findByRole('button', { name: 'Delete library' });
    expect(confirmButton).toBeDisabled();
    const field = screen.getByLabelText(/Type/);
    await userEvent.type(field, 'Movie');
    expect(confirmButton).toBeDisabled();
    await userEvent.type(field, 's');
    expect(confirmButton).toBeEnabled();
    await userEvent.click(confirmButton);
    await waitFor(() => expect(outcome).toEqual({ reason: '' }));
  });

  it('resolves with null when cancelled', async () => {
    await open({ title: 'Sure?', consequence: 'Maybe.', confirmLabel: 'Yes' });
    await userEvent.click(await screen.findByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(outcome).toBeNull());
  });
});
