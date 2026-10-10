import React, { useState } from 'react';
import { getAnalytics, getUsageAnalytics } from '../../api';
import { ConsoleBarChart, DetailList, EmptyState, ErrorState, MetricStrip, PageHeader, PageStack, Panel, RangeTabs, RankedList, SegmentedTabs, SkeletonRows } from '../components';
import { formatBytes, formatHours, formatNumber, plural } from '../lib/format';
import { useResource } from '../lib/useResource';
import { useUrlState } from '../lib/useUrlState';

const RANGES = [{ value: '7', label: '7 days' }, { value: '30', label: '30 days' }, { value: '90', label: '90 days' }, { value: '365', label: '1 year' }];
const TIME_ZONE = Intl.DateTimeFormat().resolvedOptions().timeZone;
const dayLabel = (date, days) => { const d = new Date(`${date}T00:00`); return days > 90 ? d.toLocaleDateString(undefined, { month: 'short' }) : d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' }); };
const hourLabel = hour => new Date(2000, 0, 1, hour).toLocaleTimeString(undefined, { hour: 'numeric' });

export function AnalyticsPage() {
  const [filters, setFilters] = useUrlState({ days: '30' });
  const [ranking, setRanking] = useState('studios');
  const days = Number(filters.days) || 30;
  const usage = useResource(() => getUsageAnalytics(days, TIME_ZONE), [days]);
  const catalog = useResource(() => getAnalytics(), []);
  const data = usage.data;
  const period = RANGES.find(r => r.value === String(days))?.label || `${days} days`;

  return <PageStack>
    <PageHeader title="Analytics" subtitle="How the library is used. Totals across everyone; nobody’s individual watch history is shown."
      actions={<RangeTabs label="Time range" value={String(days)} onChange={v => setFilters({ days: v })} options={RANGES} />} />
    {usage.error && !data ? <Panel bodyless><ErrorState title="Couldn't load analytics" error={usage.error} onRetry={usage.reload} /></Panel>
      : !data ? <><MetricStrip label="Usage" loading items={[{ label: 'a' }, { label: 'b' }, { label: 'c' }, { label: 'd' }]} /><Panel bodyless><SkeletonRows rows={4} /></Panel></> : (() => {
        const { users, watch, streaming, library, top } = data;
        const streamed = streaming.directBytes + streaming.hlsBytes;
        const labels = watch.daily.map(d => dayLabel(d.date, days));
        const recent = catalog.data?.itemsAddedByDay || [];
        return <>
          <MetricStrip label="Usage" items={[
            { label: 'Active viewers', value: formatNumber(users.active), detail: `of ${plural(users.total, 'user')} · ${users.activeToday} today` },
            { label: 'Watch time', value: formatHours(watch.hours), detail: `${formatNumber(watch.sessions)} plays · ${formatNumber(watch.completed)} finished` },
            { label: 'Data streamed', value: formatBytes(streamed), detail: streamed ? `${Math.round((streaming.hlsBytes / streamed) * 100)}% transcoded` : 'Nothing streamed' },
            { label: 'Streaming now', value: formatNumber(streaming.now.streams), detail: `Peak ${streaming.peakStreams} at once in ${period}` },
          ]} />
          <Panel title="Watch time" subtitle="Hours watched per day, across all users.">
            {watch.hours >= 0.01 ? <ConsoleBarChart height={230} series={[{ id: 'hours', label: 'Hours', data: watch.daily.map(d => Number(d.hours.toFixed(2))) }]} labels={labels} yFormat={v => `${v} h`} />
              : <EmptyState compact title="No watch time in this period" message={watch.sessions ? 'Plays started, but no watch time was recorded yet.' : 'Nobody has watched anything in this period.'} />}
          </Panel>
          <Panel title="Data streamed" subtitle="Bytes sent to players each day: played directly, or transcoded to HLS.">
            {streamed ? <ConsoleBarChart height={230} stacked hideLegend={false} series={[{ id: 'direct', label: 'Direct play', data: streaming.daily.map(d => d.directBytes), stack: 'bytes' }, { id: 'hls', label: 'Transcoded', data: streaming.daily.map(d => d.hlsBytes), stack: 'bytes' }]} labels={streaming.daily.map(d => dayLabel(d.date, days))} yFormat={formatBytes} />
              : <EmptyState compact title="Nothing streamed in this period" message="Counting started with this version of ReelStash." />}
          </Panel>
          <div className="console-grid-2">
            <Panel title="When people watch" subtitle={`Plays by hour of day (${TIME_ZONE}).`}>
              {watch.sessions ? <ConsoleBarChart height={200} series={[{ id: 'plays', label: 'Plays', data: watch.byHour }]} labels={watch.byHour.map((_, h) => hourLabel(h))} /> : <EmptyState compact title="No plays yet" />}
            </Panel>
            <Panel title="Viewers per day" subtitle="Different people who watched something.">
              {watch.sessions ? <ConsoleBarChart height={200} series={[{ id: 'viewers', label: 'Viewers', data: watch.daily.map(d => d.viewers) }]} labels={labels} /> : <EmptyState compact title="No viewers yet" />}
            </Panel>
            <Panel title="Library growth" subtitle={`${plural(library.addedItems, 'title')} and ${formatBytes(library.addedBytes)} added in ${period}.`}>
              {library.addedItems ? <ConsoleBarChart height={200} series={[{ id: 'items', label: 'Titles added', data: library.daily.map(d => d.items) }]} labels={library.daily.map(d => dayLabel(d.date, days))} /> : <EmptyState compact title="Nothing added in this period" />}
            </Panel>
            {catalog.data && <Panel title="Added in the last 14 days" subtitle="New titles per day, whatever the range above.">
              {recent.some(d => d.count > 0) ? <ConsoleBarChart height={200} series={[{ id: 'added', label: 'Titles added', data: recent.map(d => d.count) }]} labels={recent.map(d => dayLabel(d.date, 14))} /> : <EmptyState compact title="No new titles in the last two weeks" />}
            </Panel>}
          </div>
          <div className="console-rankgrid">
            {catalog.data && <Panel title={`Top ${ranking}`} subtitle="By number of titles." actions={<SegmentedTabs label="Ranking" value={ranking} onChange={setRanking} options={[{ value: 'studios', label: 'Studios' }, { value: 'categories', label: 'Categories' }]} />}>
              <RankedList items={ranking === 'studios' ? catalog.data.topStudios : catalog.data.topCategories} value={i => i.count} empty={`No ${ranking} yet.`} />
            </Panel>}
            <Panel title="Most watched titles"><RankedList items={top.titles} value={i => i.hours} format={(v, i) => `${formatHours(v)} · ${i.plays}`} empty="No plays yet." /></Panel>
            <Panel title="Most watched people"><RankedList items={top.performers} value={i => i.hours} format={(v, i) => `${formatHours(v)} · ${i.plays}`} empty="No plays yet." /></Panel>
            <Panel title="Most watched studios"><RankedList items={top.studios} value={i => i.hours} format={(v, i) => `${formatHours(v)} · ${i.plays}`} empty="No plays yet." /></Panel>
          </div>
          <Panel title="Library and users" bodyless><DetailList flush items={[
            { term: 'Titles', details: `${formatNumber(library.items)} · ${formatHours(library.hours)} of video` }, { term: 'Size', details: formatBytes(library.bytes) }, { term: 'People', details: formatNumber(library.performers) },
            { term: 'Studios', details: formatNumber(library.studios) }, { term: 'Collections / series', details: formatNumber(library.series) },
            { term: 'Users', details: `${users.total} (${plural(users.admins, 'admin')}${users.disabled ? `, ${users.disabled} disabled` : ''})` }, { term: 'New users', details: `${users.new} in ${period}` },
          ]} /></Panel>
        </>;
      })()}
  </PageStack>;
}
