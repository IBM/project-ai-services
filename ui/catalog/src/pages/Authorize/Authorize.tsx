import { useEffect, useState } from "react";
import { useLocation, useNavigate } from "react-router";
import { Loading, InlineNotification, Theme, Grid, Column } from "@carbon/react";
import { authorize } from "@/api/auth";
import { useAuthStore } from "@/store/auth.store";
import { ROUTES } from "@/constants";
import styles from "./Authorize.module.scss";

export const AuthorizePage = () => {
  const location = useLocation();
  const navigate = useNavigate();
  const accessToken = useAuthStore((state) => state.accessToken);

  const searchParams = new URLSearchParams(location.search);
  const service = searchParams.get("service");
  const redirectUri = searchParams.get("redirect_uri");
  const codeChallenge = searchParams.get("code_challenge");
  const state = searchParams.get("state");

  const missingParams = !service || !redirectUri || !codeChallenge || !state;

  const [asyncError, setAsyncError] = useState<string | null>(null);
  const [loading, setLoading] = useState<boolean>(!missingParams);

  useEffect(() => {
    if (missingParams) {
      return;
    }

    if (!accessToken) {
      // User is not authenticated in catalog: redirect to login preserving return_to
      const returnTo = encodeURIComponent(location.pathname + location.search);
      navigate(`${ROUTES.LOGIN}?return_to=${returnTo}`, { replace: true });
      return;
    }

    let isMounted = true;

    const performAuthorization = async () => {
      try {
        const response = await authorize({
          service,
          redirect_uri: redirectUri,
          code_challenge: codeChallenge,
          state,
        });

        if (!isMounted) return;

        // The server builds this URL from the registered callback, carrying the code and state.
        window.location.href = response.redirect_to;
      } catch (err: unknown) {
        if (!isMounted) return;
        setLoading(false);
        const errorMsg =
          err && typeof err === "object" && "response" in err && (err as { response?: { data?: { error?: string } } }).response?.data?.error
            ? (err as { response?: { data?: { error?: string } } }).response?.data?.error
            : err instanceof Error
              ? err.message
              : "Authorization failed";
        setAsyncError(errorMsg || "Authorization failed");
      }
    };

    performAuthorization();

    return () => {
      isMounted = false;
    };
  }, [missingParams, service, redirectUri, codeChallenge, state, location.pathname, location.search, accessToken, navigate]);

  const error = missingParams
    ? "Missing required authorization parameters (service, redirect_uri, code_challenge, state)."
    : asyncError;

  return (
    <Theme theme="white">
      <Grid fullWidth className={styles.container}>
        <Column lg={8} md={4} sm={4} className={styles.content}>
          <h2>Authorizing Service</h2>
          {loading && !error && (
            <div className={styles.loadingContainer}>
              <Loading description="Authorizing request..." withOverlay={false} />
              <p>Completing authentication and redirecting...</p>
            </div>
          )}
          {error && (
            <InlineNotification
              kind="error"
              role="alert"
              title="Authorization Error"
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

export default AuthorizePage;
