import React, { useMemo, useState } from 'react';
import { Check, ChevronDown, ChevronRight, Search, SlidersHorizontal, X } from 'lucide-react';
import { Popover, PopoverContent, PopoverTrigger } from '../ui';

/**
 * Filter pills for the media app: "Label  Value ▾" opens a popover list. `searchable` adds a
 * type-to-filter box for long lists (studios, people). Options are `{ value, label, count? }`;
 * `value` is compared as a string. The first option is the "any" choice: anything else marks the
 * pill active.
 */
export function FilterSelect({ label, value, options, onChange, searchable = false, searchPlaceholder = 'Search…', emptyText = 'No matches', width, onSearch }) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState('');
  const current = options.find(option => String(option.value) === String(value));
  const first = options[0];
  const active = !!first && String(value) !== String(first.value);
  const shown = useMemo(() => {
    const needle = text.trim().toLowerCase();
    return needle && !onSearch ? options.filter(option => option.label.toLowerCase().includes(needle)) : options;
  }, [options, text, onSearch]);

  function pick(option) { onChange(String(option.value)); setOpen(false); }

  return <div className="rs-filter-field">
    <Popover open={open} onOpenChange={next => { setOpen(next); if (!next) { setText(''); onSearch?.(''); } }}>
      <PopoverTrigger className={`rs-filter-trigger${active ? ' is-active' : ''}`} style={width ? { width } : undefined} aria-label={`${label}: ${current?.label ?? ''}`}>
        <span className="rs-filter-caption">{label}</span>
        <span className="rs-filter-value">{current?.label ?? first?.label}</span>
        <ChevronDown size={14} aria-hidden="true" />
      </PopoverTrigger>
      <PopoverContent align="start" sideOffset={6} className="rs-filter-menu">
        {searchable && <label className="rs-filter-search"><Search size={14} aria-hidden="true" />
          <input autoFocus aria-label={`Search ${label.toLowerCase()}`} value={text} placeholder={searchPlaceholder} onChange={e => { setText(e.target.value); onSearch?.(e.target.value); }}
            onKeyDown={e => { if (e.key === 'Enter' && shown[0]) pick(shown[0]); }} />
          {text && <button type="button" aria-label="Clear search" onClick={() => { setText(''); onSearch?.(''); }}><X size={13} /></button>}
        </label>}
        <div className="rs-filter-options" role="listbox" aria-label={label}>
          {shown.map(option => {
            const selected = String(option.value) === String(value);
            return <button type="button" role="option" aria-selected={selected} key={option.value} className={`rs-filter-option${selected ? ' is-selected' : ''}`} onClick={() => pick(option)}>
              <span className="rs-filter-option-label">{option.label}</span>
              {option.count != null && <span className="rs-filter-count">{option.count.toLocaleString()}</span>}
              {selected && <Check size={14} aria-hidden="true" />}
            </button>;
          })}
          {!shown.length && <p className="rs-filter-empty">{emptyText}</p>}
        </div>
      </PopoverContent>
    </Popover>
  </div>;
}

/**
 * Several values at once (studios): checkboxes, a search box and Clear / Show. `values` is an array of
 * strings; the trigger reads "Any", one name, or "N selected".
 */
export function FilterMulti({ label, anyLabel = 'Any', values, options, onChange, searchPlaceholder = 'Search…', emptyText = 'No matches' }) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState('');
  const [draft, setDraft] = useState(values);
  const shown = useMemo(() => {
    const needle = text.trim().toLowerCase();
    return needle ? options.filter(option => option.label.toLowerCase().includes(needle)) : options;
  }, [options, text]);
  const summary = !values.length ? anyLabel : values.length === 1 ? (options.find(o => o.value === values[0])?.label || values[0]) : `${values.length} selected`;
  const toggle = value => setDraft(current => current.includes(value) ? current.filter(v => v !== value) : [...current, value]);
  return <div className="rs-filter-field">
    <Popover open={open} onOpenChange={next => { setOpen(next); if (next) setDraft(values); else setText(''); }}>
      <PopoverTrigger className={`rs-filter-trigger${values.length ? ' is-active' : ''}`} aria-label={`${label}: ${summary}`}>
        <span className="rs-filter-caption">{label}</span>
        <span className="rs-filter-value">{summary}</span>
        <ChevronDown size={14} aria-hidden="true" />
      </PopoverTrigger>
      <PopoverContent align="start" sideOffset={6} className="rs-filter-menu is-multi">
        <label className="rs-filter-search"><Search size={14} aria-hidden="true" />
          <input autoFocus aria-label={`Search ${label.toLowerCase()}`} value={text} placeholder={searchPlaceholder} onChange={e => setText(e.target.value)} />
          {text && <button type="button" aria-label="Clear search" onClick={() => setText('')}><X size={13} /></button>}
        </label>
        <div className="rs-filter-options" role="group" aria-label={label}>
          {shown.map(option => {
            const checked = draft.includes(option.value);
            return <label key={option.value} className={`rs-filter-option${checked ? ' is-selected' : ''}`}>
              <input type="checkbox" checked={checked} onChange={() => toggle(option.value)} />
              <span className="rs-filter-option-label">{option.label}</span>
              {option.count != null && <span className="rs-filter-count">{option.count.toLocaleString()}</span>}
            </label>;
          })}
          {!shown.length && <p className="rs-filter-empty">{emptyText}</p>}
        </div>
        <div className="rs-filter-foot">
          <button type="button" className="rs-btn rs-btn--ghost rs-btn--sm" onClick={() => setDraft([])}>Clear</button>
          <button type="button" className="rs-btn rs-btn--primary rs-btn--sm" onClick={() => { onChange(draft); setOpen(false); }}>{draft.length ? `Apply (${draft.length})` : 'Show all'}</button>
        </div>
      </PopoverContent>
    </Popover>
  </div>;
}

