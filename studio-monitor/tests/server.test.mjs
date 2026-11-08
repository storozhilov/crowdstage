import assert from 'node:assert/strict';
import { test } from 'node:test';

import { configFromEnv, startStudioMonitor } from '../server.mjs';

const baseEnv = {
  STUDIO_MONITOR_LIVEKIT_URL: 'ws://127.0.0.1:7880',
  LIVEKIT_API_KEY: 'devkey',
  LIVEKIT_API_SECRET: 'test-secret',
};

function decodeJwtPayload(token) {
  const parts = token.split('.');
  assert.equal(parts.length, 3, 'token should have three JWT parts');
  return JSON.parse(Buffer.from(parts[1], 'base64url').toString('utf8'));
}

async function startTestServer(t) {
  const config = configFromEnv({
    ...baseEnv,
    STUDIO_MONITOR_LISTEN_ADDR: ':0',
  });
  const server = await startStudioMonitor(config);

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
  assert.equal(config.identityPrefix, 'studio-monitor');
});

test('configFromEnv reports every missing LiveKit setting', () => {
  assert.throws(
    () => configFromEnv({}),
    /STUDIO_MONITOR_LIVEKIT_URL.*LIVEKIT_API_KEY.*LIVEKIT_API_SECRET/,
  );
});

test('health endpoint reports ok', async (t) => {
  const baseURL = await startTestServer(t);

  const response = await fetch(`${baseURL}/health`);

  assert.equal(response.status, 200);
  assert.equal(response.headers.get('content-type'), 'application/json');
  assert.deepEqual(await response.json(), { status: 'ok' });
});

test('token endpoint returns a subscribe-only token for the studio room', async (t) => {
  const baseURL = await startTestServer(t);

  const response = await fetch(`${baseURL}/token`);
  assert.equal(response.status, 200);
  assert.equal(response.headers.get('content-type'), 'application/json');

  const body = await response.json();
  assert.equal(body.url, baseEnv.STUDIO_MONITOR_LIVEKIT_URL);
  assert.equal(body.room, 'studio');
  assert.match(body.identity, /^studio-monitor-/);
  assert.equal(typeof body.token, 'string');

  const claims = decodeJwtPayload(body.token);
  assert.equal(claims.sub, body.identity);
  assert.equal(claims.video.room, 'studio');
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
