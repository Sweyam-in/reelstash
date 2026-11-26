import React from 'react';
import { Link } from 'react-router-dom';
import { Bookmark, Building2, ChevronRight, Clapperboard, Clock3, Heart, Layers3, Library, ListVideo, Settings, Tv, Users } from 'lucide-react';
import { useLibrary } from '../libraryContext';
import { LibrarySwitcher } from '../frame/AppFrame';

// "You" on phones: everything the desktop sidebar holds that the tab bar has no room for.
export default function You() {
  const { terms, mode } = useLibrary();
  const sections = [
    { title: 'Saved', links: [['Favorites', '/favorites', Heart], ['Watch later', '/watch-later', Bookmark], ['Playlists', '/playlists', ListVideo]] },
    { title: 'Browse', links: [
      ...(mode === 'entertainment' ? [[terms.shows, '/shows', Tv]] : []),
      [terms.seriesPlural, '/series', Library],
      [terms.people, '/performers', Users],
      ...(mode === 'entertainment' ? [[terms.directors, '/directors', Clapperboard]] : []),
      [terms.studios, '/studios', Building2],
      [terms.categories, '/categories', Layers3],
    ] },
    { title: 'You', links: [['Activity', '/activity', Clock3], ['Settings', '/settings', Settings]] },
  ];
  return <div className="rs-page rs-you">
    <header className="rs-page-head"><div><p className="rs-eyebrow">Your library</p><h1 className="rs-page-title">You</h1></div></header>
    <LibrarySwitcher className="rs-you-switcher" />
    {sections.map(section => <section key={section.title} className="rs-you-section" aria-labelledby={`you-${section.title}`}>
      <h2 id={`you-${section.title}`} className="rs-eyebrow">{section.title}</h2>
      <ul className="rs-you-list">
        {section.links.map(([label, path, Icon]) => <li key={path}><Link to={path} className="rs-you-link"><span className="rs-you-icon" aria-hidden="true"><Icon size={18} /></span><span>{label}</span><ChevronRight size={18} aria-hidden="true" /></Link></li>)}
      </ul>
    </section>)}
  </div>;
}
