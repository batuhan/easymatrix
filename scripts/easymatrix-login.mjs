#!/usr/bin/env node

import { setTimeout as delay } from "node:timers/promises";

const DEFAULT_BASE_URL = "http://127.0.0.1:23373";
const DEFAULT_HOMESERVER_URL = "https://matrix.beeper.com";
const DEFAULT_DOMAIN = "beeper.com";
const DEFAULT_TIMEOUT_MS = 20_000;
const DEFAULT_WAIT_MS = 60_000;

function parseArgs(argv) {
  const args = {};

  for (let index = 0; index < argv.length; index += 1) {
    const token = argv[index];
    if (!token.startsWith("--")) {
      throw new Error(`Unexpected argument: ${token}`);
    }

    const [key, inlineValue] = token.slice(2).split("=", 2);
    if (!key) {
      throw new Error("Empty flag name");
    }
    if (inlineValue !== undefined) {
      args[key] = inlineValue;
      continue;
    }

    const next = argv[index + 1];
    if (!next || next.startsWith("--")) {
      args[key] = true;
      continue;
    }

    args[key] = next;
    index += 1;
  }

  return {
    help: args.help === true,
    baseURL: normalizeBaseURL(readOptionalString(args["base-url"]) ?? DEFAULT_BASE_URL),
    homeserverURL: readOptionalString(args["homeserver-url"]),
    domain: normalizeDomain(readOptionalString(args.domain) ?? DEFAULT_DOMAIN),
    loginToken: readOptionalString(args["login-token"]),
    username: readOptionalString(args.username),
    password: readOptionalString(args.password),
    email: readOptionalString(args.email),
    code: readOptionalString(args.code),
    recoveryKey: readOptionalString(args["recovery-key"]),
    deviceID: readOptionalString(args["device-id"]),
    deviceName: readOptionalString(args["device-name"]),
    timeoutMs: parsePositiveInteger(args["timeout-ms"], DEFAULT_TIMEOUT_MS, "timeout-ms"),
    waitMs: parsePositiveInteger(args["wait-ms"], DEFAULT_WAIT_MS, "wait-ms"),
  };
}

