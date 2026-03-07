export { BeeperDesktop } from "@beeper/desktop-api";
export type { ClientOptions } from "@beeper/desktop-api";

export {
  EMBEDDED_HTTP_REQUEST,
  EMBEDDED_HTTP_RESPONSE,
  EMBEDDED_RUNTIME_INFO,
  type EmbeddedCommand,
  type EmbeddedCommandInvoker,
  type EmbeddedCommandResult,
  type EmbeddedHTTPRequestCommand,
  type EmbeddedHTTPResponseResult,
  type EmbeddedRuntimeInfo,
  type EmbeddedRuntimeInfoCommand,
  type EmbeddedRuntimeInfoResult,
  type EmbeddedRealtimeCommand,
  type EmbeddedRealtimeEvent,
  type EmbeddedRealtimeEventType,
  type EmbeddedSubscriptionsSetCommand,
  type EmbeddedReadyEvent,
  type EmbeddedSubscriptionsUpdatedEvent,
  type EmbeddedRealtimeErrorEvent,
  type EmbeddedDomainEvent,
} from "./bridge.js";

export {
  createFetchFromNativeRequest,
  createFetchFromNativeCommand,
  createFetchFromNapiModule,
  createFetchFromNapiCommandModule,
  type FetchLike,
  type NapiNativeCommandModule,
  type NapiNativeHTTPModule,
  type NativeRequest,
  type NativeRequestBody,
  type NativeRequestFn,
  type NativeResponse,
  type NativeResponseBody,
} from "./native-fetch.js";

export {
  createRuntime,
  EmbeddedRuntime,
  type EmbeddedRuntimeOptions,
  type EmbeddedRuntimeStatus,
  type EmbeddedRealtimeConnection,
} from "./runtime.js";

export {
  createEmbeddedFetch,
  createRuntimeHandle,
  createEmbeddedFetch as desktopAPIFetch,
  withEmbedded,
  type RuntimeInput,
  type CreateEmbeddedFetchOptions,
  type EmbeddedFetchHandle,
  type EmbeddedRuntimeHandle,
  type WithEmbeddedOptions,
  type EmbeddedSDKHandle,
} from "./client.js";

export {
  createEmbeddedRealtime,
  type CreateEmbeddedRealtimeOptions,
  type EmbeddedRealtimeAdapter,
  type WaitForEmbeddedEventOptions,
} from "./realtime.js";

export { run, type RunOptions } from "./run.js";

export { serveHTTP, type HTTPServerOptions } from "./http-server.js";

export type {
  CompatibleRouteTypes,
  AccountsListResponse,
  ChatsListResponse,
  ChatsSearchResponse,
  MessagesListResponse,
  MessagesSearchResponse,
  MessageSendResponse,
  AssetsDownloadResponse,
  FocusResponse,
  SearchResponse,
} from "./type-contract.js";
