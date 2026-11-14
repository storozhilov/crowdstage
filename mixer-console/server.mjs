import { randomUUID } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import http from 'node:http';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

import { AccessToken } from 'livekit-server-sdk';

const DEFAULT_LISTEN_ADDR = ':7991';
const DEFAULT_MEDIA_MIXER_URL = 'http://127.0.0.1:7981';
const DEFAULT_MOSAIC_COMPOSER_URL = 'http://127.0.0.1:7980';
const DEFAULT_HEALTHCHECK_URL = 'http://127.0.0.1:7991/health';
const DEFAULT_IDENTITY = 'mixer-console';
const TOKEN_TTL = '1h';
const SUBSYSTEM_TIMEOUT_MS = 5000;
const STATIC_DIR = fileURLToPath(new URL('./dist/', import.meta.url));

const REQUIRED_ENV = [
  ['livekitUrl', 'MIXER_CONSOLE_LIVEKIT_URL'],
  ['apiKey', 'LIVEKIT_API_KEY'],
  ['apiSecret', 'LIVEKIT_API_SECRET'],
  ['broadcastRoom', 'MIXER_CONSOLE_BROADCAST_ROOM'],
  ['guestsRoom', 'MIXER_CONSOLE_GUESTS_ROOM'],
  ['studioRoom', 'MIXER_CONSOLE_STUDIO_ROOM'],
];

const TOKEN_ROOMS = new Map([
  ['broadcast', (config) => config.broadcastRoom],
  ['guests', (config) => config.guestsRoom],
  ['studio', (config) => config.studioRoom],
]);

const STATIC_FILES = new Map([
  ['/', { file: 'index.html', type: 'text/html; charset=utf-8' }],
  ['/index.html', { file: 'index.html', type: 'text/html; charset=utf-8' }],
  ['/app.js', { file: 'app.js', type: 'text/javascript; charset=utf-8' }],
  ['/styles.css', { file: 'styles.css', type: 'text/css; charset=utf-8' }],
]);

const SUBSYSTEMS = [
  {
    id: 'media-mixer',
    title: 'Media mixer',
    urlKey: 'mediaMixerURL',
    pipelineState: (health) => health?.subsystems?.mixer?.state ?? null,
    parameters: (health) => {
      const mixer = health?.subsystems?.mixer ?? {};
      return [
        parameter('Inputs', mixer.inputs),
        parameter('Outputs', mixer.outputs),
      ];
    },
  },
  {
    id: 'mosaic-composer',
    title: 'Mosaic composer',
    urlKey: 'mosaicComposerURL',
    pipelineState: (health) => health?.subsystems?.mosaic?.state ?? null,
    parameters: (health) => {
      const livekit = health?.subsystems?.livekit ?? {};
      const mosaic = health?.subsystems?.mosaic ?? {};
      return [
        parameter('LiveKit connection', livekit.connection_state),
        parameter('LiveKit room', livekit.room),
        parameter('Guests', mosaic.guests),
        parameter('Grid', formatGrid(mosaic.grid)),
        parameter('Rotation timeout', formatSeconds(mosaic.rotation_timeout_seconds)),
      ];
    },
  },
];

export function configFromEnv(env = process.env) {
  const config = {
    listenAddr: env.MIXER_CONSOLE_LISTEN_ADDR || DEFAULT_LISTEN_ADDR,
    mediaMixerURL: env.MIXER_CONSOLE_MEDIA_MIXER_URL || DEFAULT_MEDIA_MIXER_URL,
    mosaicComposerURL: env.MIXER_CONSOLE_MOSAIC_COMPOSER_URL || DEFAULT_MOSAIC_COMPOSER_URL,
    healthcheckURL: env.MIXER_CONSOLE_HEALTHCHECK_URL || DEFAULT_HEALTHCHECK_URL,
    livekitUrl: env.MIXER_CONSOLE_LIVEKIT_URL || '',
    apiKey: env.LIVEKIT_API_KEY || '',
    apiSecret: env.LIVEKIT_API_SECRET || '',
    broadcastRoom: env.MIXER_CONSOLE_BROADCAST_ROOM || '',
    guestsRoom: env.MIXER_CONSOLE_GUESTS_ROOM || '',
    studioRoom: env.MIXER_CONSOLE_STUDIO_ROOM || '',
    identityPrefix: env.MIXER_CONSOLE_IDENTITY || DEFAULT_IDENTITY,
  };

  const missing = REQUIRED_ENV
    .filter(([key]) => config[key] === '')
    .map(([, name]) => name);

  if (missing.length > 0) {
    throw new Error(`missing required setting(s): ${missing.join(', ')}`);
  }

  return config;
}

export function parseListenAddr(address) {
  const separator = address.lastIndexOf(':');
  if (separator === -1) {
    throw new Error(`invalid listen address: ${address}`);
  }

  const host = address.slice(0, separator);
  const port = Number(address.slice(separator + 1));

  if (!Number.isInteger(port) || port < 0 || port > 65535) {
    throw new Error(`invalid listen address: ${address}`);
  }

  return { host: host || undefined, port };
}

