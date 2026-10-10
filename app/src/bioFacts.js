// Provider bios often arrive as "Label: value" lines (Hair: Brown, Height: 5ft 8in / 173cm).
// splitBio pulls those out as facts so they can be shown as a compact grid, and keeps
// whatever is left as prose. Labels in `skip` (already shown elsewhere) are dropped.
const FACT = /^([A-Za-z][A-Za-z /&-]{0,28}):\s*(\S.{0,80})$/;

export function splitBio(text, skip = []) {
  const skipped = new Set(skip.map(s => s.toLowerCase()));
  const facts = [];
  const prose = [];
  for (const raw of String(text || '').split(/\r?\n/)) {
    const line = raw.trim();
    const match = line.match(FACT);
    // A long value ending like a sentence is prose that happens to contain a colon.
    if (match && (match[2].length <= 40 || !/[.!?]$/.test(match[2]))) {
      const label = match[1].trim();
      if (!skipped.has(label.toLowerCase())) facts.push({ label, value: match[2].trim() });
      continue;
    }
    prose.push(line);
  }
  return { facts, prose: prose.join('\n').replace(/\n{3,}/g, '\n\n').trim() };
}
