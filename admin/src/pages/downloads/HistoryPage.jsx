import React, { useCallback, useEffect, useState } from 'react';
import { Badge, Button, useToast } from '@spatika/react';
import { History, RefreshCw, RotateCcw } from 'lucide-react';
import { forgetDownload, getDownloadHistory } from '../../api';
import { moveLabel } from '../../downloadsModel';
import { timeAgo } from '../timeUtil';

/** What the move rules did with each finished torrent, newest first. */
export default function HistoryPage() {
  const { toast } = useToast();
  const [items, setItems] = useState(null);
  const load = useCallback(() => getDownloadHistory(200).then(res => setItems(res.items || []))
    .catch(err => { setItems([]); toast({ title: 'Could not load history', description: err.message, tone: 'danger' }); }), [toast]);
  useEffect(() => { load(); const id = setInterval(load, 10000); return () => clearInterval(id); }, [load]);

  async function forget(item) {
    try { await forgetDownload(item.hash); toast({ title: 'Forgotten', description: 'The move rules will look at it again if it finished after they were switched on.', tone: 'success' }); load(); }
    catch (err) { toast({ title: 'Could not forget it', description: err.message, tone: 'danger' }); }
  }

  return <div className="rs-dl-page">
    <section className="rs-dl-card">
      <header className="rs-dl-card-head">
        <div><h2><History size={18} /> History</h2><p>Every finished torrent the move rules looked at, and what happened.</p></div>
        <div className="rs-dl-card-actions"><Button size="sm" variant="ghost" onClick={load} aria-label="Refresh"><RefreshCw size={14} /></Button></div>
      </header>
      {items === null && <p className="rs-move-empty"><RefreshCw size={14} className="rs-spin" /></p>}
      {items?.length === 0 && <div className="rs-dl-empty"><strong>Nothing yet</strong><span>Turn on move rules, and finished torrents show up here.</span></div>}
      {!!items?.length && <ul className="rs-dl-history">{items.map(h => {
        const ml = moveLabel({ status: h.status, ruleName: h.ruleName, message: h.message });
        return <li key={h.hash}>
          <div className="rs-dl-history-main">
            <strong title={h.name}>{h.name}</strong>
            <span>
              <Badge variant={ml?.tone || 'neutral'}>{h.status === 'SKIPPED' ? 'Not moved' : ml?.label}</Badge>
              {h.dest && h.status !== 'SKIPPED' && <code title={h.from ? `From ${h.from}` : ''}>{h.dest}</code>}
              {h.library && <span className="rs-dl-chip is-library">{h.library}</span>}
            </span>
            {h.message && <small className={h.status === 'FAILED' ? 'is-error' : ''}>{h.message}</small>}
          </div>
          <span className="rs-dl-history-when" title={new Date(h.updatedAt).toLocaleString()}>{timeAgo(h.updatedAt)}</span>
          {h.status !== 'MOVING' && <Button size="sm" variant="ghost" title="Let the rules look at it again" aria-label={`Forget ${h.name}`} onClick={() => forget(h)}><RotateCcw size={14} /></Button>}
        </li>;
      })}</ul>}
    </section>
  </div>;
}
