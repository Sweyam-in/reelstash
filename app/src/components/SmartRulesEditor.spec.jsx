import React, { useState } from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

vi.mock('../api', () => ({ getTags: vi.fn(), getCategories: vi.fn() }));
import { getCategories, getTags } from '../api';
import SmartRulesEditor from './SmartRulesEditor';
import { emptyRules, toServerRules } from '../smartRules';

let latest;
function Harness({ initial = emptyRules() }) {
  const [rules, setRules] = useState(initial);
  latest = rules;
  return <SmartRulesEditor rules={rules} onChange={setRules} />;
}

beforeEach(() => {
  getTags.mockResolvedValue({ items: [{ id: 1, name: 'Marvel' }, { id: 2, name: 'Noir' }] });
  getCategories.mockResolvedValue({ items: [{ id: 1, name: 'Action' }] });
});

describe('SmartRulesEditor', () => {
  it('starts with one condition and builds rules from the choices', async () => {
    render(<Harness />);
    expect(screen.getAllByRole('listitem')).toHaveLength(1);
    await userEvent.selectOptions(screen.getByLabelText('Condition 1 field'), 'tag');
    // A tag has an operator and a text value (with the existing tags offered).
    expect(screen.getByLabelText('Condition 1 operator')).toHaveValue('is');
    await userEvent.type(screen.getByLabelText('Condition 1 value'), 'Marvel');
    await waitFor(() => expect(document.querySelectorAll('#rs-smart-tags option')).toHaveLength(2));

    await userEvent.click(screen.getByRole('button', { name: /add condition/i }));
    await userEvent.selectOptions(screen.getByLabelText('Condition 2 field'), 'rating');
    expect(screen.getByLabelText('Condition 2 value')).toHaveValue(4);
    await userEvent.selectOptions(screen.getByLabelText('Condition 2 operator'), 'lte');

    await userEvent.selectOptions(screen.getByLabelText('Match all or any'), 'any');
    await userEvent.selectOptions(screen.getByLabelText(/sort by/i), 'rated');
    await userEvent.type(screen.getByLabelText(/limit to/i), '25');

    expect(toServerRules(latest)).toEqual({
      match: 'any', sort: 'rated', limit: 25,
      conditions: [{ field: 'tag', op: 'is', value: 'Marvel' }, { field: 'rating', op: 'lte', value: 4 }],
    });
  });

  it('shows the right input for each kind of field', async () => {
    render(<Harness />);
    const field = screen.getByLabelText('Condition 1 field');
    await userEvent.selectOptions(field, 'watched');
    expect(screen.getByLabelText('Condition 1 value')).toHaveDisplayValue('Not watched yet');
    await userEvent.selectOptions(field, 'addedAfter');
    expect(screen.getByLabelText('Condition 1 value')).toHaveAttribute('type', 'date');
    await userEvent.selectOptions(field, 'kind');
    expect(screen.getByLabelText('Condition 1 value')).toHaveDisplayValue('Movie');
    await userEvent.selectOptions(field, 'addedWithin');
    expect(screen.getByLabelText('Condition 1 value')).toHaveValue(30);
    expect(screen.queryByLabelText('Condition 1 operator')).not.toBeInTheDocument();
  });

  it('removes conditions, and stops adding at the limit', async () => {
    const many = { ...emptyRules(), conditions: Array.from({ length: 20 }, () => ({ field: 'favorite', value: true })) };
    render(<Harness initial={many} />);
    expect(screen.getByRole('button', { name: /add condition/i })).toBeDisabled();
    await userEvent.click(screen.getByRole('button', { name: 'Remove condition 20' }));
    expect(screen.getAllByRole('listitem')).toHaveLength(19);
    expect(screen.getByRole('button', { name: /add condition/i })).toBeEnabled();
  });
});
