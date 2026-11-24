import assert from 'node:assert/strict';
import http from 'node:http';
import { test } from 'node:test';

import { configFromEnv, startControlPlane } from '../server.mjs';

const livekitEnv = {
  CONTROL_PLANE_LIVEKIT_URL: 'ws://127.0.0.1:7880',
  LIVEKIT_API_KEY: 'devkey',
  LIVEKIT_API_SECRET: 'test-secret',
  CONTROL_PLANE_BROADCAST_ROOM: 'broadcast',
  CONTROL_PLANE_GUESTS_ROOM: 'mosaic',
  CONTROL_PLANE_GUESTS_SOURCE_ROOM: 'guests',
  CONTROL_PLANE_STUDIO_ROOM: 'studio',
};

function sendJSON(response, statusCode, body) {
  const payload = JSON.stringify(body);
  response.writeHead(statusCode, {
    'Content-Type': 'application/json',
    'Content-Length': Buffer.byteLength(payload),
  });
  response.end(payload);
}

async function startHTTPServer(t, handler) {
  const server = http.createServer(handler);
  await new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(0, '127.0.0.1', resolve);
  });

  t.after(async () => {
    const closed = new Promise((resolve, reject) => {
      server.close((error) => (error ? reject(error) : resolve()));
    });
    server.closeAllConnections();
    await closed;
  });

  return `http://127.0.0.1:${server.address().port}`;
}

async function unusedBaseURL() {
  const server = http.createServer();
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  const { port } = server.address();
  await new Promise((resolve) => server.close(resolve));
  return `http://127.0.0.1:${port}`;
}

function mosaicHealth({
  status = 'ok',
  connected = true,
  connectionState = 'connected',
  room = 'guests',
  targetRoom = 'mosaic',
  state = 'idle',
  guests = 0,
  grid = null,
  rotationTimeoutSeconds = null,
} = {}) {
  const mosaic = { state, guests };
  if (grid !== null) {
    mosaic.grid = grid;
  }
  if (rotationTimeoutSeconds !== null) {
    mosaic.rotation_timeout_seconds = rotationTimeoutSeconds;
  }

  return {
    status,
    subsystems: {
      livekit: {
        status,
        connected,
        url: 'ws://livekit:7880',
        room,
        target_room: targetRoom,
        identity: 'mosaic-composer',
        connection_state: connectionState,
      },
      mosaic,
    },
  };
}

function mixerHealth({
  status = 'ok',
  connected = true,
  connectionState = 'connected',
  room = 'studio',
  targetRoom = 'broadcast',
  state = 'idle',
  inputs = 0,
  outputs = 1,
  guestBroadcastSeconds = 10,
  broadcastedParticipantIdentity = '',
  publishingParticipantIdentities = [],
} = {}) {
  return {
    status,
    subsystems: {
      livekit: {
        status,
        connected,
        url: 'ws://livekit:7880',
        room,
        target_room: targetRoom,
        identity: 'media-mixer',
        connection_state: connectionState,
      },
      mixer: {
        state,
        inputs,
        outputs,
        guest_broadcast_seconds: guestBroadcastSeconds,
        broadcasted_participant_identity: broadcastedParticipantIdentity,
        publishing_participant_identities: publishingParticipantIdentities,
      },
    },
  };
}

async function readRequestBody(request) {
  const chunks = [];
  for await (const chunk of request) {
    chunks.push(chunk);
  }
  return Buffer.concat(chunks).toString('utf8');
}

async function createFakeSubsystem(t, health, { onStart, onStop, onSwitch } = {}) {
  const calls = [];
  const baseURL = await startHTTPServer(t, async (request, response) => {
    const url = new URL(request.url ?? '/', 'http://subsystem.local');
    calls.push(`${request.method} ${url.pathname}`);

    if (request.method === 'GET' && url.pathname === '/health') {
      const result = health();
      if ('raw' in result) {
        response.writeHead(result.statusCode ?? 200, { 'Content-Type': 'application/json' });
        response.end(result.raw);
        return;
      }
      sendJSON(response, result.statusCode ?? 200, result.body);
      return;
    }
    if (request.method === 'POST' && url.pathname === '/start') {
      onStart?.();
      sendJSON(response, 200, { status: 'started' });
      return;
    }
    if (request.method === 'POST' && url.pathname === '/stop') {
      onStop?.();
      sendJSON(response, 200, { status: 'stopped' });
      return;
    }
    if (request.method === 'POST' && url.pathname === '/switch') {
      const rawBody = await readRequestBody(request);
      calls.push(`BODY /switch ${rawBody}`);
      const result = onSwitch?.(rawBody) ?? {
        statusCode: 409,
        body: { status: 'error', error: 'participant is not publishing video' },
      };
      sendJSON(response, result.statusCode, result.body);
      return;
    }
    sendJSON(response, 404, { status: 'error', error: 'not found' });
  });

  return { baseURL, calls };
}