export function createRequestHandler(config) {
  return async (request, response) => {
    try {
      await handleRequest(config, request, response);
    } catch (error) {
      console.error('mixer-console: request failed:', error);
      if (!response.headersSent) {
        sendJson(response, 500, { status: 'error', error: 'internal server error' });
      } else {
        response.end();
      }
    }
  };
}

async function handleRequest(config, request, response) {
  const url = new URL(request.url ?? '/', 'http://mixer-console.local');

  if (url.pathname === '/health') {
    if (request.method !== 'GET') {
      methodNotAllowed(response, ['GET']);
      return;
    }
    sendJson(response, 200, { status: 'ok' });
    return;
  }

  if (url.pathname === '/token') {
    if (request.method !== 'GET') {
      methodNotAllowed(response, ['GET']);
      return;
    }

    const roomKey = url.searchParams.get('room') ?? 'broadcast';
    const resolveRoom = TOKEN_ROOMS.get(roomKey);
    if (!resolveRoom) {
      sendJson(response, 400, { status: 'error', error: 'unknown token room' });
      return;
    }
    const room = resolveRoom(config);

    const identity = `${config.identityPrefix}-${randomUUID()}`;
    const accessToken = new AccessToken(config.apiKey, config.apiSecret, {
      identity,
      name: 'Mixer Console',
      ttl: TOKEN_TTL,
    });
    accessToken.addGrant({
      roomJoin: true,
      room,
      canPublish: false,
      canSubscribe: true,
    });

    sendJson(response, 200, {
      url: config.livekitUrl,
      room,
      identity,
      token: await accessToken.toJwt(),
    });
    return;
  }

  if (url.pathname === '/api/subsystems') {
    if (request.method !== 'GET') {
      methodNotAllowed(response, ['GET']);
      return;
    }
    sendJson(response, 200, await readStatus(config));
    return;
  }

  const actionMatch = url.pathname.match(/^\/api\/subsystems\/([^/]+)\/(start|stop)$/);
  if (actionMatch) {
    if (request.method !== 'POST') {
      methodNotAllowed(response, ['POST']);
      return;
    }

    const subsystem = SUBSYSTEMS.find((entry) => entry.id === actionMatch[1]);
    if (!subsystem) {
      sendJson(response, 404, { status: 'error', error: 'unknown subsystem' });
      return;
    }

    const result = await setSubsystemPipeline(
      subsystem,
      config[subsystem.urlKey],
      actionMatch[2],
    );
    sendJson(response, result.statusCode, result.body);
    return;
  }

  if (request.method === 'GET') {
    const staticFile = STATIC_FILES.get(url.pathname);
    if (staticFile) {
      await serveStaticFile(response, staticFile);
      return;
    }
  }

  sendJson(response, 404, { status: 'error', error: 'not found' });
}

async function readStatus(config) {
  const subsystems = await Promise.all(
    SUBSYSTEMS.map((subsystem) => readSubsystemStatus(subsystem, config[subsystem.urlKey])),
  );

  return {
    status: subsystems.every((subsystem) => subsystem.reachable && subsystem.status === 'ok')
      ? 'ok'
      : 'degraded',
    subsystems,
  };
}

async function readSubsystemStatus(subsystem, baseURL) {
  let response;
  try {
    response = await fetch(`${baseURL}/health`, {
      signal: AbortSignal.timeout(SUBSYSTEM_TIMEOUT_MS),
    });
  } catch (error) {
    return {
      id: subsystem.id,
      title: subsystem.title,
      reachable: false,
      status: 'unreachable',
      state: null,
      parameters: [],
      error: errorMessage(error),
    };
  }

  const health = await parseJsonBody(response);
  if (health === null) {
    return {
      id: subsystem.id,
      title: subsystem.title,
      reachable: true,
      status: 'error',
      state: null,
      parameters: [],
      error: `health endpoint returned HTTP ${response.status} without a JSON body`,
    };
  }

  return {
    id: subsystem.id,
    title: subsystem.title,
    reachable: true,
    status: typeof health.status === 'string' ? health.status : 'unknown',
    state: subsystem.pipelineState(health),
    parameters: subsystem.parameters(health).filter((entry) => entry !== null),
  };
}

async function setSubsystemPipeline(subsystem, baseURL, action) {
  try {
    const response = await fetch(`${baseURL}/${action}`, {
      method: 'POST',
      signal: AbortSignal.timeout(SUBSYSTEM_TIMEOUT_MS),
    });
    const body = await parseJsonBody(response);

    const result = {
      id: subsystem.id,
      title: subsystem.title,
      ok: response.ok,
    };
    if (body !== null) {
      result.response = body;
    }
    if (!response.ok) {
      result.error = `subsystem returned HTTP ${response.status}`;
    }

    if (!response.ok) {
      return { statusCode: 502, body: { status: 'error', subsystem: result } };
    }
    return {
      statusCode: 200,
      body: {
        status: action === 'start' ? 'started' : 'stopped',
        subsystem: result,
      },
    };
  } catch (error) {
    return {
      statusCode: 502,
      body: {
        status: 'error',
        subsystem: {
          id: subsystem.id,
          title: subsystem.title,
          ok: false,
          error: errorMessage(error),
        },
      },
    };
  }
}