/** A removable "Label: value" chip for an active filter. */
export function FilterChip({ children, onRemove, label }) {
  return <span className="rs-filter-chip">{children}<button type="button" aria-label={label || `Remove filter ${typeof children === 'string' ? children : ''}`.trim()} onClick={onRemove}><X size={12} strokeWidth={2.4} /></button></span>;
}

/**
 * One "Filters" button that opens a single popover holding every filter, so the bar stays calm.
 * Changes apply at once (the callers keep them in the browser, so they survive a reload).
 * `sections` is a list of:
 *  - { id, label, kind: 'chips', value, options, onChange }      a few choices shown as pills
 *  - { id, label, kind: 'single', value, options, onChange, onSearch? }   a searchable pick-one list
 *  - { id, label, kind: 'multi', values, options, onChange }      a searchable pick-several list
 * Options are `{ value, label, count? }`; the first option of chips/single is the "any" choice.
 * `activeCount` badges the button and `onClear` resets everything.
 */
export function FilterPanel({ sections, activeCount = 0, onClear }) {
  const [open, setOpen] = useState(false);
  const [expanded, setExpanded] = useState(null);
  const [text, setText] = useState('');
  const toggle = id => { setExpanded(current => (current === id ? null : id)); setText(''); };
  const visible = sections.filter(section => !section.hidden);
  const summary = section => {
    if (section.kind === 'multi') return !section.values.length ? 'Any' : section.values.length === 1 ? (section.options.find(o => o.value === section.values[0])?.label || section.values[0]) : `${section.values.length} selected`;
    return section.options.find(o => String(o.value) === String(section.value))?.label ?? section.options[0]?.label;
  };
  const isActive = section => (section.kind === 'multi' ? section.values.length > 0 : String(section.value) !== String(section.options[0]?.value));
  return <div className="rs-filter-field">
    <Popover open={open} onOpenChange={next => { setOpen(next); if (!next) { setExpanded(null); setText(''); } }}>
      <PopoverTrigger className={`rs-filter-trigger rs-filters-trigger${activeCount ? ' is-active' : ''}`} aria-label={activeCount ? `Filters, ${activeCount} active` : 'Filters'}>
        <SlidersHorizontal size={15} aria-hidden="true" />
        <span className="rs-filter-value">Filters</span>
        {activeCount > 0 && <span className="rs-filters-count">{activeCount}</span>}
      </PopoverTrigger>
      <PopoverContent align="start" sideOffset={6} className="rs-filter-menu rs-filters-panel">
        <div className="rs-filters-head"><strong>Filters</strong>{activeCount > 0 && <button type="button" className="rs-clear-all" onClick={onClear}>Clear all</button>}</div>
        <div className="rs-filters-body">
          {visible.map(section => <section key={section.id} className="rs-filters-section" aria-label={section.label}>
            {section.kind === 'chips'
              ? <>
                <h3>{section.label}</h3>
                <div className="rs-filters-chips" role="radiogroup" aria-label={section.label}>
                  {section.options.map(option => <button key={option.value} type="button" role="radio" aria-checked={String(option.value) === String(section.value)} className="rs-filters-chip" onClick={() => section.onChange(String(option.value))}>{option.label}</button>)}
                </div>
              </>
              : <>
                <button type="button" className={`rs-filters-row${isActive(section) ? ' is-active' : ''}`} aria-expanded={expanded === section.id} onClick={() => toggle(section.id)}>
                  <span>{section.label}</span><span className="rs-filters-row-value">{summary(section)}</span><ChevronRight size={14} aria-hidden="true" />
                </button>
                {expanded === section.id && <div className="rs-filters-list">
                  <label className="rs-filter-search"><Search size={14} aria-hidden="true" />
                    <input autoFocus aria-label={`Search ${section.label.toLowerCase()}`} value={text} placeholder={`Find a ${section.label.toLowerCase()}`} onChange={event => { setText(event.target.value); section.onSearch?.(event.target.value); }} />
                  </label>
                  <div className="rs-filter-options" role={section.kind === 'multi' ? 'group' : 'listbox'} aria-label={section.label}>
                    {(text.trim() && !section.onSearch ? section.options.filter(o => o.label.toLowerCase().includes(text.trim().toLowerCase())) : section.options).map(option => {
                      const selected = section.kind === 'multi' ? section.values.includes(option.value) : String(option.value) === String(section.value);
                      const pick = () => (section.kind === 'multi'
                        ? section.onChange(selected ? section.values.filter(v => v !== option.value) : [...section.values, option.value])
                        : section.onChange(String(option.value)));
                      return <button key={option.value} type="button" role={section.kind === 'multi' ? 'checkbox' : 'option'} aria-checked={section.kind === 'multi' ? selected : undefined} aria-selected={section.kind === 'multi' ? undefined : selected} className={`rs-filter-option${selected ? ' is-selected' : ''}`} onClick={pick}>
                        <span className="rs-filter-option-label">{option.label}</span>
                        {option.count != null && <span className="rs-filter-count">{option.count.toLocaleString()}</span>}
                        {selected && <Check size={14} aria-hidden="true" />}
                      </button>;
                    })}
                  </div>
                </div>}
              </>}
          </section>)}
        </div>
      </PopoverContent>
    </Popover>
  </div>;
}
