import React from 'react';
import { Link } from 'react-router-dom';
import { ListVideo, Trash2, X } from 'lucide-react';
import { clearQueue, removeFromQueue, setAutoplay, useQueue } from '../queue';

/** "Up next": the play queue, with the title now playing highlighted. Renders nothing when the queue is empty. */
export default function QueuePanel({ currentId }) {
  const queue = useQueue();
  if (!queue.items.length) return null;
  const upcoming = queue.index >= 0 ? queue.items.length - queue.index - 1 : queue.items.length;
  return <section className="rs-queue" aria-label="Up next">
    <div className="rs-queue-head">
      <div><p className="rs-eyebrow">PLAY QUEUE</p><h3>Up next<span className="rs-count">{upcoming}</span></h3></div>
      <div className="rs-queue-controls">
        <label className="rs-queue-autoplay"><input type="checkbox" checked={queue.autoplay} onChange={event => setAutoplay(event.target.checked)} /> Autoplay next</label>
        <button type="button" className="rs-button rs-button-outline" onClick={clearQueue}><Trash2 size={15} /> Clear</button>
      </div>
    </div>
    <ol className="rs-queue-list">
      {queue.items.map((item, index) => {
        const playing = String(item.id) === String(currentId);
        return <li key={item.id} className={playing ? 'is-playing' : undefined}>
          <span className="rs-queue-number">{playing ? <ListVideo size={15} /> : String(index + 1).padStart(2, '0')}</span>
          <Link to={`/library/${item.id}`} state={{ autoplay: true }} aria-current={playing ? 'true' : undefined}>
            <strong>{item.title}</strong>
            {item.studioName && <small>{item.studioName}</small>}
          </Link>
          <button type="button" className="rs-marker-icon" aria-label={`Remove ${item.title} from the queue`} onClick={() => removeFromQueue(item.id)}><X size={15} /></button>
        </li>;
      })}
    </ol>
  </section>;
}
