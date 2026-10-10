import { modeTerms } from './libraryModes.js';

// The admin console covers every library at once, so its words have to fit both modes:
// "People" for cast, crew and performers, "Collections / series", "Categories & genres".
// Once an admin narrows a page to one library it switches to that library's own words
// (Actors / Collections / Genres for Entertainment, Performers / Series / Categories for Private).
const NEUTRAL = {
  person: 'Person', people: 'People',
  studio: 'Studio', studios: 'Studios',
  series: 'Collection / series', seriesPlural: 'Collections / series',
  category: 'Category / genre', categories: 'Categories & genres',
  title: 'Title', titles: 'Titles',
};

export function adminTerms(library) {
  if (!library) return NEUTRAL;
  const t = modeTerms(library.mode);
  return {
    person: t.person, people: t.people,
    studio: t.studio, studios: t.studios,
    series: t.series, seriesPlural: t.seriesPlural,
    category: t.category, categories: t.categories,
    title: t.title, titles: t.titles,
  };
}

// Sidebar and command-palette labels never depend on a library.
export const ADMIN_NAV_TERMS = {
  people: 'People', studios: 'Studios', series: 'Collections / series', categories: 'Categories & genres',
};
