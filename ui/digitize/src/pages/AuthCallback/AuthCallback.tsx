import { useEffect, useState } from "react";
import { useLocation, useNavigate } from "react-router";
import { Loading, InlineNotification, Theme, Grid, Column } from "@carbon/react";
import { exchangeAuthCode } from "@/services/auth";
import styles from "./AuthCallback.module.scss";

export const AuthCallbackPage = () => {
  const location = useLocation();
  const navigate = useNavigate();

  const searchParams = new URLSearchParams(location.search);
  const code = searchParams.get("code");
  const state = searchParams.get("state");

  const missingParams = !code || !state;

  const [asyncError, setAsyncError] = useState<string | null>(null);
  const [loading, setLoading] = useState<boolean>(!missingParams);

  useEffect(() => {
    if (missingParams) {
      return;
    }

    let isMounted = true;

    const performExchange = async () => {
      try {
        const savedRoute = await exchangeAuthCode(code, state);
        if (!isMounted) return;
        navigate(savedRoute, { replace: true });
      } catch (err: unknown) {
        if (!isMounted) return;
        setLoading(false);
        const errorMsg =
          err && typeof err === "object" && "response" in err && (err as { response?: { data?: { error?: string } } }).response?.data?.error
            ? (err as { response?: { data?: { error?: string } } }).response?.data?.error
            : err instanceof Error
              ? err.message
              : "Authentication exchange failed";
        setAsyncError(errorMsg || "Authentication exchange failed");
      }
    };

    performExchange();

    return () => {
      isMounted = false;
    };
  }, [missingParams, code, state, navigate]);

  const error = missingParams
    ? "Missing required callback parameters (code, state)."
    : asyncError;

  return (
    <Theme theme="white">
      <Grid fullWidth className={styles.container}>
        <Column lg={8} md={4} sm={4} className={styles.content}>
          <h2>Authenticating with Catalog</h2>
          {loading && !error && (
            <div className={styles.loadingContainer}>
              <Loading description="Exchanging authorization code..." withOverlay={false} />
              <p>Completing login and redirecting to service...</p>
            </div>
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
        </Column>
      </Grid>
    </Theme>
  );
};

export default AuthCallbackPage;
