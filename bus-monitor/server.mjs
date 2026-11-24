import { readFile } from 'node:fs/promises';
import http from 'node:http';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const DEFAULT_LISTEN_ADDR = ':7990';
const DEFAULT_IDENTITY = 'bus-monitor';
const DEFAULT_HEALTHCHECK_URL = 'http://127.0.0.1:7990/health';
const STATIC_DIR = fileURLToPath(new URL('./dist/', import.meta.url));

const USAGE = `Usage: node server.mjs [--room ROOM]
       node server.mjs healthcheck

The LiveKit room to monitor can be set with --room or BUS_MONITOR_ROOM. It must
be one of the rooms configured in ControlPlane. The ControlPlane REST backend is
set with BUS_MONITOR_CONTROL_PLANE_URL.
Other settings are read from the environment.`;

const STATIC_FILES = new Map([
  ['/', { file: 'index.html', type: 'text/html; charset=utf-8' }],
  ['/index.html', { file: 'index.html', type: 'text/html; charset=utf-8' }],
  ['/app.js', { file: 'app.js', type: 'text/javascript; charset=utf-8' }],
  ['/styles.css', { file: 'styles.css', type: 'text/css; charset=utf-8' }],
]);

const REQUIRED_ENV = [
  ['controlPlaneURL', 'BUS_MONITOR_CONTROL_PLANE_URL'],
  ['room', 'BUS_MONITOR_ROOM (or --room)'],
];

export function parseArgs(argv) {
  const options = {};

  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];

    if (arg === '--help' || arg === '-h') {
      options.help = true;
      continue;
    }

    let room;
    if (arg === '--room' || arg === '-r') {
      if (index + 1 >= argv.length || argv[index + 1].startsWith('-')) {
        throw new Error(`${arg} requires a room name`);
      }
      index += 1;
      room = argv[index];
    } else if (arg.startsWith('--room=')) {
      room = arg.slice('--room='.length);
    } else if (arg.startsWith('-')) {
      throw new Error(`unknown option: ${arg}`);
    } else {
      room = arg;
    }

    if (!room) {
      throw new Error('room name must not be empty');
    }
    if (options.room !== undefined) {
      throw new Error('room name was provided more than once');
    }
    options.room = room;
  }

  return options;
}

export function configFromEnv(env = process.env, options = {}) {
  const config = {
    listenAddr: env.BUS_MONITOR_LISTEN_ADDR || DEFAULT_LISTEN_ADDR,
    controlPlaneURL: env.BUS_MONITOR_CONTROL_PLANE_URL || '',
    room: options.room || env.BUS_MONITOR_ROOM || '',
    identityPrefix: env.BUS_MONITOR_IDENTITY || DEFAULT_IDENTITY,
    healthcheckURL: env.BUS_MONITOR_HEALTHCHECK_URL || DEFAULT_HEALTHCHECK_URL,
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

export function tokenURL(config) {
  const url = new URL('/token', config.controlPlaneURL);
  url.searchParams.set('room', config.room);
  url.searchParams.set('identity_prefix', config.identityPrefix);
  return url.toString();
}

export function renderIndex(config, template) {
  const settings = JSON.stringify({ tokenURL: tokenURL(config) }).replaceAll('<', '\\u003c');
  const script = `<script>window.__BUS_MONITOR_CONFIG__ = ${settings};</script>`;
  if (!template.includes('</head>')) {
    throw new Error('index.html is missing a closing head tag');
  }
  return template.replace('</head>', `${script}\n  </head>`);
}

export function createRequestHandler(config) {
  return async (request, response) => {
    try {
      await handleRequest(config, request, response);
    } catch (error) {
      console.error('bus-monitor: request failed:', error);
      if (!response.headersSent) {
        sendJson(response, 500, { status: 'error', error: 'internal server error' });
      } else {
        response.end();
      }
    }
  };
}

async function handleRequest(config, request, response) {
  const url = new URL(request.url ?? '/', 'http://bus-monitor.local');

  if (url.pathname === '/health') {
    if (request.method !== 'GET') {
      methodNotAllowed(response, ['GET']);
      return;
    }
    sendJson(response, 200, { status: 'ok', room: config.room });
    return;
  }

  if (request.method === 'GET') {
    const staticFile = STATIC_FILES.get(url.pathname);
    if (staticFile) {
      await serveStaticFile(config, response, staticFile);
      return;
    }
  }

  sendJson(response, 404, { status: 'error', error: 'not found' });
}

async function serveStaticFile(config, response, entry) {
  let body = await readFile(path.join(STATIC_DIR, entry.file));
  if (entry.file === 'index.html') {
    body = Buffer.from(renderIndex(config, body.toString('utf8')));
  }

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

export async function startBusMonitor(config) {
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
  const argv = process.argv.slice(2);

  if (argv[0] === 'healthcheck') {
    const url = process.env.BUS_MONITOR_HEALTHCHECK_URL || DEFAULT_HEALTHCHECK_URL;
    process.exitCode = await runHealthcheck(url);
    return;
  }

  let options;
  try {
    options = parseArgs(argv);
  } catch (error) {
    console.error(`bus-monitor: ${errorMessage(error)}`);
    console.error(USAGE);
    process.exitCode = 2;
    return;
  }

  if (options.help) {
    console.log(USAGE);
    return;
  }

  const config = configFromEnv(process.env, options);
  await verifyStaticAssets();
  const server = await startBusMonitor(config);

  console.log(`bus-monitor: listening on ${config.listenAddr}`);
  console.log(`bus-monitor: LiveKit room ${config.room}`);
  console.log(`bus-monitor: ControlPlane at ${config.controlPlaneURL}`);

  const shutdown = (signal) => {
    console.log(`bus-monitor: received ${signal}; shutting down`);
    server.close((error) => {
      if (error) {
        console.error(`bus-monitor: shutdown failed: ${errorMessage(error)}`);
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
    console.error(`bus-monitor: ${errorMessage(error)}`);
    process.exit(1);
  });
}
