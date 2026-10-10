import React from 'react';
import { Link } from 'react-router-dom';
import { Skeleton } from '@spatika/react';
import { ArrowLeft, ChevronRight } from 'lucide-react';

/** Page title block: what this page is for, plus its actions. One primary action at most. */
export function PageHeader({ title, subtitle, actions, back }) {
  return <header className="console-page-header">
    <div className="console-page-header__copy">
      {back && <Link to={back.to} className="console-page-header__back"><ArrowLeft size={14} aria-hidden="true" />{back.label}</Link>}
      <h1 className="console-page-header__title">{title}</h1>
      {subtitle && <p className="console-page-header__subtitle">{subtitle}</p>}
    </div>
    {actions && <div className="console-page-header__actions">{actions}</div>}
  </header>;
}

/** Vertical rhythm and max width for a page body. */
export function PageStack({ children, className = '' }) {
  return <div className={`console-stack ${className}`.trim()}>{children}</div>;
}

/** A framed object: table, chart, feed. `bodyless` lets children run edge to edge (tables, lists). */
export function Panel({ title, subtitle, actions, footer, children, className = '', id, bodyless = false, ...rest }) {
  return <section className={`console-panel ${className}`.trim()} id={id} aria-label={typeof title === 'string' ? title : undefined} {...rest}>
    {(title || actions) && <div className="console-panel__head">
      <div>{title && <h2>{title}</h2>}{subtitle && <p className="console-panel__sub">{subtitle}</p>}</div>
      {actions && <div className="console-panel__actions">{actions}</div>}
    </div>}
    {bodyless ? children : <div className="console-panel__body">{children}</div>}
    {footer && <div className="console-panel__foot">{footer}</div>}
  </section>;
}

export function PanelLink({ to, children }) {
  return <Link to={to} className="console-section__link">{children}<ChevronRight size={14} aria-hidden="true" /></Link>;
}

/**
 * One ruled row of figures, used where a card grid would compete with the working table.
 * items: [{ label, value, detail, tone: 'ok'|'warning'|'danger', to }]
 */
export function MetricStrip({ items, label, loading = false }) {
  return <dl className="console-metrics" aria-label={label} aria-busy={loading || undefined}>
    {items.map(item => {
      const body = <>
        <dt>{item.label}</dt>
        <dd>
          {loading ? <>
            <Skeleton className="console-metrics__skeleton-value" />
            <Skeleton className="console-metrics__skeleton-detail" />
          </> : <>
            <span className="console-metrics__value">{item.value}</span>
            {item.detail && <span className="console-metrics__detail">{item.detail}</span>}
          </>}
        </dd>
      </>;
      return <div key={item.label} className="console-metrics__item" data-tone={item.tone ?? 'default'}>
        {item.to && !loading ? <Link to={item.to} className="console-metrics__link">{body}</Link> : body}
      </div>;
    })}
  </dl>;
}

/** Sibling views of one section (Storage › Disks / Scan rules). Each tab is a route. */
export function SectionTabs({ entries, activePath, label }) {
  return <nav className="console-segmented-tabs console-segmented-tabs--page" aria-label={label}>
    {entries.map(entry => <Link key={entry.path} to={entry.path} aria-current={entry.path === activePath ? 'page' : undefined}>{entry.label}</Link>)}
  </nav>;
}
