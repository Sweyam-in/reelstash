import React, { useState } from 'react';
import { describe, it, expect, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { Input } from '@spatika/react';
import { renderConsole } from '../testing';
import { Field, FormDialog } from './Form';

function Harness({ onSubmit = () => {}, error }) {
  const [open, setOpen] = useState(true);
  const [name, setName] = useState('');
  return <FormDialog open={open} onOpenChange={setOpen} title="Add thing" submitLabel="Add" dirty={name !== ''} canSubmit={name.trim().length > 0} error={error} onSubmit={onSubmit}>
    <Field label="Name" error={name.trim() ? null : 'Enter a name.'}>{props => <Input {...props} value={name} onChange={e => setName(e.target.value)} />}</Field>
  </FormDialog>;
}

describe('FormDialog', () => {
  it('keeps Save disabled until something changes, and shows no errors on a blank form', async () => {
    renderConsole(<Harness />);
    expect(await screen.findByRole('button', { name: 'Add' })).toBeDisabled();
    expect(screen.queryByText('Enter a name.')).not.toBeInTheDocument();
  });

  it('shows the error after the first submit attempt and puts focus on the invalid field', async () => {
    const onSubmit = vi.fn();
    renderConsole(<Harness onSubmit={onSubmit} />);
    const field = await screen.findByLabelText('Name');
    await userEvent.type(field, ' ');
    await userEvent.click(screen.getByRole('button', { name: 'Add' }));
    expect(await screen.findByText('Enter a name.')).toBeInTheDocument();
    await waitFor(() => expect(field).toHaveFocus());
    expect(onSubmit).not.toHaveBeenCalled();
    await userEvent.type(field, 'x');
    await userEvent.click(screen.getByRole('button', { name: 'Add' }));
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });

  it('asks before throwing away what was typed', async () => {
    renderConsole(<Harness />);
    await userEvent.type(await screen.findByLabelText('Name'), 'abc');
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(await screen.findByText('Discard unsaved changes?')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Discard changes' }));
    await waitFor(() => expect(screen.queryByLabelText('Name')).not.toBeInTheDocument());
  });

  it('shows a specific failure and says the input is kept', async () => {
    renderConsole(<Harness error="Couldn't add the thing. Name already exists." />);
    expect(await screen.findByRole('alert')).toHaveTextContent('Name already exists');
    expect(screen.getByText(/still here/)).toBeInTheDocument();
  });
});
