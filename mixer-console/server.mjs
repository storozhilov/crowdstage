import { readFile } from 'node:fs/promises';
import http from 'node:http';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const DEFAULT_LISTEN_ADDR = ':7991';
const DEFAULT_CONTROL_PLANE_URL = 'http://127.0.0.1:7982';
const DEFAULT_HEALTHCHECK_URL = 'http://127.0.0.1:7991/health';
const STATIC_DIR = fileURLToPath(new URL('./dist/', import.meta.url));

const STATIC_FILES = new Map([
  ['/', { file: 'index.html', type: 'text/html; charset=utf-8' }],
  ['/index.html', { file: 'index.html', type: 'text/html; charset=utf-8' }],
  ['/app.js', { file: 'app.js', type: 'text/javascript; charset=utf-8' }],
  ['/styles.css', { file: 'styles.css', type: 'text/css; charset=utf-8' }],
]);

export function configFromEnv(env = process.env) {
  return {
    listenAddr: env.MIXER_CONSOLE_LISTEN_ADDR || DEFAULT_LISTEN_ADDR,
    controlPlaneURL: env.MIXER_CONSOLE_CONTROL_PLANE_URL || DEFAULT_CONTROL_PLANE_URL,
    healthcheckURL: env.MIXER_CONSOLE_HEALTHCHECK_URL || DEFAULT_HEALTHCHECK_URL,
  };
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

export function renderIndex(config, template) {
  const settings = JSON.stringify({ controlPlaneURL: config.controlPlaneURL }).replaceAll('<', '\\u003c');
  const script = `<script>window.__MIXER_CONSOLE_CONFIG__ = ${settings};</script>`;
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
  console.log(`mixer-console: ControlPlane at ${config.controlPlaneURL}`);

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