function parameter(name, value) {
  if (value === null || value === undefined) {
    return null;
  }
  return { name, value: String(value) };
}

function formatGrid(grid) {
  if (
    typeof grid !== 'object' ||
    grid === null ||
    !Number.isInteger(grid.columns) ||
    !Number.isInteger(grid.rows)
  ) {
    return null;
  }
  return `${grid.columns}x${grid.rows}`;
}

function formatSeconds(value) {
  if (value === null || value === undefined) {
    return null;
  }
  return `${value}s`;
}

async function parseJsonBody(response) {
  try {
    const body = await response.json();
    if (typeof body !== 'object' || body === null || Array.isArray(body)) {
      return null;
    }
    return body;
  } catch {
    return null;
  }
}

async function serveStaticFile(response, entry) {
  const body = await readFile(path.join(STATIC_DIR, entry.file));
  response.writeHead(200, {
    'Content-Type': entry.type,
    'Content-Length': body.length,
    'Cache-Control': 'no-store',
  });
  response.end(body);
}

function sendJson(response, statusCode, body) {
  const payload = JSON.stringify(body);
  response.writeHead(statusCode, {
    'Content-Type': 'application/json',
    'Content-Length': Buffer.byteLength(payload),
    'Cache-Control': 'no-store',
  });
  response.end(payload);
}

function methodNotAllowed(response, allowedMethods) {
  const payload = JSON.stringify({ status: 'error', error: 'method not allowed' });
  response.writeHead(405, {
    Allow: allowedMethods.join(', '),
    'Content-Type': 'application/json',
    'Content-Length': Buffer.byteLength(payload),
  });
  response.end(payload);
}

export async function verifyStaticAssets() {
  const files = new Set([...STATIC_FILES.values()].map((entry) => entry.file));
  for (const file of files) {
    await readFile(path.join(STATIC_DIR, file));
  }
}

export async function startMixerConsole(config) {
  const server = http.createServer(createRequestHandler(config));
  const { host, port } = parseListenAddr(config.listenAddr);

  await new Promise((resolve, reject) => {
    const onError = (error) => {
      reject(error);
    };
    const onListening = () => {
      server.off('error', onError);
      resolve();
    };

    server.once('error', onError);
    if (host) {
      server.listen(port, host, onListening);
    } else {
      server.listen(port, onListening);
    }
  });

  return server;
}

async function runHealthcheck(url) {
  try {
    const response = await fetch(url, { signal: AbortSignal.timeout(5000) });
    if (!response.ok) {
      console.error(`healthcheck GET ${url} status = ${response.status}`);
      return 1;
    }
    return 0;
  } catch (error) {
    console.error(`healthcheck GET ${url}: ${errorMessage(error)}`);
    return 1;
  }
}

async function main() {
  if (process.argv[2] === 'healthcheck') {
    const url = process.env.MIXER_CONSOLE_HEALTHCHECK_URL || DEFAULT_HEALTHCHECK_URL;
    process.exitCode = await runHealthcheck(url);
    return;
  }

  const config = configFromEnv();
  await verifyStaticAssets();
  const server = await startMixerConsole(config);

  console.log(`mixer-console: listening on ${config.listenAddr}`);
  console.log(`mixer-console: MediaMixer at ${config.mediaMixerURL}`);
  console.log(`mixer-console: MosaicComposer at ${config.mosaicComposerURL}`);
  console.log(`mixer-console: LiveKit room ${config.broadcastRoom} at ${config.livekitUrl}`);
  console.log(`mixer-console: LiveKit room ${config.guestsRoom} at ${config.livekitUrl}`);
  console.log(`mixer-console: LiveKit room ${config.studioRoom} at ${config.livekitUrl}`);

  const shutdown = (signal) => {
    console.log(`mixer-console: received ${signal}; shutting down`);
    server.close((error) => {
      if (error) {
        console.error(`mixer-console: shutdown failed: ${errorMessage(error)}`);
        process.exit(1);
      }
      process.exit(0);
    });
  };

  process.once('SIGINT', () => shutdown('SIGINT'));
  process.once('SIGTERM', () => shutdown('SIGTERM'));
}

function errorMessage(error) {
  return error instanceof Error ? error.message : String(error);
}

const entryPoint = process.argv[1]
  ? pathToFileURL(path.resolve(process.argv[1])).href
  : '';

if (entryPoint === import.meta.url) {
  main().catch((error) => {
    console.error(`mixer-console: ${errorMessage(error)}`);
    process.exit(1);
  });
}
