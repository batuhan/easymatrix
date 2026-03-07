import { createRuntime, EmbeddedRuntime, type EmbeddedRuntimeOptions } from "./runtime.js";

export type RuntimeInput = EmbeddedRuntime | EmbeddedRuntimeOptions | false;

export interface NormalizedRuntime {
  runtime?: EmbeddedRuntime;
  owned: boolean;
}

export function normalizeRuntime(runtime: RuntimeInput | undefined): NormalizedRuntime {
  if (runtime === false) {
    return { runtime: undefined, owned: false };
  }
  if (runtime instanceof EmbeddedRuntime) {
    return { runtime, owned: false };
  }
  return {
    runtime: createRuntime(runtime ?? {}),
    owned: true,
  };
}
