import { createRuntime, type EmbeddedRuntime, type EmbeddedRuntimeOptions } from "./runtime.js";

export interface HTTPServerOptions extends EmbeddedRuntimeOptions {
  hostname?: string;
  port?: number;
  runtime?: EmbeddedRuntime;
}

function responseHeaders(headers?: Record<string, string | readonly string[]>): Headers {
  const out = new Headers();
  if (!headers) {
    return out;
  }
  for (const [key, value] of Object.entries(headers)) {
    if (typeof value === "string") {
      out.set(key, value);
    } else {
      for (const item of value) {
        out.append(key, item);
      }
    }
  }
  return out;
}

async function readBody(req: Request): Promise<Uint8Array | undefined> {
  if (req.method === "GET" || req.method === "HEAD") {
    return undefined;
  }
  if (!req.body) {
    return undefined;
  }
  return new Uint8Array(await req.arrayBuffer());
}

export function serveHTTP(options: HTTPServerOptions = {}) {
  const runtime = options.runtime ?? createRuntime(options);

  return Bun.serve({
    hostname: options.hostname ?? "127.0.0.1",
    port: options.port ?? 23373,
    async fetch(req) {
      const response = await runtime.request({
        method: req.method,
        url: req.url,
        headers: Object.fromEntries(req.headers.entries()),
        body: await readBody(req),
      });
      const body = response.body instanceof Uint8Array ? Buffer.from(response.body) : response.body;
      return new Response(body ?? null, {
        status: response.status,
        statusText: response.statusText,
        headers: responseHeaders(response.headers),
      });
    },
  });
}
