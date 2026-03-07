export type HeaderRecord = Record<string, string | readonly string[]>;

export function toArrayBuffer(input: ArrayBufferLike | Uint8Array): ArrayBuffer {
  const view = input instanceof Uint8Array ? input : new Uint8Array(input);
  const copy = new Uint8Array(view.byteLength);
  copy.set(view);
  return copy.buffer;
}

export function normalizeResponseBody(
  body: Uint8Array | ArrayBuffer | string | null | undefined,
): BodyInit | null {
  if (body == null) {
    return null;
  }
  if (typeof body === "string") {
    return body;
  }
  if (body instanceof Uint8Array) {
    return toArrayBuffer(body);
  }
  return toArrayBuffer(body);
}

export function responseHeaders(headers?: HeaderRecord): Headers {
  const out = new Headers();
  if (!headers) {
    return out;
  }
  for (const [key, value] of Object.entries(headers)) {
    if (typeof value === "string") {
      out.set(key, value);
      continue;
    }
    for (const item of value) {
      out.append(key, item);
    }
  }
  return out;
}

export function normalizeHeaderRecord(headers?: HeaderRecord): Record<string, string[]> | undefined {
  if (!headers) {
    return undefined;
  }
  const out: Record<string, string[]> = {};
  for (const [key, value] of Object.entries(headers)) {
    out[key] = typeof value === "string" ? [value] : [...value];
  }
  return out;
}

export async function readRequestBody(req: Request): Promise<Uint8Array | undefined> {
  if (req.method === "GET" || req.method === "HEAD" || req.body == null) {
    return undefined;
  }
  return new Uint8Array(await req.arrayBuffer());
}
