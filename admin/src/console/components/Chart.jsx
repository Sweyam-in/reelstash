import React from 'react';
import { AreaChart, BarChart } from '@spatika/charts';
import { formatNumber } from '../lib/format';

/** Ranked bars (formats, folders, top studios). items: [{ name, ... }] */
export function RankedList({ items, value, format = v => formatNumber(v), empty = 'Nothing yet.' }) {
  if (!items?.length) return <p className="console-muted console-small">{empty}</p>;
  const max = Math.max(...items.map(value), 1);
  return <ul className="console-ranked">
    {items.map(item => <li key={item.name}>
      <span className="console-ranked__label" title={item.name}>{item.name}</span>
      <span className="console-ranked__track"><span style={{ width: `${Math.max(2, (value(item) / max) * 100)}%` }} /></span>
      <span className="console-ranked__count">{format(value(item), item)}</span>
    </li>)}
  </ul>;
}

// A bar chart on the console palette. Series colours come from CSS so they follow the theme.
export function ConsoleBarChart({ height = 220, stacked, series, labels, yFormat, hideLegend = true }) {
  return <div className="console-chart" style={{ height }}>
    <BarChart height={height} stacked={stacked} series={series} xAxis={[{ data: labels, scaleType: 'band' }]} yAxis={[{ min: 0, valueFormatter: yFormat }]}
      colors={['var(--console-chart-1)', 'var(--console-chart-2)', 'var(--console-chart-3)']} hideLegend={hideLegend} legendPosition={hideLegend ? undefined : 'top'} />
  </div>;
}

export function ConsoleAreaChart({ height = 220, series, labels, yFormat, yMax }) {
  return <div className="console-chart" style={{ height }}>
    <AreaChart height={height} series={series} xAxis={[{ data: labels, scaleType: 'point' }]} yAxis={[{ min: 0, max: yMax, valueFormatter: yFormat }]} colors={['var(--console-chart-1)']} hideLegend />
  </div>;
}