async function startTestConsole(t, mosaicComposerURL, mediaMixerURL, extraEnv = {}) {
  const config = configFromEnv({
    ...livekitEnv,
    ...extraEnv,
    CONTROL_PLANE_LISTEN_ADDR: ':0',
    CONTROL_PLANE_MOSAIC_COMPOSER_URL: mosaicComposerURL,
    CONTROL_PLANE_MEDIA_MIXER_URL: mediaMixerURL,
  });
  const server = await startControlPlane(config);

  t.after(async () => {
    const closed = new Promise((resolve, reject) => {
      server.close((error) => (error ? reject(error) : resolve()));
    });
    server.closeAllConnections();
    await closed;
  });

  return `http://127.0.0.1:${server.address().port}`;
}

function findSubsystem(body, id) {
  const subsystem = body.subsystems.find((entry) => entry.id === id);
  assert.ok(subsystem, `subsystem ${id} is present`);
  return subsystem;
}

function findParameter(subsystem, name) {
  const parameter = subsystem.parameters.find((entry) => entry.name === name);
  assert.ok(parameter, `parameter ${name} is present`);
  return parameter.value;
}

function decodeJwtPayload(token) {
  const parts = token.split('.');
  assert.equal(parts.length, 3, 'token should have three JWT parts');
  return JSON.parse(Buffer.from(parts[1], 'base64url').toString('utf8'));
}

test('configFromEnv applies development defaults', () => {
  const config = configFromEnv(livekitEnv);

  assert.equal(config.listenAddr, ':7982');
  assert.equal(config.mediaMixerURL, 'http://127.0.0.1:7981');
  assert.equal(config.mosaicComposerURL, 'http://127.0.0.1:7980');
  assert.equal(config.healthcheckURL, 'http://127.0.0.1:7982/health');
  assert.equal(config.livekitUrl, 'ws://127.0.0.1:7880');
  assert.equal(config.broadcastRoom, 'broadcast');
  assert.equal(config.guestsRoom, 'mosaic');
  assert.equal(config.guestsSourceRoom, 'guests');
  assert.equal(config.studioRoom, 'studio');
  assert.equal(config.identityPrefix, 'control-plane');
});

test('configFromEnv reports every missing LiveKit setting', () => {
  assert.throws(
    () => configFromEnv({}),
    /CONTROL_PLANE_LIVEKIT_URL.*LIVEKIT_API_KEY.*LIVEKIT_API_SECRET.*CONTROL_PLANE_BROADCAST_ROOM.*CONTROL_PLANE_GUESTS_ROOM.*CONTROL_PLANE_GUESTS_SOURCE_ROOM.*CONTROL_PLANE_STUDIO_ROOM/,
  );
});

test('configFromEnv rejects a missing guests room', () => {
  const { CONTROL_PLANE_GUESTS_ROOM, ...env } = livekitEnv;

  assert.ok(CONTROL_PLANE_GUESTS_ROOM);
  assert.throws(() => configFromEnv(env), /CONTROL_PLANE_GUESTS_ROOM/);
});

test('configFromEnv rejects a missing guests source room', () => {
  const { CONTROL_PLANE_GUESTS_SOURCE_ROOM, ...env } = livekitEnv;

  assert.ok(CONTROL_PLANE_GUESTS_SOURCE_ROOM);
  assert.throws(() => configFromEnv(env), /CONTROL_PLANE_GUESTS_SOURCE_ROOM/);
});

test('configFromEnv rejects a missing studio room', () => {
  const { CONTROL_PLANE_STUDIO_ROOM, ...env } = livekitEnv;

  assert.ok(CONTROL_PLANE_STUDIO_ROOM);
  assert.throws(() => configFromEnv(env), /CONTROL_PLANE_STUDIO_ROOM/);
});

