import { useEffect, useState } from 'react';
import { InlineNotification, Loading } from '@carbon/react';
import { exchangeAuthCode } from '../services/auth.js';

/**
 * Landing page of the catalog sign-in. Trades the one-time code for a token, then hands back to
 * the chat. There is no page reload: the token is held in memory only, so a reload would lose it.
 */
export function AuthCallback({ onDone }) {
  const params = new URLSearchParams(window.location.search);
  const code = params.get('code');
  const state = params.get('state');
  const missingParams = !code || !state;

  const [asyncError, setAsyncError] = useState(null);

  useEffect(() => {
    if (missingParams) {
      return;
    }

    let isMounted = true;

    const performExchange = async () => {
      try {
        const savedRoute = await exchangeAuthCode(code, state);
        if (!isMounted) return;
        window.history.replaceState(null, '', savedRoute);
        onDone();
      } catch (err) {
        if (!isMounted) return;
        setAsyncError(
          err?.response?.data?.error ||
            err?.message ||
            'Authentication exchange failed',
        );
      }
    };

    performExchange();

    return () => {
      isMounted = false;
    };
  }, [missingParams, code, state, onDone]);

  const error = missingParams
    ? 'Missing required callback parameters (code, state).'
    : asyncError;

  return (
    <div style={{ padding: '2rem', maxWidth: '40rem' }}>
      <h2>Signing in with Catalog</h2>
      {!error && (
        <Loading description="Exchanging authorization code..." withOverlay={false} />
      )}
      {error && (
        <InlineNotification
          kind="error"
          role="alert"
          title="Authentication Error"
          subtitle={error}
          lowContrast
          hideCloseButton
        />
      )}
    </div>
  );
}

export default AuthCallback;
