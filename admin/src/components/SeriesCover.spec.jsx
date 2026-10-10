import React from 'react';
import { describe, it, expect } from 'vitest';
import { act, render } from '@testing-library/react';
import SeriesCover from './SeriesCover';

const imgs = container => [...container.querySelectorAll('img')];
const cover = (id, poster) => ({ id, posterImageId: poster });

describe('SeriesCover', () => {
  it('renders an empty tile without a series', () => {
    const { container } = render(<SeriesCover />);
    expect(container.firstChild).toHaveClass('is-empty');
  });

  it('prefers the series poster', () => {
    const { container } = render(<SeriesCover series={{ id: 3, name: 'Saga', posterImageId: 8, covers: [cover(1)] }} />);
    expect(imgs(container)).toHaveLength(1);
    expect(imgs(container)[0]).toHaveAttribute('src', '/api/series/3/image?v=8');
  });

  it('falls back to a placeholder when the poster fails to load and there are no thumbnails', () => {
    const { container } = render(<SeriesCover series={{ id: 3, name: 'Saga', posterImageId: 8 }} />);
    act(() => { imgs(container)[0].dispatchEvent(new Event('error')); });
    expect(imgs(container)).toHaveLength(0);
    expect(container.firstChild).toHaveClass('is-empty', 'rs-mono');
  });

  it('builds a collage from the first videos when there is no poster', () => {
    for (const count of [1, 2, 4]) {
      const covers = Array.from({ length: count }, (_, i) => cover(i + 1, 5));
      const { container, unmount } = render(<SeriesCover series={{ id: 1, name: 'S', covers }} />);
      expect(imgs(container)).toHaveLength(count);
      expect(container.firstChild).toHaveClass(`has-${count}`);
      unmount();
    }
  });

  it('lays three thumbnails out on a four-tile grid with a filler', () => {
    const { container } = render(<SeriesCover series={{ id: 1, name: 'S', covers: [cover(1), cover(2), cover(3)] }} />);
    expect(container.firstChild).toHaveClass('has-4');
    expect(container.querySelector('.rs-pl-cover-fill')).not.toBeNull();
  });

  it('shows at most four thumbnails', () => {
    const covers = Array.from({ length: 9 }, (_, i) => cover(i + 1));
    const { container } = render(<SeriesCover series={{ id: 1, name: 'S', covers }} />);
    expect(imgs(container)).toHaveLength(4);
  });

  it('uses a thumbnail when the poster fails, then drops the tile if that fails too', () => {
    const { container } = render(<SeriesCover series={{ id: 1, name: 'S', covers: [cover(1, 9), cover(2)] }} />);
    expect(imgs(container)[0]).toHaveAttribute('src', '/api/media/1/artwork?kind=poster&v=9');
    act(() => { imgs(container)[0].dispatchEvent(new Event('error')); });
    expect(imgs(container)).toHaveLength(2);
    expect(imgs(container)[0]).toHaveAttribute('src', '/api/media/1/artwork?kind=thumbnail&v=9');
    act(() => { imgs(container)[0].dispatchEvent(new Event('error')); });
    expect(imgs(container)).toHaveLength(1);
    expect(container.firstChild).toHaveClass('has-1');
  });

  it('accepts an extra class', () => {
    const { container } = render(<SeriesCover series={{ id: 1, name: 'S' }} className="big" />);
    expect(container.firstChild).toHaveClass('big');
  });
});