test('token endpoint returns a subscribe-only token for the broadcast room', async (t) => {
  const unusedURL = await unusedBaseURL();
  const consoleURL = await startTestConsole(t, unusedURL, unusedURL);

  const response = await fetch(`${consoleURL}/token`);
  assert.equal(response.status, 200);
  assert.equal(response.headers.get('content-type'), 'application/json');

  const body = await response.json();
  assert.equal(body.url, livekitEnv.CONTROL_PLANE_LIVEKIT_URL);
  assert.equal(body.room, 'broadcast');
  assert.match(body.identity, /^control-plane-/);
  assert.equal(typeof body.token, 'string');

  const claims = decodeJwtPayload(body.token);
  assert.equal(claims.sub, body.identity);
  assert.equal(claims.video.room, 'broadcast');
  assert.equal(claims.video.roomJoin, true);
  assert.equal(claims.video.canPublish, false);
  assert.equal(claims.video.canSubscribe, true);
});

test('token endpoint returns a subscribe-only token for the guests room', async (t) => {
  const unusedURL = await unusedBaseURL();
  const consoleURL = await startTestConsole(t, unusedURL, unusedURL);

  const response = await fetch(`${consoleURL}/token?room=guests`);
  assert.equal(response.status, 200);

  const body = await response.json();
  assert.equal(body.room, 'mosaic');
  assert.match(body.identity, /^control-plane-/);

  const claims = decodeJwtPayload(body.token);
  assert.equal(claims.video.room, 'mosaic');
  assert.equal(claims.video.roomJoin, true);
  assert.equal(claims.video.canPublish, false);
  assert.equal(claims.video.canSubscribe, true);
});

test('token endpoint returns a subscribe-only token for the guests source room', async (t) => {
  const unusedURL = await unusedBaseURL();
  const consoleURL = await startTestConsole(t, unusedURL, unusedURL);

  const response = await fetch(`${consoleURL}/token?room=guests-sources`);
  assert.equal(response.status, 200);

  const body = await response.json();
  assert.equal(body.room, 'guests');
  assert.match(body.identity, /^control-plane-/);

  const claims = decodeJwtPayload(body.token);
  assert.equal(claims.video.room, 'guests');
  assert.equal(claims.video.roomJoin, true);
  assert.equal(claims.video.canPublish, false);
  assert.equal(claims.video.canSubscribe, true);
});

test('token endpoint returns a subscribe-only token for the studio room', async (t) => {
  const unusedURL = await unusedBaseURL();
  const consoleURL = await startTestConsole(t, unusedURL, unusedURL);

  const response = await fetch(`${consoleURL}/token?room=studio`);
  assert.equal(response.status, 200);

  const body = await response.json();
  assert.equal(body.room, 'studio');
  assert.match(body.identity, /^control-plane-/);

  const claims = decodeJwtPayload(body.token);
  assert.equal(claims.video.room, 'studio');
  assert.equal(claims.video.roomJoin, true);
  assert.equal(claims.video.canPublish, false);
  assert.equal(claims.video.canSubscribe, true);
});

test('token endpoint returns a publishing token for the studio camera room', async (t) => {
  const unusedURL = await unusedBaseURL();
  const consoleURL = await startTestConsole(t, unusedURL, unusedURL);

  const response = await fetch(
    `${consoleURL}/token?room=studio-camera&name=${encodeURIComponent('CrowdStage Camera 1')}`,
  );
  assert.equal(response.status, 200);

  const body = await response.json();
  assert.equal(body.room, 'studio');
  assert.match(body.identity, /^control-plane-camera-/);

  const claims = decodeJwtPayload(body.token);
  assert.equal(claims.video.room, 'studio');
  assert.equal(claims.video.roomJoin, true);
  assert.equal(claims.video.canPublish, true);
  assert.equal(claims.video.canSubscribe, false);
  assert.equal(claims.name, 'CrowdStage Camera 1');
});

test('studio camera tokens fall back to a default participant name', async (t) => {
  const unusedURL = await unusedBaseURL();
  const consoleURL = await startTestConsole(t, unusedURL, unusedURL);

  const response = await fetch(`${consoleURL}/token?room=studio-camera`);

  assert.equal(response.status, 200);
  const body = await response.json();
  const claims = decodeJwtPayload(body.token);
  assert.equal(claims.name, 'Mixer Console Camera');
});

