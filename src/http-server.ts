import { createRuntime, type EmbeddedRuntime, type EmbeddedRuntimeOptions } from "./runtime.js";
import { readRequestBody, responseHeaders } from "./transport-codec.js";

export interface HTTPServerOptions extends EmbeddedRuntimeOptions {
  hostname?: string;
  port?: number;
  runtime?: EmbeddedRuntime;
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
        body: await readRequestBody(req),
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
