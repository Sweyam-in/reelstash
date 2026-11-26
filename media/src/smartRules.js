// Saved filters behind a smart playlist. The shapes match the server's rules JSON:
//   { match: 'all' | 'any', conditions: [{ field, op?, value? }], sort, limit }
// In the editor every value is held as typed (a string or boolean) and converted by toServerRules.

export const OP_LABELS = { gte: 'at least', lte: 'at most', eq: 'exactly', is: 'is', isNot: 'is not', contains: 'contains' };

// kind: bool | text | int | date | choice. `fixed` fields have no operator.
export const SMART_FIELDS = [
  { id: 'watched', label: 'Watched by me', kind: 'bool', yes: 'Watched', no: 'Not watched yet' },
  { id: 'favorite', label: 'Favorite', kind: 'bool', yes: 'Is a favorite', no: 'Is not a favorite' },
  { id: 'watchLater', label: 'Watch later', kind: 'bool', yes: 'Is in Watch later', no: 'Is not in Watch later' },
  { id: 'rating', label: 'My rating', kind: 'int', ops: ['gte', 'lte', 'eq'], unit: 'stars', min: 1, max: 5, initial: '4' },
  { id: 'avgRating', label: 'Average rating', kind: 'int', ops: ['gte', 'lte', 'eq'], unit: 'stars', min: 1, max: 5, initial: '4' },
  { id: 'tag', label: 'Tag', kind: 'text', ops: ['is', 'isNot'], suggest: 'tags' },
  { id: 'category', label: 'Category / genre', kind: 'text', ops: ['is', 'isNot'], suggest: 'categories' },
  { id: 'studio', label: 'Studio', kind: 'text', ops: ['is', 'contains'] },
  { id: 'performer', label: 'Performer / actor', kind: 'text', ops: ['is', 'contains'] },
  { id: 'series', label: 'Series / collection', kind: 'text', ops: ['is', 'contains'] },
  { id: 'text', label: 'Words in title or cast', kind: 'text', placeholder: 'Marvel' },
  { id: 'kind', label: 'Type', kind: 'choice', choices: [['movie', 'Movie'], ['episode', 'Episode']] },
  { id: 'year', label: 'Release year', kind: 'int', ops: ['gte', 'lte', 'eq'], min: 1800, max: 2200, initial: '2000' },
  { id: 'addedWithin', label: 'Added in the last', kind: 'int', unit: 'days', min: 0, max: 100000, initial: '30' },
  { id: 'addedYear', label: 'Added in the year', kind: 'int', min: 1800, max: 2200, initial: String(new Date().getFullYear()) },
  { id: 'addedAfter', label: 'Added on or after', kind: 'date' },
  { id: 'addedBefore', label: 'Added on or before', kind: 'date' },
  { id: 'duration', label: 'Runtime', kind: 'int', ops: ['gte', 'lte'], unit: 'minutes', min: 0, max: 100000, initial: '90' },
];

export const SMART_SORTS = [
  ['added', 'Newest added'], ['oldest', 'Oldest added'], ['title', 'Title A–Z'], ['rated', 'Highest rated'],
  ['watched', 'Most watched'], ['released', 'Newest release'], ['series', 'Season and episode'],
];

export const MAX_CONDITIONS = 20;
const fieldOf = id => SMART_FIELDS.find(f => f.id === id);

export function newCondition(fieldId = 'watched') {
  const spec = fieldOf(fieldId) || SMART_FIELDS[0];
  const base = { field: spec.id };
  if (spec.ops) base.op = spec.ops[0];
  if (spec.kind === 'bool') base.value = false;
  else if (spec.kind === 'int') base.value = spec.initial ?? '';
  else if (spec.kind === 'choice') base.value = spec.choices[0][0];
  else base.value = '';
  return base;
}

export const emptyRules = () => ({ match: 'all', conditions: [newCondition('watched')], sort: 'added', limit: 0 });

/** Rules as saved by the server, normalised for the editor (numbers become strings the inputs can hold). */
export function fromServerRules(rules) {
  if (!rules || !Array.isArray(rules.conditions)) return emptyRules();
  return {
    match: rules.match === 'any' ? 'any' : 'all',
    sort: rules.sort || 'added',
    limit: Number(rules.limit) || 0,
    conditions: rules.conditions.map(c => {
      const spec = fieldOf(c.field);
      const value = c.value === undefined ? (spec?.kind === 'bool' ? true : '') : spec?.kind === 'int' ? String(c.value) : c.value;
      return { field: c.field, ...(c.op ? { op: c.op } : spec?.ops ? { op: spec.ops[0] } : {}), value };
    }),
  };
}

/** Editor state to what the server accepts: numbers as numbers, blank text dropped by validateRules first. */
export function toServerRules(rules) {
  return {
    match: rules.match,
    sort: rules.sort,
    ...(rules.limit > 0 ? { limit: Number(rules.limit) } : {}),
    conditions: rules.conditions.map(c => {
      const spec = fieldOf(c.field);
      const out = { field: c.field };
      if (spec?.ops) out.op = c.op || spec.ops[0];
      if (spec?.kind === 'int') out.value = Number(c.value);
      else if (spec?.kind === 'bool') out.value = Boolean(c.value);
      else out.value = String(c.value ?? '').trim();
      return out;
    }),
  };
}

/** A message for the first thing wrong with the rules, or '' when they can be saved. */
export function validateRules(rules) {
  if (!rules.conditions.length) return 'Add at least one condition.';
  if (rules.conditions.length > MAX_CONDITIONS) return `Use at most ${MAX_CONDITIONS} conditions.`;
  for (const c of rules.conditions) {
    const spec = fieldOf(c.field);
    if (!spec) return 'Pick what each condition checks.';
    if (spec.kind === 'text' && !String(c.value ?? '').trim()) return `Enter a value for “${spec.label}”.`;
    if (spec.kind === 'date' && !/^\d{4}-\d{2}-\d{2}$/.test(String(c.value ?? ''))) return `Choose a date for “${spec.label}”.`;
    if (spec.kind === 'int') {
      const n = Number(c.value);
      if (c.value === '' || !Number.isInteger(n)) return `Enter a whole number for “${spec.label}”.`;
      if (n < spec.min || n > spec.max) return `“${spec.label}” must be between ${spec.min} and ${spec.max}.`;
    }
  }
  if (rules.limit < 0 || rules.limit > 5000) return 'The title limit must be between 0 and 5000.';
  return '';
}

/** One condition in words, e.g. “My rating at least 4 stars”. */
export function describeCondition(c) {
  const spec = fieldOf(c.field);
  if (!spec) return c.field;
  if (spec.kind === 'bool') return c.value === false ? spec.no : spec.yes;
  if (spec.kind === 'choice') return `${spec.label} is ${(spec.choices.find(([v]) => v === c.value) || [, c.value])[1].toLowerCase()}`;
  const op = c.op ? OP_LABELS[c.op] : '';
  const value = spec.kind === 'text' ? `“${c.value}”` : `${c.value}${spec.unit ? ` ${spec.unit}` : ''}`;
  return [spec.label, op, value].filter(Boolean).join(' ');
}

/** The whole rule set as a short sentence. */
export function describeRules(rules) {
  const joiner = rules.match === 'any' ? ' or ' : ' and ';
  return rules.conditions.map(describeCondition).join(joiner);
}
