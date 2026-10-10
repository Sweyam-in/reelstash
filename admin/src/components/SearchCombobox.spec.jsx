import React from 'react';
import { describe, it, expect, vi, afterEach } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { SearchCombobox, Thumb } from './SearchCombobox';

const people = [{ id: 1, name: 'Ann Lee' }, { id: 2, name: 'Bob Ray' }];

function setup(props = {}) {
  const searchFn = props.searchFn || vi.fn().mockResolvedValue(people);
  const onPick = vi.fn();
  render(<SearchCombobox placeholder="Find a performer" searchFn={searchFn} onPick={onPick} renderOption={o => <span>{o.name}</span>} {...props} />);
  return { searchFn, onPick, input: screen.getByRole('textbox', { name: 'Find a performer' }) };
}

afterEach(() => vi.useRealTimers());

describe('SearchCombobox', () => {
  it('searches after the debounce and lists the results', async () => {
    const { searchFn, input } = setup();
    await userEvent.type(input, 'an');
    await waitFor(() => expect(searchFn).toHaveBeenCalledWith('an'));
    expect(await screen.findByRole('button', { name: 'Ann Lee' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Bob Ray' })).toBeInTheDocument();
  });

  it('debounces: one search for a burst of typing', async () => {
    const { searchFn, input } = setup();
    await userEvent.type(input, 'annie');
    await screen.findByRole('button', { name: 'Ann Lee' });
    expect(searchFn).toHaveBeenCalledTimes(1);
    expect(searchFn).toHaveBeenCalledWith('annie');
  });

  it('does not search below the minimum length', async () => {
    const { searchFn, input } = setup({ minLength: 3 });
    await userEvent.type(input, 'ab');
    await new Promise(r => setTimeout(r, 400));
    expect(searchFn).not.toHaveBeenCalled();
  });

  it('picks a result, reports it and clears the field', async () => {
    const { onPick, input } = setup();
    await userEvent.type(input, 'an');
    await userEvent.click(await screen.findByRole('button', { name: 'Bob Ray' }));
    expect(onPick).toHaveBeenCalledWith(people[1]);
    expect(input).toHaveValue('');
    expect(screen.queryByRole('button', { name: 'Bob Ray' })).not.toBeInTheDocument();
  });

  it('says so when nothing matches', async () => {
    const { input } = setup({ searchFn: vi.fn().mockResolvedValue([]) });
    await userEvent.type(input, 'zzz');
    expect(await screen.findByText('No matches.')).toBeInTheDocument();
  });

  it('treats a failed search as no results', async () => {
    const { input } = setup({ searchFn: vi.fn().mockRejectedValue(new Error('down')) });
    await userEvent.type(input, 'zzz');
    expect(await screen.findByText('No matches.')).toBeInTheDocument();
  });

  it('offers to create a new entry that is not an exact match', async () => {
    const { onPick, input } = setup({ createLabel: name => `Create "${name}"` });
    await userEvent.type(input, 'Carla');
    const create = await screen.findByRole('button', { name: /Create "Carla"/ });
    await userEvent.click(create);
    expect(onPick).toHaveBeenCalledWith({ name: 'Carla' });
  });

  it('does not offer to create something that already exists (case-insensitive)', async () => {
    const { input } = setup({ createLabel: name => `Create "${name}"` });
    await userEvent.type(input, 'ann lee');
    await screen.findByRole('button', { name: 'Ann Lee' });
    expect(screen.queryByRole('button', { name: /Create/ })).not.toBeInTheDocument();
  });

  it('closes on Escape and on an outside click', async () => {
    const { input } = setup();
    await userEvent.type(input, 'an');
    await screen.findByRole('button', { name: 'Ann Lee' });
    await userEvent.keyboard('{Escape}');
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
    await userEvent.type(input, 'n'); // typing reopens it
    expect(await screen.findByRole('listbox')).toBeInTheDocument();
    act(() => { document.body.dispatchEvent(new Event('pointerdown', { bubbles: true })); });
    await waitFor(() => expect(screen.queryByRole('listbox')).not.toBeInTheDocument());
  });
});

describe('Thumb', () => {
  it('shows the image when there is one', () => {
    const { container } = render(<Thumb src="/a.jpg" name="Ann" />);
    expect(container.querySelector('img')).toHaveAttribute('src', '/a.jpg');
  });

  it('falls back to initials without an image', () => {
    render(<Thumb name="ann lee" />);
    expect(screen.getByText('AN')).toBeInTheDocument();
    render(<Thumb />);
    expect(screen.getByText('?')).toBeInTheDocument();
  });

  it('falls back to initials when the image fails to load', () => {
    const { container } = render(<Thumb src="/broken.jpg" name="Bob" round />);
    act(() => { container.querySelector('img').dispatchEvent(new Event('error')); });
    expect(screen.getByText('BO')).toHaveClass('is-round');
  });
});