test('studio camera tokens sanitize and limit the participant name', async (t) => {
  const unusedURL = await unusedBaseURL();
  const consoleURL = await startTestConsole(t, unusedURL, unusedURL);
  const longName = `Camera\u0000\n${'x'.repeat(200)}`;

  const response = await fetch(
    `${consoleURL}/token?room=studio-camera&name=${encodeURIComponent(longName)}`,
  );

  assert.equal(response.status, 200);
  const body = await response.json();
  const claims = decodeJwtPayload(body.token);
  assert.equal(claims.name.length, 64);
  assert.equal(claims.name, `Camera ${'x'.repeat(57)}`);
  assert.ok(
    Array.from(claims.name).every((character) => {
      const code = character.codePointAt(0);
      return code >= 0x20 && !(code >= 0x7f && code <= 0x9f);
    }),
  );
});

test('token endpoint rejects unknown rooms', async (t) => {
  const unusedURL = await unusedBaseURL();
  const consoleURL = await startTestConsole(t, unusedURL, unusedURL);

  const response = await fetch(`${consoleURL}/token?room=unknown`);

  assert.equal(response.status, 400);
  assert.deepEqual(await response.json(), {
    status: 'error',
    error: 'unknown token room',
  });
});

test('only GET is allowed on the token endpoint', async (t) => {
  const unusedURL = await unusedBaseURL();
  const consoleURL = await startTestConsole(t, unusedURL, unusedURL);

  const response = await fetch(`${consoleURL}/token`, { method: 'POST' });

  assert.equal(response.status, 405);
  assert.equal(response.headers.get('allow'), 'GET');
});

test('health endpoint reports ok without depending on the subsystems', async (t) => {
  const unusedURL = await unusedBaseURL();
  const consoleURL = await startTestConsole(t, unusedURL, unusedURL);

  const response = await fetch(`${consoleURL}/health`);

  assert.equal(response.status, 200);
  assert.equal(response.headers.get('content-type'), 'application/json');
  assert.deepEqual(await response.json(), { status: 'ok' });
});

test('subsystem status reports both control areas while idle', async (t) => {
  const mosaic = await createFakeSubsystem(t, () => ({ body: mosaicHealth() }));
  const mixer = await createFakeSubsystem(t, () => ({ body: mixerHealth() }));
  const consoleURL = await startTestConsole(t, mosaic.baseURL, mixer.baseURL);

  const response = await fetch(`${consoleURL}/api/subsystems`);

  assert.equal(response.status, 200);
  const body = await response.json();
  assert.equal(body.status, 'ok');
  assert.equal(body.subsystems.length, 2);

  const mixerSubsystem = findSubsystem(body, 'media-mixer');
  assert.equal(mixerSubsystem.title, 'Media mixer');
  assert.equal(mixerSubsystem.reachable, true);
  assert.equal(mixerSubsystem.status, 'ok');
  assert.equal(mixerSubsystem.state, 'idle');
  assert.equal(findParameter(mixerSubsystem, 'LiveKit connection'), 'connected');
  assert.equal(findParameter(mixerSubsystem, 'Studio LiveKit room'), 'studio');
  assert.equal(findParameter(mixerSubsystem, 'Broadcast LiveKit room'), 'broadcast');
  assert.equal(findParameter(mixerSubsystem, 'Inputs'), '0');
  assert.equal(findParameter(mixerSubsystem, 'Outputs'), '1');
  assert.equal(findParameter(mixerSubsystem, 'Guest broadcast'), '10s');

  const mosaicSubsystem = findSubsystem(body, 'mosaic-composer');
  assert.equal(mosaicSubsystem.title, 'Mosaic composer');
  assert.equal(mosaicSubsystem.reachable, true);
  assert.equal(mosaicSubsystem.status, 'ok');
  assert.equal(mosaicSubsystem.state, 'idle');
  assert.equal(findParameter(mosaicSubsystem, 'LiveKit connection'), 'connected');
  assert.equal(findParameter(mosaicSubsystem, 'Guests LiveKit room'), 'guests');
  assert.equal(findParameter(mosaicSubsystem, 'Mosaic LiveKit room'), 'mosaic');
  assert.equal(
    mosaicSubsystem.parameters.some((parameter) => parameter.name === 'LiveKit room'),
    false,
    'the guests room parameter is labeled explicitly',
  );
  assert.equal(findParameter(mosaicSubsystem, 'Guests'), '0');
  assert.equal(
    mosaicSubsystem.parameters.some((parameter) => parameter.name === 'Grid'),
    false,
    'grid is not reported while the mosaic is idle',
  );

  assert.ok(mosaic.calls.includes('GET /health'));
  assert.ok(mixer.calls.includes('GET /health'));
});

