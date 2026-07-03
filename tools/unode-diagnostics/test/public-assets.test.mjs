import { readFile } from 'node:fs/promises';
import assert from 'node:assert/strict';
import test from 'node:test';

async function readPublicFile(name) {
  return readFile(new URL(`../public/${name}`, import.meta.url), 'utf8');
}

test('browser workbench public assets expose required UI and API wiring', async () => {
  const [html, css, js] = await Promise.all([
    readPublicFile('index.html'),
    readPublicFile('styles.css'),
    readPublicFile('app.js')
  ]);

  for (const id of [
    'history-list',
    'history-filter',
    'preset-select',
    'load-env',
    'send-request',
    'target',
    'method',
    'base-url',
    'path',
    'headers-json',
    'body-json',
    'response-headers',
    'response-body'
  ]) {
    assert.match(html, new RegExp(`id="${id}"`));
  }

  assert.match(html, /href="\/styles\.css"/);
  assert.match(html, /src="\/app\.js"/);
  assert.match(css, /\.workbench/);
  assert.match(css, /\.history-pane/);
  assert.match(css, /\.request-pane/);
  assert.match(css, /\.response-pane/);

  for (const endpoint of ['/api/config', '/api/presets', '/api/history', '/api/send', '/api/history/']) {
    assert.match(js, new RegExp(endpoint.replaceAll('/', '\\/')));
  }
});
