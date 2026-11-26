import React, { useEffect, useState } from 'react';
import { ListVideo } from 'lucide-react';
import { artworkUrl, seriesImageUrl } from '../api';
import { monogram } from './Monogram';

function CoverImage({ cover, onBroken }) {
  const [kind, setKind] = useState('poster');
  const src = artworkUrl(cover.id, kind, cover.posterImageId);
  return <img key={src} src={src} alt="" decoding="async" onError={() => { if (kind === 'poster') setKind('thumbnail'); else onBroken(); }} />;
}

/**
 * A series' cover: its poster (uploaded or from a matched provider) when it has one, otherwise a collage
 * of its first videos' posters like a playlist (1 = full-bleed, 2-4 = collage), otherwise a tinted placeholder.
 * A missing poster falls back to that video's thumbnail, cropped into the same poster frame.
 */
export default function SeriesCover({ series, className = '' }) {
  const [posterFailed, setPosterFailed] = useState(false);
  const [broken, setBroken] = useState(() => new Set());
  useEffect(() => setPosterFailed(false), [series?.id, series?.posterImageId]);
  if (!series) return <span className={`rs-pl-cover is-empty ${className}`} />;
  if (series.posterImageId && !posterFailed) {
    return <span className={`rs-pl-cover ${className}`}><img src={seriesImageUrl(series.id, series.posterImageId)} alt="" loading="lazy" onError={() => setPosterFailed(true)} /></span>;
  }
  const shown = (series.covers || []).filter(cover => !broken.has(cover.id)).slice(0, 4);
  if (!shown.length) {
    const mono = monogram(series.name);
    return <span className={`rs-pl-cover is-empty rs-mono ${className}`} style={mono.style}><ListVideo size={34} strokeWidth={1.4} /></span>;
  }
  return <span className={`rs-pl-cover has-${shown.length === 3 ? 4 : shown.length} ${className}`}>
    {shown.map(cover => <CoverImage key={cover.id} cover={cover} onBroken={() => setBroken(prev => new Set(prev).add(cover.id))} />)}
    {shown.length === 3 && <span className="rs-pl-cover-fill" />}
  </span>;
}
