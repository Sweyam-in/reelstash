import { describe, it, expect, beforeEach, vi } from 'vitest';
import { __resetQueueForTests, nextInQueue, syncPlaying } from './queue.js';
import { fetchPlaylistItems, fetchRandomTitles, playAll } from './queueActions.js';

let requests;
function serve(handler) {
  vi.stubGlobal('fetch', vi.fn(async url => {
    requests.push(url);
    return { ok: true, status: 200, json: async () => handler(new URL(url, 'http://x')) };
  }));
}
const titles = (from, count) => Array.from({ length: count }, (_, i) => ({ id: from + i, title: `T${from + i}` }));

beforeEach(() => { requests = []; __resetQueueForTests(); });

describe('fetchPlaylistItems', () => {
  it('walks every page of a long playlist in order', async () => {
    serve(url => {
      const offset = Number(url.searchParams.get('offset'));
      return { items: titles(offset + 1, Math.min(200, 450 - offset)), total: 450 };
    });
    const items = await fetchPlaylistItems(9);
    expect(items).toHaveLength(450);
    expect(items.slice(0, 3).map(i => i.id)).toEqual([1, 2, 3]);
    expect(items.at(-1).id).toBe(450);
    expect(requests).toEqual(['/api/collections/9?limit=200&offset=0', '/api/collections/9?limit=200&offset=200', '/api/collections/9?limit=200&offset=400']);
  });

  it('stops after one page when the playlist is short', async () => {
    serve(() => ({ items: titles(1, 5), total: 5 }));
    expect(await fetchPlaylistItems(1)).toHaveLength(5);
    expect(requests).toHaveLength(1);
  });

  it('returns nothing for an empty playlist', async () => {
    serve(() => ({ total: 0 }));
    expect(await fetchPlaylistItems(1)).toEqual([]);
  });
});

describe('fetchRandomTitles', () => {
  it('asks for a random sort with the same filters', async () => {
    serve(() => ({ items: titles(1, 3) }));
    expect(await fetchRandomTitles({ studioId: 4 }, 25)).toHaveLength(3);
    const params = new URL(requests[0], 'http://x').searchParams;
    expect(params.get('sort')).toBe('random');
    expect(params.get('limit')).toBe('25');
    expect(params.get('studioId')).toBe('4');
  });

  it('defaults to 100 titles and tolerates an empty result', async () => {
    serve(() => ({}));
    expect(await fetchRandomTitles({})).toEqual([]);
    expect(new URL(requests[0], 'http://x').searchParams.get('limit')).toBe('100');
  });
});

describe('playAll', () => {
  it('opens the first title with autoplay and queues the rest', () => {
    const navigate = vi.fn();
    const first = playAll(titles(1, 3), { navigate });
    expect(first.id).toBe(1);
    expect(navigate).toHaveBeenCalledWith('/library/1', { state: { autoplay: true } });
    expect(nextInQueue().id).toBe(2);
  });

  it('does nothing when there is nothing to play', () => {
    const navigate = vi.fn();
    expect(playAll([], { navigate })).toBeNull();
    expect(navigate).not.toHaveBeenCalled();
  });

  it('a shuffled run plays every title exactly once', () => {
    const navigate = vi.fn();
    playAll(titles(1, 20), { shuffle: true, navigate });
    const played = [Number(navigate.mock.calls[0][0].split('/').pop())];
    syncPlaying(played[0]);
    for (let next = nextInQueue(); next; next = nextInQueue()) {
      played.push(next.id);
      syncPlaying(next.id);
    }
    expect(played).toHaveLength(20);
    expect(new Set(played).size).toBe(20);
  });
});
