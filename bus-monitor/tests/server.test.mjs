import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  configFromEnv,
  parseArgs,
  startBusMonitor,
} from '../server.mjs';

const baseEnv = {
  BUS_MONITOR_LIVEKIT_URL: 'ws://127.0.0.1:7880',
  LIVEKIT_API_KEY: 'devkey',
  LIVEKIT_API_SECRET: 'test-secret',
  BUS_MONITOR_ROOM: 'studio',
};

function decodeJwtPayload(token) {
  const parts = token.split('.');
  assert.equal(parts.length, 3, 'token should have three JWT parts');
  return JSON.parse(Buffer.from(parts[1], 'base64url').toString('utf8'));
}

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
  assert.equal(config.livekitUrl, 'ws://127.0.0.1:7880');
  assert.equal(config.room, 'studio');
  assert.equal(config.identityPrefix, 'bus-monitor');
  assert.equal(config.healthcheckURL, 'http://127.0.0.1:7990/health');
});

test('configFromEnv reports every missing setting', () => {
  assert.throws(
    () => configFromEnv({}),
    /BUS_MONITOR_LIVEKIT_URL.*LIVEKIT_API_KEY.*LIVEKIT_API_SECRET.*BUS_MONITOR_ROOM/,
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

test('health endpoint reports ok and the monitored room', async (t) => {
  const baseURL = await startTestServer(t);

  const response = await fetch(`${baseURL}/health`);

  assert.equal(response.status, 200);
  assert.equal(response.headers.get('content-type'), 'application/json');
  assert.deepEqual(await response.json(), { status: 'ok', room: 'studio' });
});

test('token endpoint returns a subscribe-only token for the configured room', async (t) => {
  const baseURL = await startTestServer(t, { BUS_MONITOR_ROOM: 'broadcast' });

  const response = await fetch(`${baseURL}/token`);
  assert.equal(response.status, 200);
  assert.equal(response.headers.get('content-type'), 'application/json');

  const body = await response.json();
  assert.equal(body.url, baseEnv.BUS_MONITOR_LIVEKIT_URL);
  assert.equal(body.room, 'broadcast');
  assert.match(body.identity, /^bus-monitor-/);
  assert.equal(typeof body.token, 'string');

  const claims = decodeJwtPayload(body.token);
  assert.equal(claims.sub, body.identity);
  assert.equal(claims.video.room, 'broadcast');
  assert.equal(claims.video.roomJoin, true);
  assert.equal(claims.video.canPublish, false);
  assert.equal(claims.video.canSubscribe, true);
});

test('only GET is allowed on API endpoints', async (t) => {
  const baseURL = await startTestServer(t);

  const response = await fetch(`${baseURL}/token`, { method: 'POST' });

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
