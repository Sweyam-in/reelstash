import test, { beforeEach } from 'node:test';
import assert from 'node:assert/strict';

// A controllable EventSource: tests push server messages by hand.
const sources = [];
class FakeEventSource {
  constructor(url, opts) {
    this.url = url; this.opts = opts; this.closed = false; this.handlers = {};
    sources.push(this);
  }
  addEventListener(name, fn) { (this.handlers[name] ||= []).push(fn); }
  close() { this.closed = true; }
  emit(name, data) { (this.handlers[name] || []).forEach(fn => fn({ data: typeof data === 'string' ? data : JSON.stringify(data) })); }
}
globalThis.EventSource = FakeEventSource;

const { subscribe } = await import('./events.js');

beforeEach(() => { sources.length = 0; });

test('the first subscriber opens one credentialed connection', () => {
  const off = subscribe('jobs', () => {});
  assert.equal(sources.length, 1);
  assert.equal(sources[0].url, '/api/events');
  assert.deepEqual(sources[0].opts, { withCredentials: true });
  off();
});

test('subscribers share the connection and receive parsed payloads', () => {
  const a = [], b = [];
  const offA = subscribe('jobs', d => a.push(d));
  const offB = subscribe('jobs', d => b.push(d));
  assert.equal(sources.length, 1);
  sources[0].emit('jobs', { id: 1 });
  assert.deepEqual(a, [{ id: 1 }]);
  assert.deepEqual(b, [{ id: 1 }]);
  offA(); offB();
});

test('events for other names are not delivered', () => {
  const seen = [];
  const off = subscribe('notification', d => seen.push(d));
  sources[0].emit('jobs', { id: 1 });
  assert.deepEqual(seen, []);
  off();
});

test('a name subscribed after the connection opened is wired up too', () => {
  const first = subscribe('jobs', () => {});
  const seen = [];
  const second = subscribe('notification', d => seen.push(d));
  assert.equal(sources.length, 1);
  sources[0].emit('notification', { title: 'hi' });
  assert.deepEqual(seen, [{ title: 'hi' }]);
  first(); second();
});

test('malformed payloads are ignored rather than thrown', () => {
  const seen = [];
  const off = subscribe('jobs', d => seen.push(d));
  assert.doesNotThrow(() => sources[0].emit('jobs', '{not json'));
  assert.deepEqual(seen, []);
  off();
});

test('unsubscribing stops delivery, and the last one out closes the connection', () => {
  const seen = [];
  const offA = subscribe('jobs', d => seen.push(d));
  const offB = subscribe('jobs', () => {});
  const source = sources[0];
  offA();
  assert.equal(source.closed, false);
  source.emit('jobs', { n: 1 });
  assert.deepEqual(seen, []);
  offB();
  assert.equal(source.closed, true);
});

test('after everyone leaves, a new subscriber opens a fresh connection', () => {
  subscribe('jobs', () => {})();
  const off = subscribe('jobs', () => {});
  assert.equal(sources.length, 2);
  assert.equal(sources[0].closed, true);
  assert.equal(sources[1].closed, false);
  off();
});
