import React, { useState } from 'react';
import { describe, it, expect } from 'vitest';
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { FilterChip, FilterMulti, FilterPanel, FilterSelect } from './FilterControls';
import { renderApp } from '../testing/render';

function Harness() {
  const [studios, setStudios] = useState([]);
  const [rating, setRating] = useState('0');
  return <>
    <FilterMulti label="Studio" anyLabel="All" values={studios} onChange={setStudios} options={[{ value: 'Halcyon', label: 'Halcyon', count: 12 }, { value: 'Lumen', label: 'Lumen', count: 4 }]} />
    <FilterSelect label="Rating" value={rating} onChange={setRating} options={[{ value: '0', label: 'Any' }, { value: '4', label: '4+ ★' }]} />
    {studios.map(name => <FilterChip key={name} onRemove={() => setStudios(studios.filter(s => s !== name))} label={`Remove ${name}`}>{name}</FilterChip>)}
  </>;
}

describe('filter pills', () => {
  it('picks several studios at once and shows them as removable chips', async () => {
    renderApp(<Harness />);
    await userEvent.click(screen.getByRole('button', { name: 'Studio: All' }));
    const menu = await screen.findByRole('dialog');
    await userEvent.click(within(menu).getByRole('checkbox', { name: /Halcyon/ }));
    await userEvent.click(within(menu).getByRole('checkbox', { name: /Lumen/ }));
    await userEvent.click(within(menu).getByRole('button', { name: 'Apply (2)' }));
    expect(screen.getByRole('button', { name: 'Studio: 2 selected' })).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Remove Lumen' }));
    expect(screen.getByRole('button', { name: 'Studio: Halcyon' })).toBeInTheDocument();
  });

  it('a single-choice pill reads its label and marks itself active', async () => {
    renderApp(<Harness />);
    await userEvent.click(screen.getByRole('button', { name: 'Rating: Any' }));
    await userEvent.click(await screen.findByRole('option', { name: '4+ ★' }));
    const pill = screen.getByRole('button', { name: 'Rating: 4+ ★' });
    expect(pill).toHaveClass('is-active');
  });
});

describe('FilterPanel', () => {
  function Panel() {
    const [studios, setStudios] = useState([]);
    const [rating, setRating] = useState('0');
    const active = (studios.length ? 1 : 0) + (rating !== '0' ? 1 : 0);
    return <FilterPanel activeCount={active} onClear={() => { setStudios([]); setRating('0'); }} sections={[
      { id: 'studio', label: 'Studio', kind: 'multi', values: studios, onChange: setStudios, options: [{ value: 'Halcyon', label: 'Halcyon' }, { value: 'Lumen', label: 'Lumen' }] },
      { id: 'rating', label: 'Rating', kind: 'chips', value: rating, onChange: setRating, options: [{ value: '0', label: 'Any' }, { value: '4', label: '4+ ★' }] },
      { id: 'hidden', label: 'Hidden', kind: 'chips', value: 'a', onChange: () => {}, hidden: true, options: [{ value: 'a', label: 'A' }] },
    ]} />;
  }

  it('holds every filter in one popover, applies at once and counts what is active', async () => {
    renderApp(<Panel />);
    await userEvent.click(screen.getByRole('button', { name: 'Filters' }));
    expect(screen.queryByText('Hidden')).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('radio', { name: '4+ ★' }));
    await userEvent.click(screen.getByRole('button', { name: /Studio/ }));
    await userEvent.click(screen.getByRole('checkbox', { name: 'Lumen' }));
    expect(screen.getByRole('button', { name: 'Filters, 2 active' })).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Clear all' }));
    expect(screen.getByRole('button', { name: 'Filters' })).toBeInTheDocument();
  });
});
