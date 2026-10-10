import React, { useMemo, useState } from 'react';
import { Check, ChevronDown, Search, X } from 'lucide-react';
import { Popover, PopoverContent, PopoverTrigger } from '@spatika/react';

/**
 * Compact filter controls for the media app: a tiny caption sits above a pill trigger that opens a
 * popover list, so a row of filters stays short. `searchable` adds a type-to-filter box for long lists
 * (studios). Options are `{ value, label }`; `value` is compared as a string.
 */
export function FilterSelect({ label, value, options, onChange, searchable = false, searchPlaceholder = 'Search…', emptyText = 'No matches', width }) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState('');
  const current = options.find(option => String(option.value) === String(value));
  const first = options[0];
  const active = !!first && String(value) !== String(first.value);
  const shown = useMemo(() => {
    const needle = text.trim().toLowerCase();
    return needle ? options.filter(option => option.label.toLowerCase().includes(needle)) : options;
  }, [options, text]);

  function pick(option) { onChange(String(option.value)); setOpen(false); }

  return <div className="rs-filter-field">
    <span className="rs-filter-caption">{label}</span>
    <Popover open={open} onOpenChange={next => { setOpen(next); if (!next) setText(''); }}>
      <PopoverTrigger className={`rs-filter-trigger${active ? ' is-active' : ''}`} style={width ? { width } : undefined} aria-label={`${label}: ${current?.label ?? ''}`}>
        <span>{current?.label ?? first?.label}</span>
        <ChevronDown size={14} aria-hidden="true" />
      </PopoverTrigger>
      <PopoverContent align="start" sideOffset={6} className="rs-filter-menu">
        {searchable && <label className="rs-filter-search"><Search size={14} aria-hidden="true" />
          <input autoFocus aria-label={`Search ${label.toLowerCase()}`} value={text} placeholder={searchPlaceholder} onChange={e => setText(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter' && shown[0]) pick(shown[0]); }} />
          {text && <button type="button" aria-label="Clear search" onClick={() => setText('')}><X size={13} /></button>}
        </label>}
        <div className="rs-filter-options" role="listbox" aria-label={label}>
          {shown.map(option => {
            const selected = String(option.value) === String(value);
            return <button type="button" role="option" aria-selected={selected} key={option.value} className={`rs-filter-option${selected ? ' is-selected' : ''}`} onClick={() => pick(option)}>
              <span>{option.label}</span>{selected && <Check size={14} aria-hidden="true" />}
            </button>;
          })}
          {!shown.length && <p className="rs-filter-empty">{emptyText}</p>}
        </div>
      </PopoverContent>
    </Popover>
  </div>;
}
