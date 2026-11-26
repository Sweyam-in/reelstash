import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';

export const PAGE_SIZE = 30;

export function useInfiniteList(fetcher, key) {
  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;
  const [items, setItems] = useState([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState('');
  const [hasMore, setHasMore] = useState(false);
  const offsetRef = useRef(0);
  const requestRef = useRef(0);
  const inflight = useRef(false);
  const hasMoreRef = useRef(false);
  hasMoreRef.current = hasMore;

  const load = useCallback(async (reset) => {
    if (!reset && inflight.current) return;
    const requestId = ++requestRef.current;
    const offset = reset ? 0 : offsetRef.current;
    inflight.current = true;
    if (reset) {
      offsetRef.current = 0;
      setItems([]);
      setTotal(0);
      setHasMore(false);
      setLoading(true);
      setError('');
    } else {
      setLoadingMore(true);
    }
    try {
      const result = await fetcherRef.current({ limit: PAGE_SIZE, offset });
      if (requestId !== requestRef.current) return;
      const page = result.items || [];
      setItems(prev => (reset ? page : [...prev, ...page]));
      const next = offset + page.length;
      offsetRef.current = next;
      const reported = typeof result.total === 'number' ? result.total : null;
      setTotal(reported ?? next);
      setHasMore(reported != null ? next < reported : page.length === PAGE_SIZE);
    } catch (cause) {
      if (requestId !== requestRef.current) return;
      setError(cause.message || 'Could not load this view');
      if (reset) {
        setItems([]);
        setTotal(0);
        setHasMore(false);
      }
    } finally {
      if (requestId === requestRef.current) {
        inflight.current = false;
        setLoading(false);
        setLoadingMore(false);
      }
    }
  }, []);

  useEffect(() => { load(true); }, [key, load]);

  const loadMore = useCallback(() => {
    if (!hasMoreRef.current) return;
    load(false);
  }, [load]);
  const refresh = useCallback(() => load(true), [load]);

  return { items, total, loading, loadingMore, error, hasMore, loadMore, refresh };
}

export function useColumnCount(wide, narrow = 1, query = '(max-width: 1100px)') {
  const [columns, setColumns] = useState(() => (typeof window !== 'undefined' && window.matchMedia(query).matches ? narrow : wide));
  useEffect(() => {
    const media = window.matchMedia(query);
    const apply = () => setColumns(media.matches ? narrow : wide);
    apply();
    media.addEventListener('change', apply);
    return () => media.removeEventListener('change', apply);
  }, [wide, narrow, query]);
  return columns;
}

function nearestScrollParent(node) {
  let parent = node.parentElement;
  while (parent) {
    const { overflowY } = getComputedStyle(parent);
    if (overflowY === 'auto' || overflowY === 'scroll') return parent;
    parent = parent.parentElement;
  }
  return null;
}

export function VirtualWindow({
  items,
  minColumn = 220,
  columns,
  gapX = 14,
  gapY = 26,
  aspect,
  extra = 0,
  rowHeight,
  className = '',
  renderItem,
  onNearEnd,
  hasMore = false,
  scrollRef,
}) {
  const containerRef = useRef(null);
  const sentinelRef = useRef(null);
  const frameRef = useRef(0);
  const [width, setWidth] = useState(0);
  const [range, setRange] = useState({ start: 0, end: 0 });

  const cols = Math.max(1, columns || (width ? Math.floor((width + gapX) / (minColumn + gapX)) : 1));
  const colWidth = width > 0 ? (width - gapX * (cols - 1)) / cols : minColumn;
  const cellHeight = rowHeight || ((aspect ? colWidth / aspect : 0) + extra);
  const stride = Math.max(1, cellHeight + gapY);
  const rowCount = items.length ? Math.ceil(items.length / cols) : 0;
  const totalHeight = rowCount ? rowCount * cellHeight + Math.max(0, rowCount - 1) * gapY : 0;
  const startIndex = range.start * cols;
  const endIndex = Math.min(items.length, range.end * cols);

  useLayoutEffect(() => {
    const node = containerRef.current;
    if (!node) return undefined;
    const scrollEl = scrollRef?.current || nearestScrollParent(node);
    const update = () => {
      const bounds = node.getBoundingClientRect();
      const viewTop = scrollEl ? scrollEl.getBoundingClientRect().top : 0;
      const viewHeight = scrollEl ? scrollEl.clientHeight : window.innerHeight;
      const startPx = Math.max(0, viewTop - bounds.top);
      const start = Math.max(0, Math.floor(startPx / stride) - 1);
      const end = Math.min(rowCount, Math.ceil((startPx + viewHeight) / stride) + 2);
      setRange(current => (current.start === start && current.end === end ? current : { start, end }));
      setWidth(current => (current === node.clientWidth ? current : node.clientWidth));
    };
    update();
    const target = scrollEl || window;
    const onScroll = () => {
      cancelAnimationFrame(frameRef.current);
      frameRef.current = requestAnimationFrame(update);
    };
    target.addEventListener('scroll', onScroll, { passive: true });
    window.addEventListener('resize', onScroll);
    const observer = new ResizeObserver(onScroll);
    observer.observe(node);
    if (scrollEl) observer.observe(scrollEl);
    return () => {
      cancelAnimationFrame(frameRef.current);
      target.removeEventListener('scroll', onScroll);
      window.removeEventListener('resize', onScroll);
      observer.disconnect();
    };
  }, [stride, rowCount, scrollRef]);

  useEffect(() => {
    const sentinel = sentinelRef.current;
    if (!sentinel || !hasMore || !onNearEnd) return undefined;
    const root = scrollRef?.current || nearestScrollParent(sentinel);
    const observer = new IntersectionObserver(entries => {
      if (entries.some(entry => entry.isIntersecting)) onNearEnd();
    }, { root, rootMargin: '800px' });
    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [hasMore, onNearEnd, items.length, scrollRef, totalHeight]);

  return <div ref={containerRef} className={`rs-virtual ${className}`.trim()} style={{ height: totalHeight }}>
    {items.slice(startIndex, endIndex).map((item, sliceIndex) => {
      const index = startIndex + sliceIndex;
      const row = Math.floor(index / cols);
      const column = index % cols;
      return <div
        key={item.id ?? index}
        className="rs-virtual-cell"
        style={{ top: row * stride, left: column * (colWidth + gapX), width: colWidth, height: cellHeight }}
      >{renderItem(item, index)}</div>;
    })}
    <div ref={sentinelRef} className="rs-virtual-sentinel" />
  </div>;
}