test('subsystem status reports running parameters', async (t) => {
  const mosaic = await createFakeSubsystem(t, () => ({
    body: mosaicHealth({
      state: 'running',
      guests: 3,
      grid: { columns: 4, rows: 3 },
      rotationTimeoutSeconds: 10,
    }),
  }));
  const mixer = await createFakeSubsystem(t, () => ({
    body: mixerHealth({ state: 'running', inputs: 2, outputs: 1 }),
  }));
  const consoleURL = await startTestConsole(t, mosaic.baseURL, mixer.baseURL);

  const body = await (await fetch(`${consoleURL}/api/subsystems`)).json();

  assert.equal(body.status, 'ok');

  const mosaicSubsystem = findSubsystem(body, 'mosaic-composer');
  assert.equal(mosaicSubsystem.state, 'running');
  assert.equal(findParameter(mosaicSubsystem, 'Guests'), '3');
  assert.equal(findParameter(mosaicSubsystem, 'Grid'), '4x3');
  assert.equal(findParameter(mosaicSubsystem, 'Rotation timeout'), '10s');

  const mixerSubsystem = findSubsystem(body, 'media-mixer');
  assert.equal(mixerSubsystem.state, 'running');
  assert.equal(findParameter(mixerSubsystem, 'Inputs'), '2');
  assert.equal(findParameter(mixerSubsystem, 'Outputs'), '1');
  assert.equal(findParameter(mixerSubsystem, 'Guest broadcast'), '10s');
});

test('subsystem status reports the configured guest broadcast duration', async (t) => {
  const mosaic = await createFakeSubsystem(t, () => ({ body: mosaicHealth() }));
  const mixer = await createFakeSubsystem(t, () => ({
    body: mixerHealth({ state: 'running', guestBroadcastSeconds: 4 }),
  }));
  const consoleURL = await startTestConsole(t, mosaic.baseURL, mixer.baseURL);

  const body = await (await fetch(`${consoleURL}/api/subsystems`)).json();

  assert.equal(findParameter(findSubsystem(body, 'media-mixer'), 'Guest broadcast'), '4s');
});

test('subsystem status reports unreachable subsystems as degraded', async (t) => {
  const unreachableURL = await unusedBaseURL();
  const mixer = await createFakeSubsystem(t, () => ({ body: mixerHealth() }));
  const consoleURL = await startTestConsole(t, unreachableURL, mixer.baseURL);

  const response = await fetch(`${consoleURL}/api/subsystems`);

  assert.equal(response.status, 200);
  const body = await response.json();
  assert.equal(body.status, 'degraded');

  const mosaicSubsystem = findSubsystem(body, 'mosaic-composer');
  assert.equal(mosaicSubsystem.reachable, false);
  assert.equal(mosaicSubsystem.status, 'unreachable');
  assert.equal(mosaicSubsystem.state, null);
  assert.deepEqual(mosaicSubsystem.parameters, []);
  assert.ok(mosaicSubsystem.error, 'unreachable subsystem reports an error');
});

test('subsystem status keeps degraded subsystem health', async (t) => {
  const mosaic = await createFakeSubsystem(t, () => ({
    statusCode: 503,
    body: mosaicHealth({
      status: 'degraded',
      connected: false,
      connectionState: 'disconnected',
    }),
  }));
  const mixer = await createFakeSubsystem(t, () => ({ body: mixerHealth() }));
  const consoleURL = await startTestConsole(t, mosaic.baseURL, mixer.baseURL);

  const body = await (await fetch(`${consoleURL}/api/subsystems`)).json();

  assert.equal(body.status, 'degraded');

  const mosaicSubsystem = findSubsystem(body, 'mosaic-composer');
  assert.equal(mosaicSubsystem.reachable, true);
  assert.equal(mosaicSubsystem.status, 'degraded');
  assert.equal(mosaicSubsystem.state, 'idle');
  assert.equal(findParameter(mosaicSubsystem, 'LiveKit connection'), 'disconnected');
  assert.equal(findParameter(mosaicSubsystem, 'Guests LiveKit room'), 'guests');
  assert.equal(findParameter(mosaicSubsystem, 'Mosaic LiveKit room'), 'mosaic');
});

