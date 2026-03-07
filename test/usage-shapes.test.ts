import { BeeperDesktop } from "@beeper/desktop-api";

import { EMBEDDED_RUNTIME_INFO } from "../src/bridge.js";
import { createEmbeddedFetch, createRuntimeHandle, withEmbedded } from "../src/client.js";
import { createEmbeddedRealtime } from "../src/realtime.js";

async function usage() {
  const embedded = await createEmbeddedFetch({
    runtime: false,
  });

  const sdk = new BeeperDesktop({
    baseURL: embedded.baseURL,
    accessToken: "token",
    fetch: embedded.fetch,
  });
  void sdk;

  const wrappedCtor = await withEmbedded(BeeperDesktop, {
    runtime: false,
    sdkOptions: { accessToken: "token" },
  });
  void wrappedCtor.sdk;

  const existing = new BeeperDesktop({ accessToken: "token" });
  const wrappedInstance = await withEmbedded(existing, {
    runtime: false,
  });
  void wrappedInstance.sdk;

  const handle = await createRuntimeHandle({
    runtime: false,
  });
  void handle.invoke({ type: EMBEDDED_RUNTIME_INFO });
  void handle.createRealtime();

  void createEmbeddedRealtime({ runtime: false });
}

void usage;
