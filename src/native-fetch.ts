import {
  EMBEDDED_HTTP_REQUEST,
  isEmbeddedHTTPResponseResult,
  type EmbeddedCommandInvoker,
} from "./bridge.js";
import { normalizeResponseBody, readRequestBody, responseHeaders } from "./transport-codec.js";

export type NativeRequestBody = Uint8Array | ArrayBuffer | string | null | undefined;

export type NativeResponseBody = Uint8Array | ArrayBuffer | string | null | undefined;

export interface NativeRequest {
  method: string;
  url: string;
  headers: Record<string, string>;
  body?: NativeRequestBody;
}

export interface NativeResponse {
  status: number;
  statusText?: string;
  headers?: Record<string, string | readonly string[]>;
  body?: NativeResponseBody;
}

export type NativeRequestFn = (request: NativeRequest) => NativeResponse | Promise<NativeResponse>;
export type FetchLike = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

export interface NapiNativeHTTPModule {
  request: NativeRequestFn;
}

export interface NapiNativeCommandModule {
  invoke: EmbeddedCommandInvoker;
}

function requestHeaders(headers: Headers): Record<string, string> {
  const out: Record<string, string> = {};
  headers.forEach((value, key) => {
    out[key] = value;
  });
  return out;
}

function abortError(): Error {
  return new DOMException("The operation was aborted", "AbortError");
}

export function createFetchFromNativeRequest(nativeRequest: NativeRequestFn): FetchLike {
  return async function fetchFromNative(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
    const req = new Request(input, init);

    if (req.signal.aborted) {
      throw abortError();
    }

    const payload: NativeRequest = {
      method: req.method,
      url: req.url,
      headers: requestHeaders(req.headers),
      body: await readRequestBody(req),
    };

    const nativeCall = Promise.resolve(nativeRequest(payload));

    const response = req.signal
      ? await Promise.race([
          nativeCall,
          new Promise<never>((_, reject) => {
            req.signal.addEventListener("abort", () => reject(abortError()), { once: true });
          }),
        ])
      : await nativeCall;

    return new Response(normalizeResponseBody(response.body), {
      status: response.status,
      statusText: response.statusText,
      headers: responseHeaders(response.headers),
    });
  };
}

export function createFetchFromNapiModule(module: NapiNativeHTTPModule): FetchLike {
  return createFetchFromNativeRequest(module.request);
}

export function createFetchFromNativeCommand(nativeInvoke: EmbeddedCommandInvoker): FetchLike {
  return createFetchFromNativeRequest(async (request) => {
    const result = await nativeInvoke({
      type: EMBEDDED_HTTP_REQUEST,
      request,
    });
    if (!isEmbeddedHTTPResponseResult(result)) {
      throw new Error(`Embedded command returned ${result.type} for ${EMBEDDED_HTTP_REQUEST}.`);
    }
    return result.response;
  });
}

export function createFetchFromNapiCommandModule(module: NapiNativeCommandModule): FetchLike {
  return createFetchFromNativeCommand(module.invoke);
}