test('subsystem status reports malformed health as an error', async (t) => {
  const mosaic = await createFakeSubsystem(t, () => ({ raw: 'not-json' }));
  const mixer = await createFakeSubsystem(t, () => ({ body: mixerHealth() }));
  const consoleURL = await startTestConsole(t, mosaic.baseURL, mixer.baseURL);

  const body = await (await fetch(`${consoleURL}/api/subsystems`)).json();

  assert.equal(body.status, 'degraded');
  const mosaicSubsystem = findSubsystem(body, 'mosaic-composer');
  assert.equal(mosaicSubsystem.reachable, true);
  assert.equal(mosaicSubsystem.status, 'error');
  assert.equal(mosaicSubsystem.state, null);
  assert.deepEqual(mosaicSubsystem.parameters, []);
  assert.ok(mosaicSubsystem.error);
});

test('starting one subsystem leaves the other unchanged', async (t) => {
  let mixerState = 'idle';
  const mosaic = await createFakeSubsystem(t, () => ({ body: mosaicHealth() }));
  const mixer = await createFakeSubsystem(
    t,
    () => ({ body: mixerHealth({ state: mixerState }) }),
    {
      onStart: () => (mixerState = 'running'),
      onStop: () => (mixerState = 'idle'),
    },
  );
  const consoleURL = await startTestConsole(t, mosaic.baseURL, mixer.baseURL);

  const response = await fetch(`${consoleURL}/api/subsystems/media-mixer/start`, {
    method: 'POST',
  });

  assert.equal(response.status, 200);
  const body = await response.json();
  assert.equal(body.status, 'started');
  assert.equal(body.subsystem.id, 'media-mixer');
  assert.equal(body.subsystem.ok, true);
  assert.equal(body.subsystem.response.status, 'started');
  assert.ok(mixer.calls.includes('POST /start'));
  assert.equal(mosaic.calls.includes('POST /start'), false);

  const after = await (await fetch(`${consoleURL}/api/subsystems`)).json();
  assert.equal(findSubsystem(after, 'media-mixer').state, 'running');
  assert.equal(findSubsystem(after, 'mosaic-composer').state, 'idle');
});

test('stopping one subsystem leaves the other unchanged', async (t) => {
  let mosaicState = 'running';
  const mosaic = await createFakeSubsystem(
    t,
    () => ({ body: mosaicHealth({ state: mosaicState }) }),
    {
      onStart: () => (mosaicState = 'running'),
      onStop: () => (mosaicState = 'idle'),
    },
  );
  const mixer = await createFakeSubsystem(t, () => ({ body: mixerHealth({ state: 'idle' }) }));
  const consoleURL = await startTestConsole(t, mosaic.baseURL, mixer.baseURL);

  const response = await fetch(`${consoleURL}/api/subsystems/mosaic-composer/stop`, {
    method: 'POST',
  });

  assert.equal(response.status, 200);
  const body = await response.json();
  assert.equal(body.status, 'stopped');
  assert.equal(body.subsystem.response.status, 'stopped');
  assert.ok(mosaic.calls.includes('POST /stop'));
  assert.equal(mixer.calls.includes('POST /stop'), false);

  const after = await (await fetch(`${consoleURL}/api/subsystems`)).json();
  assert.equal(findSubsystem(after, 'mosaic-composer').state, 'idle');
});

test('subsystem actions report subsystem failures', async (t) => {
  const mosaic = await createFakeSubsystem(t, () => ({ body: mosaicHealth() }));
  const failingMixerURL = await startHTTPServer(t, (request, response) => {
    if (request.method === 'POST' && request.url === '/start') {
      sendJSON(response, 500, { status: 'error', error: 'pipeline failure' });
      return;
    }
    sendJSON(response, 200, mixerHealth());
  });
  const consoleURL = await startTestConsole(t, mosaic.baseURL, failingMixerURL);

  const response = await fetch(`${consoleURL}/api/subsystems/media-mixer/start`, {
    method: 'POST',
  });

  assert.equal(response.status, 502);
  const body = await response.json();
  assert.equal(body.status, 'error');
  assert.equal(body.subsystem.ok, false);
  assert.ok(body.subsystem.error);
});

