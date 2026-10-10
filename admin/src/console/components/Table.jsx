import React from 'react';
import { NativeSelect, Pagination } from '@spatika/react';
import { ArrowDown, ArrowUp, ArrowUpDown, Search } from 'lucide-react';
import { formatNumber } from '../lib/format';

export const PAGE_SIZES = [25, 50, 100];

/** Server-side pagination footer driven by limit/offset/total. */
export function TablePagination({ limit, offset, total, noun, onChange, pageSizes = PAGE_SIZES }) {
  const page = Math.floor(offset / limit) + 1;
  const pageCount = Math.max(1, Math.ceil(total / limit));
  const from = total === 0 ? 0 : offset + 1;
  const to = Math.min(offset + limit, total);
  return <div className="console-table-footer">
    <p className="console-table-footer__summary">{total === 0 ? `No ${noun}` : `${formatNumber(from)}–${formatNumber(to)} of ${formatNumber(total)} ${noun}`}</p>
    <div className="console-table-footer__controls">
      {pageSizes && <label className="console-table-footer__size">Rows
        <NativeSelect size="sm" value={String(limit)} onChange={e => onChange({ limit: Number(e.target.value), offset: 0 })} aria-label="Rows per page">
          {pageSizes.map(n => <option key={n} value={n}>{n}</option>)}
        </NativeSelect>
      </label>}
      {pageCount > 1 && <Pagination page={page} pageCount={pageCount} onPageChange={p => onChange({ limit, offset: (p - 1) * limit })} variant="compact" />}
    </div>
  </div>;
}

/** Clickable column header for server-side sorting. sort: { by, dir } */
export function SortHeader({ label, column, sort, onSort }) {
  const active = sort.by === column;
  const Icon = !active ? ArrowUpDown : sort.dir === 'asc' ? ArrowUp : ArrowDown;
  return <button type="button" className="console-sort" data-active={active || undefined}
    aria-label={`Sort by ${label}${active ? `, currently ${sort.dir === 'asc' ? 'ascending' : 'descending'}` : ''}`}
    onClick={() => onSort({ by: column, dir: active && sort.dir === 'desc' ? 'asc' : 'desc' })}>
    {label}<Icon size={12} aria-hidden="true" />
  </button>;
}

/** Search + filters row on top of a table panel. */
export function TableToolbar({ search, onSearch, placeholder, label = 'Search', children, end }) {
  return <div className="console-table-toolbar" role="search">
    {onSearch && <label className="console-search console-table-toolbar__search">
      <Search size={14} aria-hidden="true" />
      <input type="search" aria-label={label} value={search} placeholder={placeholder} onChange={e => onSearch(e.target.value)} />
    </label>}
    {children}
    {end && <div className="console-table-toolbar__end">{end}</div>}
  </div>;
}

/** Selected-rows bar with the bulk actions the API allows. */
export function BulkBar({ count, noun, onClear, children }) {
  if (!count) return null;
  return <div className="console-bulkbar" role="region" aria-label="Bulk actions">
    <span><strong>{formatNumber(count)}</strong> {noun} selected</span>
    <button type="button" className="console-link" onClick={onClear}>Clear</button>
    <div className="console-bulkbar__actions">{children}</div>
  </div>;
}

/** Tabs (ARIA tablist) that sit on top of a panel, or on the page when `page`. */
export function SegmentedTabs({ value, onChange, options, label, page = false }) {
  const onKeyDown = event => {
    if (event.key !== 'ArrowRight' && event.key !== 'ArrowLeft') return;
    const index = options.findIndex(o => o.value === value);
    const next = options[(index + (event.key === 'ArrowRight' ? 1 : -1) + options.length) % options.length];
    onChange(next.value);
    event.currentTarget.querySelectorAll('[role="tab"]')[options.indexOf(next)]?.focus();
  };
  return <div className={page ? 'console-segmented-tabs console-segmented-tabs--page' : 'console-segmented-tabs'} role="tablist" aria-label={label} onKeyDown={onKeyDown}>
    {options.map(option => <button key={option.value} type="button" role="tab" aria-selected={option.value === value} tabIndex={option.value === value ? 0 : -1} onClick={() => onChange(option.value)}>
      {option.label}{option.count !== undefined && option.count !== null && <span className="console-segmented-tabs__count">{formatNumber(option.count)}</span>}
    </button>)}
  </div>;
}

/** Compact time-range switcher for charts. */
export function RangeTabs({ value, onChange, options, label }) {
  return <div className="console-range" role="group" aria-label={label}>
    {options.map(option => <button key={option.value} type="button" aria-pressed={option.value === value} onClick={() => onChange(option.value)}>{option.label}</button>)}
  </div>;
}

/** Keyboard and pointer activation for clickable table rows. Spread onto <TableRow>. */
export function rowActivation(onActivate, { selected = false } = {}) {
  return {
    tabIndex: 0,
    className: 'console-clickable-row',
    'data-selected': selected || undefined,
    onClick: event => {
      if (event.target.closest('button, a, input, label, select, [role="checkbox"], [role="menuitem"], [role="switch"]')) return;
      onActivate();
    },
    onKeyDown: event => {
      if (event.target !== event.currentTarget) return;
      if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); onActivate(); }
    },
  };
}
