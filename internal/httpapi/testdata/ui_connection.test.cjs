const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, '../ui/app.js'), 'utf8');

function dashboard() {
  const elements = new Map();
  function element() {
    return {
      textContent: '', value: '', className: '', style: {}, dataset: {}, listeners: {}, children: [],
      classList: { add() {}, remove() {}, toggle() {} },
      append(...children) { this.children.push(...children); },
      replaceChildren(...children) { this.children = children; },
      setAttribute() {}, addEventListener(type, callback) { this.listeners[type] = callback; },
    };
  }
  const control = { mode: 'ok', calls: [], gate: null };
  const context = vm.createContext({
    Error, TypeError, AbortController,
    setTimeout: () => 1, clearTimeout() {},
    document: {
      querySelector(selector) {
        if (!elements.has(selector)) elements.set(selector, element());
        return elements.get(selector);
      },
      querySelectorAll: () => [], createElement: element,
    },
    window: { setInterval: () => 1, clearInterval() {}, confirm: () => true },
    IntersectionObserver: class { observe() {} },
    fetch: async (url) => {
      control.calls.push(url);
      if (control.gate) await control.gate;
      if (control.mode === 'offline') throw new TypeError('Failed to fetch');
      const data = url === '/v1/app' ? { version: 'test' }
        : url === '/v1/queue' ? { queued: 0, running: 0, succeeded: 1, failed: 0 }
        : url === '/v1/settings' ? { priorities: {}, rename_pattern: '{name} ({n}){ext}' }
        : control.mode === 'legacy' && (url === '/v1/folders' || url.startsWith('/v1/proposals')) ? null : [];
      const failed = control.mode === 'api-error' && url.startsWith('/v1/proposals');
      return { ok: !failed, status: failed ? 500 : 200,
        headers: { get: () => 'application/json' },
        json: async () => failed ? { error: 'database unavailable' } : data };
    },
  });
  return { control, context, elements,
    start() { vm.runInContext(source, context); return vm.runInContext('state.refreshPromise', context); },
    refresh() { return vm.runInContext('refreshAll(true)', context); },
    status() { return elements.get('#serverStatus').textContent; },
  };
}

test('empty and legacy null lists render without a connection error', async () => {
  const ui = dashboard();
  ui.control.mode = 'legacy';
  await ui.start();
  assert.equal(ui.status(), 'サーバー稼働中');
  assert.equal(ui.elements.get('#proposalList').children[0].textContent, '保留中の移動提案はありません。');
});

test('a disconnected dashboard fully recovers on the next polling cycle', async () => {
  const ui = dashboard();
  ui.control.mode = 'offline';
  await ui.start();
  assert.equal(ui.status(), '再接続待ち');
  ui.control.mode = 'ok';
  await ui.refresh();
  assert.equal(ui.status(), 'サーバー稼働中');
  assert.equal(ui.elements.get('#serverStatus').title, '');
  assert.equal(ui.elements.get('#countSucceeded').textContent, 1);
  assert.equal(ui.control.calls.filter((url) => url.startsWith('/v1/classifications')).length, 2);
});

test('HTTP errors are distinguished from a disconnected server', async () => {
  const ui = dashboard();
  ui.control.mode = 'api-error';
  await ui.start();
  assert.equal(ui.status(), 'データ取得エラー');
  assert.equal(ui.elements.get('#serverStatus').title, 'database unavailable');
  assert.equal(ui.elements.get('#countSucceeded').textContent, 1);
});

test('overlapping refreshes are coalesced and cannot overwrite shutdown status', async () => {
  const ui = dashboard();
  let release;
  ui.control.gate = new Promise((resolve) => { release = resolve; });
  const pending = ui.start();
  const repeated = ui.refresh();
  assert.equal(ui.control.calls.filter((url) => url === '/v1/queue').length, 1);
  vm.runInContext("state.shuttingDown = true; $('#serverStatus').textContent = '終了要求済み'", ui.context);
  release();
  await Promise.all([pending, repeated]);
  assert.equal(ui.status(), '終了要求済み');
  const before = ui.control.calls.length;
  await ui.refresh();
  assert.equal(ui.control.calls.length, before);
});
