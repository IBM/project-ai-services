import axios from "axios";

// In-memory token storage (never written to localStorage or sessionStorage)
let inMemoryToken: string | null = null;
let tokenExpiresAt: number | null = null;
let isRedirecting = false;

const PKCE_VERIFIER_KEY = "digitize_pkce_verifier";
const OAUTH_STATE_KEY = "digitize_oauth_state";
const SAVED_ROUTE_KEY = "digitize_saved_route";

function base64UrlEncode(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = "";
  for (let i = 0; i < bytes.byteLength; i++) {
    binary += String.fromCharCode(bytes[i]);
  }
  return btoa(binary)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}

function generateRandomString(length = 43): string {
  const array = new Uint8Array(length);
  crypto.getRandomValues(array);
  return base64UrlEncode(array.buffer).slice(0, length);
}

async function generateCodeChallenge(verifier: string): Promise<string> {
  const encoder = new TextEncoder();
  const data = encoder.encode(verifier);
  const digest = await crypto.subtle.digest("SHA-256", data);
  return base64UrlEncode(digest);
}

export function getServiceToken(): string | null {
  return inMemoryToken;
}

export function setServiceToken(token: string, expiresInSeconds: number): void {
  inMemoryToken = token;
  tokenExpiresAt = Date.now() + expiresInSeconds * 1000;
}

export function clearServiceToken(): void {
  inMemoryToken = null;
  tokenExpiresAt = null;
}

export function isTokenExpiringSoon(): boolean {
  if (!inMemoryToken || !tokenExpiresAt) return true;
  // Expiring within 60 seconds
  return Date.now() >= tokenExpiresAt - 60 * 1000;
}

export function getCatalogUiBaseUrl(): string {
  const envUrl = import.meta.env.VITE_CATALOG_UI_URL;
  if (envUrl) {
    return envUrl;
  }
  // If hosted on standard subdomains e.g. digitize-ui-<slug>.domain.com, infer catalog-ui.domain.com
  const currentOrigin = window.location.origin;
  if (currentOrigin.includes("digitize-ui-")) {
    return currentOrigin.replace(/digitize-ui-[^.:]+/, "catalog-ui");
  }
  return currentOrigin;
}

export async function startLoginFlow(): Promise<void> {
  if (isRedirecting) return;
  if (window.location.pathname === "/auth/callback") return;

  isRedirecting = true;
  const verifier = generateRandomString(64);
  const challenge = await generateCodeChallenge(verifier);
  const state = generateRandomString(32);

  const currentRoute = window.location.pathname + window.location.search;
  sessionStorage.setItem(PKCE_VERIFIER_KEY, verifier);
  sessionStorage.setItem(OAUTH_STATE_KEY, state);
  sessionStorage.setItem(SAVED_ROUTE_KEY, currentRoute);

  const redirectUri = `${window.location.origin}/auth/callback`;
  const catalogBase = getCatalogUiBaseUrl();
  const authorizeUrl = new URL("/authorize", catalogBase);
  authorizeUrl.searchParams.set("service", "digitize");
  authorizeUrl.searchParams.set("redirect_uri", redirectUri);
  authorizeUrl.searchParams.set("code_challenge", challenge);
  authorizeUrl.searchParams.set("state", state);

  window.location.href = authorizeUrl.toString();
}

// A code is single-use. React StrictMode (dev) runs the callback effect twice, and a second
// request with the same code would be rejected as "invalid grant", so share one in-flight
// exchange per code.
const inFlightExchanges = new Map<string, Promise<string>>();

export function exchangeAuthCode(code: string, state: string): Promise<string> {
  let pending = inFlightExchanges.get(code);
  if (!pending) {
    pending = doExchangeAuthCode(code, state);
    inFlightExchanges.set(code, pending);
  }
  return pending;
}

async function doExchangeAuthCode(code: string, state: string): Promise<string> {
  const savedState = sessionStorage.getItem(OAUTH_STATE_KEY);
  const verifier = sessionStorage.getItem(PKCE_VERIFIER_KEY);

  if (!savedState || savedState !== state) {
    throw new Error("Invalid OAuth state parameter");
  }

  if (!verifier) {
    throw new Error("PKCE code verifier not found");
  }

  const redirectUri = `${window.location.origin}/auth/callback`;

  const response = await axios.post<{ token: string; expires_at: string }>(
    "/auth/exchange",
    {
      service: "digitize",
      redirect_uri: redirectUri,
      code,
      code_verifier: verifier,
    },
    {
      headers: { "Content-Type": "application/json" },
    },
  );

  const { token, expires_at } = response.data;
  const expiresIn = Math.max(0, (Date.parse(expires_at) - Date.now()) / 1000);
  setServiceToken(token, expiresIn);

  sessionStorage.removeItem(OAUTH_STATE_KEY);
  sessionStorage.removeItem(PKCE_VERIFIER_KEY);

  const savedRoute = sessionStorage.getItem(SAVED_ROUTE_KEY) || "/jobs";
  sessionStorage.removeItem(SAVED_ROUTE_KEY);

  isRedirecting = false;
  return savedRoute;
}