test('subsystem actions report unreachable subsystems', async (t) => {
  const unreachableURL = await unusedBaseURL();
  const mixer = await createFakeSubsystem(t, () => ({ body: mixerHealth() }));
  const consoleURL = await startTestConsole(t, unreachableURL, mixer.baseURL);

  const response = await fetch(`${consoleURL}/api/subsystems/mosaic-composer/start`, {
    method: 'POST',
  });

  assert.equal(response.status, 502);
  const body = await response.json();
  assert.equal(body.status, 'error');
  assert.equal(body.subsystem.ok, false);
  assert.ok(body.subsystem.error);
});

test('unknown subsystems return a JSON 404', async (t) => {
  const unusedURL = await unusedBaseURL();
  const consoleURL = await startTestConsole(t, unusedURL, unusedURL);

  const response = await fetch(`${consoleURL}/api/subsystems/unknown/start`, {
    method: 'POST',
  });

  assert.equal(response.status, 404);
  assert.deepEqual(await response.json(), {
    status: 'error',
    error: 'unknown subsystem',
  });
});

test('only GET is allowed on the subsystem status endpoint', async (t) => {
  const unusedURL = await unusedBaseURL();
  const consoleURL = await startTestConsole(t, unusedURL, unusedURL);

  const response = await fetch(`${consoleURL}/api/subsystems`, { method: 'POST' });

  assert.equal(response.status, 405);
  assert.equal(response.headers.get('allow'), 'GET');
});

test('only POST is allowed on the subsystem action endpoints', async (t) => {
  const unusedURL = await unusedBaseURL();
  const consoleURL = await startTestConsole(t, unusedURL, unusedURL);

  const response = await fetch(`${consoleURL}/api/subsystems/media-mixer/start`);

  assert.equal(response.status, 405);
  assert.equal(response.headers.get('allow'), 'POST');
});

test('unknown paths return a JSON 404', async (t) => {
  const unusedURL = await unusedBaseURL();
  const consoleURL = await startTestConsole(t, unusedURL, unusedURL);

  const response = await fetch(`${consoleURL}/missing`);

  assert.equal(response.status, 404);
  assert.equal(response.headers.get('content-type'), 'application/json');
  assert.deepEqual(await response.json(), {
    status: 'error',
    error: 'not found',
  });
});

test('subsystem status reports the broadcasted and publishing studio identities', async (t) => {
  const mosaic = await createFakeSubsystem(t, () => ({ body: mosaicHealth() }));
  const mixer = await createFakeSubsystem(t, () => ({
    body: mixerHealth({
      state: 'running',
      inputs: 2,
      broadcastedParticipantIdentity: 'studio-a-video',
      publishingParticipantIdentities: ['studio-a-video', 'studio-b-video'],
    }),
  }));
  const consoleURL = await startTestConsole(t, mosaic.baseURL, mixer.baseURL);

  const body = await (await fetch(`${consoleURL}/api/subsystems`)).json();
  const mixerSubsystem = findSubsystem(body, 'media-mixer');

  assert.deepEqual(mixerSubsystem.studio, {
    broadcastedParticipantIdentity: 'studio-a-video',
    publishedParticipantIdentities: ['studio-a-video', 'studio-b-video'],
  });
  assert.equal(findSubsystem(body, 'mosaic-composer').studio, undefined);
});

test('studio stream switch forwards the selected participant identity', async (t) => {
  const switchRequests = [];
  const mosaic = await createFakeSubsystem(t, () => ({ body: mosaicHealth() }));
  const mixer = await createFakeSubsystem(
    t,
    () => ({ body: mixerHealth({ state: 'running' }) }),
    {
      onSwitch: (rawBody) => {
        switchRequests.push(JSON.parse(rawBody));
        return { statusCode: 200, body: { status: 'switched' } };
      },
    },
  );
  const consoleURL = await startTestConsole(t, mosaic.baseURL, mixer.baseURL);

  const response = await fetch(`${consoleURL}/api/subsystems/media-mixer/switch`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ participant_identity: 'studio-b-video' }),
  });

  assert.equal(response.status, 200);
  const body = await response.json();
  assert.equal(body.status, 'switched');
  assert.equal(body.subsystem.id, 'media-mixer');
  assert.equal(body.subsystem.ok, true);
  assert.equal(body.subsystem.response.status, 'switched');
  assert.deepEqual(switchRequests, [{ participant_identity: 'studio-b-video' }]);
  assert.ok(mixer.calls.includes('BODY /switch {"participant_identity":"studio-b-video"}'));
});

