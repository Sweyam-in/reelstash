import React, { useEffect, useState } from 'react';
import { Plus, X } from 'lucide-react';
import { Button } from '@spatika/react';
import { getCategories, getTags } from '../api';
import { MAX_CONDITIONS, OP_LABELS, SMART_FIELDS, SMART_SORTS, newCondition } from '../smartRules';

// Builds the rules of a smart playlist: which conditions, all or any of them, how to sort, and an
// optional cap on the number of titles. `rules` is editor state (see smartRules.js).

function useSuggestions() {
  const [names, setNames] = useState({ tags: [], categories: [] });
  useEffect(() => {
    let live = true;
    Promise.allSettled([getTags(), getCategories()]).then(([tags, categories]) => {
      if (!live) return;
      setNames({
        tags: tags.status === 'fulfilled' ? (tags.value.items || []).map(t => t.name) : [],
        categories: categories.status === 'fulfilled' ? (categories.value.items || []).map(c => c.name) : [],
      });
    });
    return () => { live = false; };
  }, []);
  return names;
}

function ValueInput({ spec, condition, suggestions, onChange, label }) {
  if (spec.kind === 'bool') {
    return <select aria-label={`${label} value`} value={condition.value === false ? 'no' : 'yes'} onChange={e => onChange({ value: e.target.value === 'yes' })}>
      <option value="yes">{spec.yes}</option><option value="no">{spec.no}</option>
    </select>;
  }
  if (spec.kind === 'choice') {
    return <select aria-label={`${label} value`} value={condition.value} onChange={e => onChange({ value: e.target.value })}>
      {spec.choices.map(([value, name]) => <option key={value} value={value}>{name}</option>)}
    </select>;
  }
  if (spec.kind === 'date') return <input type="date" aria-label={`${label} value`} value={condition.value} onChange={e => onChange({ value: e.target.value })} />;
  if (spec.kind === 'int') {
    return <span className="rs-smart-number">
      <input type="number" inputMode="numeric" aria-label={`${label} value`} min={spec.min} max={spec.max} value={condition.value} onChange={e => onChange({ value: e.target.value })} />
      {spec.unit && <small>{spec.unit}</small>}
    </span>;
  }
  const listId = spec.suggest ? `rs-smart-${spec.suggest}` : undefined;
  return <>
    <input type="text" aria-label={`${label} value`} list={listId} maxLength={200} placeholder={spec.placeholder || 'Name'} value={condition.value} onChange={e => onChange({ value: e.target.value })} />
    {listId && <datalist id={listId}>{suggestions[spec.suggest].map(name => <option key={name} value={name} />)}</datalist>}
  </>;
}

export default function SmartRulesEditor({ rules, onChange }) {
  const suggestions = useSuggestions();
  const setCondition = (index, patch) => onChange({ ...rules, conditions: rules.conditions.map((c, i) => i === index ? { ...c, ...patch } : c) });
  const changeField = (index, field) => onChange({ ...rules, conditions: rules.conditions.map((c, i) => i === index ? newCondition(field) : c) });
  const remove = index => onChange({ ...rules, conditions: rules.conditions.filter((_, i) => i !== index) });

  return <div className="rs-smart-editor">
    <div className="rs-smart-match">
      <span>Include titles that match</span>
      <select aria-label="Match all or any" value={rules.match} onChange={e => onChange({ ...rules, match: e.target.value })}>
        <option value="all">all</option><option value="any">any</option>
      </select>
      <span>of these conditions</span>
    </div>
    <ul className="rs-smart-conditions">
      {rules.conditions.map((condition, index) => {
        const spec = SMART_FIELDS.find(f => f.id === condition.field) || SMART_FIELDS[0];
        const label = `Condition ${index + 1}`;
        return <li key={index} className="rs-smart-condition">
          <select aria-label={`${label} field`} value={condition.field} onChange={e => changeField(index, e.target.value)}>
            {SMART_FIELDS.map(f => <option key={f.id} value={f.id}>{f.label}</option>)}
          </select>
          {spec.ops && <select aria-label={`${label} operator`} value={condition.op || spec.ops[0]} onChange={e => setCondition(index, { op: e.target.value })}>
            {spec.ops.map(op => <option key={op} value={op}>{OP_LABELS[op]}</option>)}
          </select>}
          <ValueInput spec={spec} condition={condition} suggestions={suggestions} label={label} onChange={patch => setCondition(index, patch)} />
          <Button type="button" variant="ghost" size="icon-sm" aria-label={`Remove condition ${index + 1}`} onClick={() => remove(index)}><X size={15} /></Button>
        </li>;
      })}
    </ul>
    <Button type="button" variant="outline" size="sm" disabled={rules.conditions.length >= MAX_CONDITIONS} onClick={() => onChange({ ...rules, conditions: [...rules.conditions, newCondition('watched')] })}><Plus size={15} /> Add condition</Button>
    <div className="rs-smart-options">
      <label>Sort by
        <select value={rules.sort} onChange={e => onChange({ ...rules, sort: e.target.value })}>
          {SMART_SORTS.map(([value, name]) => <option key={value} value={value}>{name}</option>)}
        </select>
      </label>
      <label>Limit to <span>optional</span>
        <input type="number" inputMode="numeric" min={0} max={5000} placeholder="No limit" value={rules.limit || ''} onChange={e => onChange({ ...rules, limit: e.target.value === '' ? 0 : Number(e.target.value) })} />
      </label>
    </div>
  </div>;
}
