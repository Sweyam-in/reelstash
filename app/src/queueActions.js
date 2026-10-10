import { getCollection, getMediaItems } from './api';
import { shuffled, startQueue } from './queue';

const PAGE = 200; // the server's largest page

/** Every title in a playlist, in playlist order. */
export async function fetchPlaylistItems(id) {
  const items = [];
  for (let offset = 0; ; offset += PAGE) {
    const result = await getCollection(id, { limit: PAGE, offset });
    const page = result.items || [];
    items.push(...page);
    if (page.length < PAGE || items.length >= result.total) return items;
  }
}

/** Up to `limit` random titles matching the same filters a list page uses. */
export async function fetchRandomTitles(filters, limit = 100) {
  const result = await getMediaItems({ ...filters, sort: 'random', limit });
  return result.items || [];
}

/**
 * Replaces the queue with `items` (optionally shuffled) and opens the first one, set to play.
 * Returns the title it opened, or null when there was nothing to play.
 */
export function playAll(items, { shuffle = false, navigate }) {
  const first = startQueue(shuffle ? shuffled(items) : items);
  if (first) navigate(`/library/${first.id}`, { state: { autoplay: true } });
  return first;
}