test('studio stream switch rejects a missing participant identity', async (t) => {
  const switchRequests = [];
  const mosaic = await createFakeSubsystem(t, () => ({ body: mosaicHealth() }));
  const mixer = await createFakeSubsystem(
    t,
    () => ({ body: mixerHealth() }),
    {
      onSwitch: (rawBody) => {
        switchRequests.push(rawBody);
        return { statusCode: 200, body: { status: 'switched' } };
      },
    },
  );
  const consoleURL = await startTestConsole(t, mosaic.baseURL, mixer.baseURL);

  const response = await fetch(`${consoleURL}/api/subsystems/media-mixer/switch`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ participant_identity: '' }),
  });

  assert.equal(response.status, 400);
  assert.deepEqual(await response.json(), {
    status: 'error',
    error: 'participant_identity is required',
  });
  assert.deepEqual(switchRequests, []);
});

test('studio stream switch reports when the participant has no video', async (t) => {
  const mosaic = await createFakeSubsystem(t, () => ({ body: mosaicHealth() }));
  const mixer = await createFakeSubsystem(t, () => ({ body: mixerHealth() }));
  const consoleURL = await startTestConsole(t, mosaic.baseURL, mixer.baseURL);

  const response = await fetch(`${consoleURL}/api/subsystems/media-mixer/switch`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ participant_identity: 'studio-missing-video' }),
  });

  assert.equal(response.status, 502);
  const body = await response.json();
  assert.equal(body.status, 'error');
  assert.equal(body.subsystem.ok, false);
  assert.match(body.subsystem.error, /not publishing video/);
});

test('token endpoint returns a subscribe-only token for the mosaic room', async (t) => {
  const unusedURL = await unusedBaseURL();
  const consoleURL = await startTestConsole(t, unusedURL, unusedURL);

  const response = await fetch(`${consoleURL}/token?room=mosaic`);

  assert.equal(response.status, 200);
  const body = await response.json();
  assert.equal(body.room, 'mosaic');
  assert.match(body.identity, /^control-plane-/);

  const claims = decodeJwtPayload(body.token);
  assert.equal(claims.video.room, 'mosaic');
  assert.equal(claims.video.roomJoin, true);
  assert.equal(claims.video.canPublish, false);
  assert.equal(claims.video.canSubscribe, true);
});

test('token endpoint accepts a configured room by its actual name', async (t) => {
  const unusedURL = await unusedBaseURL();
  const consoleURL = await startTestConsole(t, unusedURL, unusedURL, {
    CONTROL_PLANE_GUESTS_ROOM: 'mosaic-main',
  });

  const response = await fetch(`${consoleURL}/token?room=mosaic-main`);

  assert.equal(response.status, 200);
  const body = await response.json();
  assert.equal(body.room, 'mosaic-main');

  const claims = decodeJwtPayload(body.token);
  assert.equal(claims.video.room, 'mosaic-main');
  assert.equal(claims.video.canPublish, false);
  assert.equal(claims.video.canSubscribe, true);
});

test('token endpoint uses the requested identity prefix', async (t) => {
  const unusedURL = await unusedBaseURL();
  const consoleURL = await startTestConsole(t, unusedURL, unusedURL);

  const response = await fetch(
    `${consoleURL}/token?room=broadcast&identity_prefix=bus-monitor-broadcast`,
  );

  assert.equal(response.status, 200);
  const body = await response.json();
  assert.match(body.identity, /^bus-monitor-broadcast-/);
});

test('the API allows cross-origin requests from the client applications', async (t) => {
  const unusedURL = await unusedBaseURL();
  const consoleURL = await startTestConsole(t, unusedURL, unusedURL);

  const response = await fetch(`${consoleURL}/token?room=broadcast`, {
    headers: { Origin: 'http://127.0.0.1:7991' },
  });

  assert.equal(response.status, 200);
  assert.equal(response.headers.get('access-control-allow-origin'), '*');

  const preflight = await fetch(`${consoleURL}/api/subsystems/media-mixer/start`, {
    method: 'OPTIONS',
  });

  assert.equal(preflight.status, 204);
  assert.equal(preflight.headers.get('access-control-allow-origin'), '*');
  assert.match(
    preflight.headers.get('access-control-allow-methods') ?? '',
    /POST/,
  );
});
