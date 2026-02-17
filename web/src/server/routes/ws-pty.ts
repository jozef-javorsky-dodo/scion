/**
 * Copyright 2026 Google LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

/**
 * WebSocket proxy for PTY connections
 *
 * Handles WebSocket upgrade requests for /api/agents/{agentId}/pty,
 * extracts auth from session cookies or dev token, and proxies
 * bidirectionally to the Hub PTY WebSocket endpoint.
 */

import type { Server as HttpServer, IncomingMessage } from 'http';
import type { Duplex } from 'stream';
import { createHmac } from 'crypto';
import { WebSocketServer, WebSocket } from 'ws';

import type { AppConfig } from '../config.js';
import { resolveDevToken } from '../middleware/dev-auth.js';

/** Pattern matching /api/agents/{agentId}/pty */
const PTY_PATH_PATTERN = /^\/api\/agents\/([^/]+)\/pty$/;

/**
 * Parses cookies from a raw Cookie header string.
 */
function parseCookies(cookieHeader: string): Map<string, string> {
  const cookies = new Map<string, string>();
  for (const pair of cookieHeader.split(';')) {
    const idx = pair.indexOf('=');
    if (idx === -1) continue;
    const key = pair.substring(0, idx).trim();
    const value = pair.substring(idx + 1).trim();
    cookies.set(key, value);
  }
  return cookies;
}

/**
 * Verifies a signed cookie value against its signature using the session secret.
 * koa-session uses Keygrip which produces base64url HMAC-SHA1 signatures.
 */
function verifySignedCookie(value: string, signature: string, secret: string): boolean {
  const expected = createHmac('sha1', secret)
    .update(value)
    .digest('base64')
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '');
  return expected === signature;
}

/**
 * Extracts the Hub access token from the session cookie on a raw HTTP request.
 *
 * The session cookie (scion_sess) contains base64-encoded JSON with session data.
 * The signature cookie (scion_sess.sig) is verified against the session secret.
 */
function extractTokenFromSession(req: IncomingMessage, config: AppConfig): string | null {
  const cookieHeader = req.headers.cookie;
  if (!cookieHeader) return null;

  const cookies = parseCookies(cookieHeader);
  const sessionCookie = cookies.get('scion_sess');
  const signatureCookie = cookies.get('scion_sess.sig');

  if (!sessionCookie || !signatureCookie) return null;

  // Verify signature
  if (!verifySignedCookie(sessionCookie, signatureCookie, config.session.secret)) {
    console.warn('[WS-PTY] Session cookie signature verification failed');
    return null;
  }

  // Decode session data
  try {
    const decoded = Buffer.from(sessionCookie, 'base64').toString('utf-8');
    const session = JSON.parse(decoded) as { hubAccessToken?: string };
    return session.hubAccessToken || null;
  } catch {
    console.warn('[WS-PTY] Failed to decode session cookie');
    return null;
  }
}

/**
 * Resolves an auth token for the Hub connection.
 *
 * Priority order (mirrors api.ts):
 * 1. Dev token (development mode)
 * 2. Hub access token from session cookie
 */
function resolveAuthToken(req: IncomingMessage, config: AppConfig): string | null {
  // Priority 1: Dev token
  const devToken = resolveDevToken();
  if (devToken) return devToken;

  // Priority 2: Session token
  return extractTokenFromSession(req, config);
}

/**
 * Builds the upstream Hub WebSocket URL for PTY.
 */
function buildHubUrl(config: AppConfig, agentId: string, query: string): string {
  const hubBase = config.hubApiUrl.replace(/^http/, 'ws');
  const url = `${hubBase}/api/v1/agents/${agentId}/pty`;
  return query ? `${url}?${query}` : url;
}

/**
 * Sets up the WebSocket proxy on the HTTP server for PTY connections.
 *
 * This operates at the HTTP server level (not Koa middleware) because
 * WebSocket upgrade happens before Koa's middleware chain runs.
 */
export function setupWebSocketProxy(server: HttpServer, config: AppConfig): void {
  const wss = new WebSocketServer({ noServer: true });

  server.on('upgrade', (req: IncomingMessage, socket: Duplex, head: Buffer) => {
    const url = req.url || '';
    const [pathname, querystring] = url.split('?', 2);

    // Only handle PTY paths
    const match = pathname.match(PTY_PATH_PATTERN);
    if (!match) return; // Let other upgrade handlers (if any) handle it

    const agentId = match[1];

    // Resolve auth token
    const token = resolveAuthToken(req, config);
    if (!token) {
      console.warn('[WS-PTY] No auth token available, rejecting connection');
      socket.write('HTTP/1.1 401 Unauthorized\r\n\r\n');
      socket.destroy();
      return;
    }

    // Build upstream Hub URL
    const hubUrl = buildHubUrl(config, agentId, querystring || '');

    // Connect to Hub upstream
    const upstream = new WebSocket(hubUrl, {
      headers: {
        Authorization: `Bearer ${token}`,
      },
    });

    upstream.on('error', (err) => {
      console.error('[WS-PTY] Upstream connection error:', err.message);
      socket.write('HTTP/1.1 502 Bad Gateway\r\n\r\n');
      socket.destroy();
    });

    upstream.on('open', () => {
      // Upstream connected, now accept the browser WebSocket
      wss.handleUpgrade(req, socket, head, (clientWs) => {
        wss.emit('connection', clientWs, req);

        // Pipe: client -> upstream (preserve text/binary frame type)
        clientWs.on('message', (data: Buffer, isBinary: boolean) => {
          if (upstream.readyState === WebSocket.OPEN) {
            upstream.send(data, { binary: isBinary });
          }
        });

        // Pipe: upstream -> client (preserve text/binary frame type)
        upstream.on('message', (data: Buffer, isBinary: boolean) => {
          if (clientWs.readyState === WebSocket.OPEN) {
            clientWs.send(data, { binary: isBinary });
          }
        });

        // Handle client close -> close upstream
        clientWs.on('close', (code: number, reason: Buffer) => {
          if (upstream.readyState === WebSocket.OPEN) {
            upstream.close(code, reason.toString());
          }
        });

        // Handle upstream close -> close client
        upstream.on('close', (code: number, reason: Buffer) => {
          if (clientWs.readyState === WebSocket.OPEN) {
            clientWs.close(code, reason.toString());
          }
        });

        // Handle errors after connection established
        clientWs.on('error', (err) => {
          console.error('[WS-PTY] Client WebSocket error:', err.message);
          if (upstream.readyState === WebSocket.OPEN) {
            upstream.close(1011, 'Client error');
          }
        });

        upstream.on('error', (err) => {
          console.error('[WS-PTY] Upstream WebSocket error:', err.message);
          if (clientWs.readyState === WebSocket.OPEN) {
            clientWs.close(1011, 'Upstream error');
          }
        });

        console.info(`[WS-PTY] Session established for agent ${agentId}`);
      });
    });
  });
}
