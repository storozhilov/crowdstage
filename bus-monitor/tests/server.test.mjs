import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  configFromEnv,
  parseArgs,
  startBusMonitor,
  tokenURL,
} from '../server.mjs';

const baseEnv = {
  BUS_MONITOR_CONTROL_PLANE_URL: 'http://127.0.0.1:7982',
  BUS_MONITOR_ROOM: 'studio',
};

async function startTestServer(t, env = {}) {
  const config = configFromEnv({
    ...baseEnv,
    ...env,
    BUS_MONITOR_LISTEN_ADDR: ':0',
  });
  const server = await startBusMonitor(config);

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
  const config = configFromEnv(baseEnv);

  assert.equal(config.listenAddr, ':7990');
  assert.equal(config.controlPlaneURL, 'http://127.0.0.1:7982');
  assert.equal(config.room, 'studio');
  assert.equal(config.identityPrefix, 'bus-monitor');
  assert.equal(config.healthcheckURL, 'http://127.0.0.1:7990/health');
});

test('configFromEnv reports every missing setting', () => {
  assert.throws(
    () => configFromEnv({}),
    /BUS_MONITOR_CONTROL_PLANE_URL.*BUS_MONITOR_ROOM/,
  );
});

test('configFromEnv uses the environment room when no option is given', () => {
  const config = configFromEnv({ ...baseEnv, BUS_MONITOR_ROOM: 'broadcast' }, {});

  assert.equal(config.room, 'broadcast');
});

test('configFromEnv lets the command line room override the environment', () => {
  const config = configFromEnv({ ...baseEnv, BUS_MONITOR_ROOM: 'studio' }, { room: 'broadcast' });

  assert.equal(config.room, 'broadcast');
});

test('parseArgs accepts the room as an option or a positional argument', () => {
  assert.deepEqual(parseArgs(['--room', 'broadcast']), { room: 'broadcast' });
  assert.deepEqual(parseArgs(['--room=broadcast']), { room: 'broadcast' });
  assert.deepEqual(parseArgs(['-r', 'broadcast']), { room: 'broadcast' });
  assert.deepEqual(parseArgs(['broadcast']), { room: 'broadcast' });
  assert.deepEqual(parseArgs(['--help']), { help: true });
});

test('parseArgs rejects incomplete or conflicting room arguments', () => {
  assert.throws(() => parseArgs(['--room']), /requires a room name/);
  assert.throws(() => parseArgs(['--room', '--help']), /requires a room name/);
  assert.throws(() => parseArgs(['--room', '-r', 'broadcast']), /requires a room name/);
  assert.throws(() => parseArgs(['--room=']), /must not be empty/);
  assert.throws(() => parseArgs(['--room', 'studio', 'broadcast']), /more than once/);
  assert.throws(() => parseArgs(['--room=studio', '--room=broadcast']), /more than once/);
  assert.throws(() => parseArgs(['--unknown']), /unknown option/);
});

test('tokenURL points at the ControlPlane token endpoint', () => {
  const url = new URL(tokenURL(configFromEnv({ ...baseEnv, BUS_MONITOR_ROOM: 'broadcast' })));

  assert.equal(url.origin, 'http://127.0.0.1:7982');
  assert.equal(url.pathname, '/token');
  assert.equal(url.searchParams.get('room'), 'broadcast');
  assert.equal(url.searchParams.get('identity_prefix'), 'bus-monitor');
});

test('health endpoint reports ok and the monitored room', async (t) => {
  const baseURL = await startTestServer(t);

  const response = await fetch(`${baseURL}/health`);

  assert.equal(response.status, 200);
  assert.equal(response.headers.get('content-type'), 'application/json');
  assert.deepEqual(await response.json(), { status: 'ok', room: 'studio' });
});

test('the index page receives the ControlPlane token URL', async (t) => {
  const baseURL = await startTestServer(t, {
    BUS_MONITOR_ROOM: 'broadcast',
    BUS_MONITOR_CONTROL_PLANE_URL: 'http://control-plane:7982',
    BUS_MONITOR_IDENTITY: 'bus-monitor-broadcast',
  });

  const response = await fetch(`${baseURL}/`);
  const body = await response.text();

  assert.equal(response.status, 200);
  assert.match(response.headers.get('content-type'), /^text\/html/);
  assert.match(body, /window\.__BUS_MONITOR_CONFIG__/);
  assert.match(
    body,
    /http:\/\/control-plane:7982\/token\?room=broadcast&identity_prefix=bus-monitor-broadcast/,
  );
});

test('the token endpoint is not served by BusMonitor anymore', async (t) => {
  const baseURL = await startTestServer(t);

  const response = await fetch(`${baseURL}/token`);

  assert.equal(response.status, 404);
  assert.deepEqual(await response.json(), { status: 'error', error: 'not found' });
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