function readOptionalString(value) {
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function parsePositiveInteger(rawValue, fallback, label) {
  if (rawValue === undefined || rawValue === true) {
    return fallback;
  }
  const parsed = Number(rawValue);
  if (!Number.isInteger(parsed) || parsed <= 0) {
    throw new Error(`--${label} must be a positive integer`);
  }
  return parsed;
}

function normalizeBaseURL(value) {
  return value.replace(/\/+$/, "");
}

function normalizeDomain(value) {
  return value.replace(/^https?:\/\//i, "").replace(/^matrix\./i, "").replace(/^api\./i, "").replace(/\/+$/, "");
}

function resolveHomeserverURL(options) {
  if (options.homeserverURL) {
    return options.homeserverURL;
  }
  if (options.domain) {
    return `https://matrix.${options.domain}`;
  }
  return DEFAULT_HOMESERVER_URL;
}

async function api(baseURL, path, body, timeoutMs) {
  const response = await fetch(`${baseURL}${path}`, {
    method: body ? "POST" : "GET",
    headers: body ? { "content-type": "application/json" } : undefined,
    body: body ? JSON.stringify(body) : undefined,
    signal: AbortSignal.timeout(timeoutMs),
  });

  const text = await response.text();
  let payload = null;
  try {
    payload = text ? JSON.parse(text) : null;
  } catch {
    payload = text ? { raw: text } : null;
  }

  if (!response.ok) {
    const message =
      payload?.message ??
      payload?.error ??
      payload?.raw ??
      `HTTP ${response.status}`;
    throw new Error(message);
  }

  return payload;
}

async function waitForState(baseURL, timeoutMs, predicate) {
  const startedAt = Date.now();
  while (Date.now() - startedAt < timeoutMs) {
    const state = await api(baseURL, "/manage/state", null, DEFAULT_TIMEOUT_MS);
    if (predicate(state)) {
      return state;
    }
    await delay(500);
  }
  throw new Error(`Timed out waiting for /manage/state after ${timeoutMs}ms`);
}

function printHelp() {
  process.stdout.write("Drive EasyMatrix /manage login flows from the terminal.\n\n");
  process.stdout.write("Usage:\n");
  process.stdout.write("  node scripts/easymatrix-login.mjs --login-token TOKEN --recovery-key KEY\n");
  process.stdout.write("  node scripts/easymatrix-login.mjs --username USER --password PASS --recovery-key KEY\n");
  process.stdout.write("  node scripts/easymatrix-login.mjs --domain beeper-staging.com --email you@example.com --code 959729 --recovery-key KEY\n\n");
  process.stdout.write("Flags:\n");
  process.stdout.write(`  --base-url         EasyMatrix base URL (default ${DEFAULT_BASE_URL})\n`);
  process.stdout.write("  --homeserver-url   Matrix homeserver URL (defaults from --domain)\n");
  process.stdout.write(`  --domain           Beeper domain for email-code login (default ${DEFAULT_DOMAIN})\n`);
  process.stdout.write("  --login-token      Matrix JWT login token\n");
  process.stdout.write("  --username         Matrix username / user ID for password login\n");
  process.stdout.write("  --password         Matrix password for password login\n");
  process.stdout.write("  --email            Beeper email for request-code flow\n");
  process.stdout.write("  --code             Beeper login code for request-code flow\n");
  process.stdout.write("  --recovery-key     Recovery key or passphrase to verify after login\n");
  process.stdout.write("  --device-id        Optional device ID for JWT login\n");
  process.stdout.write("  --device-name      Optional device display name for JWT login\n");
  process.stdout.write(`  --timeout-ms       Request timeout in milliseconds (default ${DEFAULT_TIMEOUT_MS})\n`);
  process.stdout.write(`  --wait-ms          State wait timeout in milliseconds (default ${DEFAULT_WAIT_MS})\n`);
}

async function loginWithEmailCode(options) {
  const start = await api(options.baseURL, "/manage/beeper/start-login", { domain: options.domain }, options.timeoutMs);
  const request = String(start?.request ?? "").trim();
  if (!request) {
    throw new Error("Beeper start-login did not return a request ID");
  }

  await api(
    options.baseURL,
    "/manage/beeper/request-code",
    { domain: options.domain, request, email: options.email },
    options.timeoutMs,
  );

  const sanitizedCode = String(options.code ?? "").replace(/[^0-9]/g, "").slice(0, 6);
  if (sanitizedCode.length !== 6) {
    throw new Error("--code must contain a 6-digit login code");
  }

  const submit = await api(
    options.baseURL,
    "/manage/beeper/submit-code",
    { domain: options.domain, request, response: sanitizedCode },
    options.timeoutMs,
  );
  const loginToken = String(submit?.token ?? "").trim();
  if (!loginToken) {
    throw new Error("Beeper submit-code did not return a login token");
  }

  return loginToken;
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  if (options.help) {
    printHelp();
    return;
  }

  const hasTokenLogin = Boolean(options.loginToken);
  const hasPasswordLogin = Boolean(options.username || options.password);
  const hasEmailCodeLogin = Boolean(options.email || options.code);

  const modeCount = [hasTokenLogin, hasPasswordLogin, hasEmailCodeLogin].filter(Boolean).length;
  if (modeCount !== 1) {
    throw new Error("Choose exactly one login mode: --login-token, --username/--password, or --email/--code");
  }
  if (hasPasswordLogin && (!options.username || !options.password)) {
    throw new Error("--username and --password must be provided together");
  }
  if (hasEmailCodeLogin && (!options.email || !options.code)) {
    throw new Error("--email and --code must be provided together");
  }

  const homeserverURL = resolveHomeserverURL(options);

  if (hasTokenLogin) {
    await api(
      options.baseURL,
      "/manage/login-token",
      {
        homeserverURL,
        loginToken: options.loginToken,
        deviceID: options.deviceID,
        deviceName: options.deviceName,
      },
      options.timeoutMs,
    );
  } else if (hasPasswordLogin) {
    await api(
      options.baseURL,
      "/manage/login-password",
      {
        homeserverURL,
        username: options.username,
        password: options.password,
      },
      options.timeoutMs,
    );
  } else {
    const loginToken = await loginWithEmailCode(options);
    await api(
      options.baseURL,
      "/manage/login-token",
      {
        homeserverURL,
        loginToken,
        deviceID: options.deviceID,
        deviceName: options.deviceName,
      },
      options.timeoutMs,
    );
  }

  if (options.recoveryKey) {
    await api(
      options.baseURL,
      "/manage/verify",
      { recoveryKey: options.recoveryKey },
      options.timeoutMs,
    );
  }

  const state = await waitForState(options.baseURL, options.waitMs, (value) => {
    const clientState = value?.client_state ?? {};
    return Boolean(clientState.is_logged_in) && (!options.recoveryKey || Boolean(clientState.is_verified));
  });
  process.stdout.write(`${JSON.stringify(state, null, 2)}\n`);
}

main().catch((error) => {
  const message = error instanceof Error ? error.stack ?? error.message : String(error);
  process.stderr.write(`${message}\n`);
  process.exit(1);
});
