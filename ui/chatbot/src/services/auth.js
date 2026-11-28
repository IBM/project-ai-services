import axios from 'axios';

// Sign-in with the catalog: authorization code with PKCE. The token lives in module memory only.
// It is never written to localStorage or sessionStorage, so a page reload signs in again.
let inMemoryToken = null;
let tokenExpiresAt = null;
let isRedirecting = false;

const SERVICE = 'chat';
export const CALLBACK_PATH = '/auth/callback';

const PKCE_VERIFIER_KEY = 'chat_pkce_verifier';
const OAUTH_STATE_KEY = 'chat_oauth_state';
const SAVED_ROUTE_KEY = 'chat_saved_route';
// Time of the last successful sign-in. Not a secret. customSendMessage uses it to stop a loop
// when the backend refuses a token the catalog has just issued (a 401 would otherwise restart
// the sign-in forever). It must not gate startLoginFlow: a reload drops the in-memory token and
// has to be able to sign in again at once.
const LAST_LOGIN_KEY = 'chat_last_login_ms';
const LOOP_GUARD_MS = 30 * 1000;

function base64UrlEncode(buffer) {
  const bytes = new Uint8Array(buffer);
  let binary = '';
  for (let i = 0; i < bytes.byteLength; i++) {
    binary += String.fromCharCode(bytes[i]);
  }
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

function generateRandomString(length = 43) {
  const array = new Uint8Array(length);
  crypto.getRandomValues(array);
  return base64UrlEncode(array.buffer).slice(0, length);
}

async function generateCodeChallenge(verifier) {
  const data = new TextEncoder().encode(verifier);
  const digest = await crypto.subtle.digest('SHA-256', data);
  return base64UrlEncode(digest);
}

export function getServiceToken() {
  return inMemoryToken;
}

export function setServiceToken(token, expiresInSeconds) {
  inMemoryToken = token;
  tokenExpiresAt = Date.now() + expiresInSeconds * 1000;
}

export function clearServiceToken() {
  inMemoryToken = null;
  tokenExpiresAt = null;
}

export function isTokenExpiringSoon() {
  if (!inMemoryToken || !tokenExpiresAt) return true;
  // Expiring within 60 seconds
  return Date.now() >= tokenExpiresAt - 60 * 1000;
}

// True when a sign-in finished moments ago. A 401 now is not an expired token, so signing in
// again would not help.
export function signedInJustNow() {
  const at = Number(sessionStorage.getItem(LAST_LOGIN_KEY));
  return at > 0 && Date.now() - at < LOOP_GUARD_MS;
}

export function getCatalogUiBaseUrl() {
  const envUrl = import.meta.env.VITE_CATALOG_UI_URL;
  if (envUrl) {
    return envUrl;
  }
  // Hosted on the standard host names, e.g. chat-bot-ui-<slug>.domain.com: infer catalog-ui.domain.com
  const currentOrigin = window.location.origin;
  if (currentOrigin.includes('chat-bot-ui')) {
    return currentOrigin.replace(/chat-bot-ui(-[^.:]+)?/, 'catalog-ui');
  }
  return currentOrigin;
}

// The catalog UI address set at deploy time (served by the UI's Nginx as /config.json). It is
// needed when this UI runs on another host than the catalog UI, where inferring it is wrong.
async function fetchConfiguredCatalogUiUrl() {
  try {
    const res = await fetch('/config.json', { cache: 'no-store' });
    if (!res.ok) return '';
    const cfg = await res.json();
    return typeof cfg.catalogUiUrl === 'string' ? cfg.catalogUiUrl : '';
  } catch {
    return '';
  }
}

export async function startLoginFlow() {
  if (isRedirecting) return;
  if (window.location.pathname === CALLBACK_PATH) return;

  isRedirecting = true;
  const verifier = generateRandomString(64);
  const challenge = await generateCodeChallenge(verifier);
  const state = generateRandomString(32);

  const currentRoute = window.location.pathname + window.location.search;
  sessionStorage.setItem(PKCE_VERIFIER_KEY, verifier);
  sessionStorage.setItem(OAUTH_STATE_KEY, state);
  sessionStorage.setItem(SAVED_ROUTE_KEY, currentRoute);

  const redirectUri = `${window.location.origin}${CALLBACK_PATH}`;
  const catalogUiUrl = (await fetchConfiguredCatalogUiUrl()) || getCatalogUiBaseUrl();
  const authorizeUrl = new URL('/authorize', catalogUiUrl);
  authorizeUrl.searchParams.set('service', SERVICE);
  authorizeUrl.searchParams.set('redirect_uri', redirectUri);
  authorizeUrl.searchParams.set('code_challenge', challenge);
  authorizeUrl.searchParams.set('state', state);

  window.location.href = authorizeUrl.toString();
}

// A code is single-use. React StrictMode (dev) runs the callback effect twice, and a second
// request with the same code would be rejected as "invalid grant", so share one in-flight
// exchange per code.
const inFlightExchanges = new Map();

export function exchangeAuthCode(code, state) {
  let pending = inFlightExchanges.get(code);
  if (!pending) {
    pending = doExchangeAuthCode(code, state);
    inFlightExchanges.set(code, pending);
  }
  return pending;
}

// Only a same-origin path is accepted as the place to return to ("//host" would be another origin).
function safeRoute(route) {
  return route && /^\/(?!\/)/.test(route) && route !== CALLBACK_PATH
    ? route
    : '/';
}

async function doExchangeAuthCode(code, state) {
  const savedState = sessionStorage.getItem(OAUTH_STATE_KEY);
  const verifier = sessionStorage.getItem(PKCE_VERIFIER_KEY);

  if (!savedState || savedState !== state) {
    throw new Error('Invalid OAuth state parameter');
  }

  if (!verifier) {
    throw new Error('PKCE code verifier not found');
  }

  const redirectUri = `${window.location.origin}${CALLBACK_PATH}`;

  const response = await axios.post(
    '/auth/exchange',
    {
      service: SERVICE,
      redirect_uri: redirectUri,
      code,
      code_verifier: verifier,
    },
    {
      headers: { 'Content-Type': 'application/json' },
    },
  );

  const { token, expires_at } = response.data;
  const expiresIn = Math.max(0, (Date.parse(expires_at) - Date.now()) / 1000);
  setServiceToken(token, expiresIn);

  sessionStorage.removeItem(OAUTH_STATE_KEY);
  sessionStorage.removeItem(PKCE_VERIFIER_KEY);
  sessionStorage.setItem(LAST_LOGIN_KEY, String(Date.now()));

  const savedRoute = safeRoute(sessionStorage.getItem(SAVED_ROUTE_KEY));
  sessionStorage.removeItem(SAVED_ROUTE_KEY);

  isRedirecting = false;
  return savedRoute;
}
