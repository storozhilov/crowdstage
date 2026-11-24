import assert from 'node:assert/strict';
import { test } from 'node:test';

import { configFromEnv, parseListenAddr, startMixerConsole } from '../server.mjs';

const baseEnv = {
  MIXER_CONSOLE_CONTROL_PLANE_URL: 'http://127.0.0.1:7982',
};

async function startTestServer(t, env = {}) {
  const config = configFromEnv({
    ...baseEnv,
    ...env,
    MIXER_CONSOLE_LISTEN_ADDR: ':0',
  });
  const server = await startMixerConsole(config);

  t.after(async () => {
    const closed = new Promise((resolve, reject) => {
      server.close((error) => (error ? reject(error) : resolve()));
    });
    server.closeAllConnections();
    await closed;
  });

  const address = server.address();
  return `http://127.0.0.1:${address.port}`;
}

test('configFromEnv applies development defaults', () => {
  const config = configFromEnv({});

  assert.equal(config.listenAddr, ':7991');
  assert.equal(config.controlPlaneURL, 'http://127.0.0.1:7982');
  assert.equal(config.healthcheckURL, 'http://127.0.0.1:7991/health');
});

test('configFromEnv reads overrides', () => {
  const config = configFromEnv({
    MIXER_CONSOLE_LISTEN_ADDR: ':9000',
    MIXER_CONSOLE_CONTROL_PLANE_URL: 'http://control-plane:7982',
    MIXER_CONSOLE_HEALTHCHECK_URL: 'http://127.0.0.1:9000/health',
  });

  assert.equal(config.listenAddr, ':9000');
  assert.equal(config.controlPlaneURL, 'http://control-plane:7982');
  assert.equal(config.healthcheckURL, 'http://127.0.0.1:9000/health');
});

test('parseListenAddr accepts a host and port', () => {
  assert.deepEqual(parseListenAddr('127.0.0.1:7991'), { host: '127.0.0.1', port: 7991 });
  assert.deepEqual(parseListenAddr(':7991'), { host: undefined, port: 7991 });
  assert.throws(() => parseListenAddr('7991'), /invalid listen address/);
  assert.throws(() => parseListenAddr(':99999'), /invalid listen address/);
});

test('health endpoint reports ok', async (t) => {
  const baseURL = await startTestServer(t);

  const response = await fetch(`${baseURL}/health`);

  assert.equal(response.status, 200);
  assert.equal(response.headers.get('content-type'), 'application/json');
  assert.deepEqual(await response.json(), { status: 'ok' });
});

test('the index page receives the ControlPlane URL', async (t) => {
  const baseURL = await startTestServer(t, {
    MIXER_CONSOLE_CONTROL_PLANE_URL: 'http://control-plane:7982',
  });

  const response = await fetch(`${baseURL}/`);
  const body = await response.text();

  assert.equal(response.status, 200);
  assert.match(response.headers.get('content-type'), /^text\/html/);
  assert.match(body, /window\.__MIXER_CONSOLE_CONFIG__/);
  assert.match(body, /"controlPlaneURL":"http:\/\/control-plane:7982"/);
});

test('the REST API is not served by MixerConsole anymore', async (t) => {
  const baseURL = await startTestServer(t);

  for (const path of ['/token', '/api/subsystems']) {
    const response = await fetch(`${baseURL}${path}`);
    assert.equal(response.status, 404);
    assert.deepEqual(await response.json(), { status: 'error', error: 'not found' });
  }
});

test('only GET is allowed on the health endpoint', async (t) => {
  const baseURL = await startTestServer(t);

  const response = await fetch(`${baseURL}/health`, { method: 'POST' });

  assert.equal(response.status, 405);
  assert.equal(response.headers.get('allow'), 'GET');
});

test('unknown paths return a JSON 404', async (t) => {
  const baseURL = await startTestServer(t);

  const response = await fetch(`${baseURL}/missing`);

  assert.equal(response.status, 404);
  assert.equal(response.headers.get('content-type'), 'application/json');
  assert.deepEqual(await response.json(), {
    status: 'error',
    error: 'not found',
  });
});
