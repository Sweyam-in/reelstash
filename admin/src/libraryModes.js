// Each library has a mode that sets the words the media app uses and which
// pages it shows. Entertainment follows the Plex/Jellyfin conventions (movies,
// TV shows with seasons and episodes, cast & crew, genres); Private keeps
// ReelStash's original videos / performers / studios / series.

export const LIBRARY_MODES = [
  { id: 'entertainment', label: 'Entertainment', description: 'Movies and TV shows, with cast & crew, seasons and episodes.' },
  { id: 'private', label: 'Private videos', description: 'Videos with performers, studios and series.' },
];

const TERMS = {
  entertainment: {
    title: 'Title', titles: 'Titles', library: 'Movies & TV',
    movie: 'Movie', movies: 'Movies', show: 'TV show', shows: 'TV shows', season: 'Season', seasons: 'Seasons', episode: 'Episode', episodes: 'Episodes',
    person: 'Actor', people: 'Actors', cast: 'Cast', actors: 'Actors', director: 'Director', directors: 'Directors',
    studio: 'Studio', studios: 'Studios',
    series: 'Collection', seriesPlural: 'Collections',
    category: 'Genre', categories: 'Genres',
    searchPlaceholder: 'Search movies, shows, cast…',
  },
  private: {
    title: 'Video', titles: 'Videos', library: 'Library',
    show: 'Series', season: 'Season', episode: 'Episode',
    person: 'Performer', people: 'Performers', cast: 'Performers', actors: 'Performers', director: 'Director', directors: 'Directors',
    studio: 'Studio', studios: 'Studios',
    series: 'Series', seriesPlural: 'Series',
    category: 'Category', categories: 'Categories',
    searchPlaceholder: 'Search titles, studios, performers…',
  },
};

export function modeTerms(mode) {
  return TERMS[mode] || TERMS.private;
}
