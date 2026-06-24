import {
  Button,
  InlineNotification,
  TextInput,
  Theme,
  Grid,
  Column,
  ToastNotification,
} from "@carbon/react";
import { ArrowRight } from "@carbon/icons-react";
import { useState, useEffect } from "react";
import { useNavigate, useLocation } from "react-router-dom";
import styles from "./Login.module.scss";
import { login } from "@/services/auth";
import { ROUTES } from "@/constants/endpoints.constants";
import {
  LogoutReason,
  SESSION_STORAGE_KEYS,
  type LoginLocationState,
} from "@/types/navigation.types";
import axios from "axios";

const LoginPage = () => {
  const navigate = useNavigate();
  const location = useLocation();

  const [username, setUsername] = useState<string>("");
  const [password, setPassword] = useState<string>("");

  const [credentialError, setCredentialError] = useState<boolean>(false);
  const [networkError, setNetworkError] = useState<boolean>(false);
  const [loading, setLoading] = useState<boolean>(false);
  const [showInactivityNotification, setShowInactivityNotification] =
    useState<boolean>(false);

  useEffect(() => {
    const locationState = location.state as LoginLocationState | null;
    const logoutReason = locationState?.logoutReason;
    const storedReason = sessionStorage.getItem(
      SESSION_STORAGE_KEYS.LOGOUT_REASON,
    );

    if (
      logoutReason === LogoutReason.INACTIVITY ||
      storedReason === LogoutReason.INACTIVITY
    ) {
      setShowInactivityNotification(true);

      sessionStorage.removeItem(SESSION_STORAGE_KEYS.LOGOUT_REASON);
      sessionStorage.removeItem(SESSION_STORAGE_KEYS.LOGOUT_MESSAGE);

      if (locationState) {
        navigate(location.pathname, { replace: true, state: null });
      }
    }
  }, [location, navigate]);

  const handleLogin = async (): Promise<void> => {
    setCredentialError(false);
    setNetworkError(false);
    setLoading(true);

    try {
      await login({
        username,
        password,
      });

      navigate(ROUTES.DIGITAL_ASSISTANTS);
    } catch (error) {
      if (axios.isAxiosError(error) && error.response) {
        // Treat 400 and 401 as credential/validation errors
        if (error.response.status === 400 || error.response.status === 401) {
          setCredentialError(true);
        } else {
          // 5xx server errors or other unexpected errors
          setNetworkError(true);
        }
      } else {
        // Network error (no response from server)
        setNetworkError(true);
      }
    } finally {
      setLoading(false);
    }
  };

  return (
    <Theme theme="white">
      {networkError && (
        <ToastNotification
          kind="error"
          title="Network error"
          subtitle="Unable to connect to server. Please try again."
          timeout={5000}
          onClose={() => setNetworkError(false)}
          className={styles.toastNotification}
        />
      )}
      <Grid fullWidth className={styles.loginPage}>
        <Column lg={8} md={4} sm={4} className={styles.loginLeft}>
          <div className={styles.loginForm}>
            <h1 className={styles.heading}>
              Log in to <strong>AI Services</strong>
            </h1>

            <form
              className={styles.inputFields}
              onSubmit={(e) => {
                e.preventDefault();
                handleLogin();
              }}
            >
              {showInactivityNotification && (
                <InlineNotification
                  kind="warning"
                  role="alert"
                  title="Session expired"
                  subtitle="You were logged out due to inactivity."
                  lowContrast
                  hideCloseButton={false}
                  onCloseButtonClick={() =>
                    setShowInactivityNotification(false)
                  }
                />
              )}

              {credentialError && (
                <InlineNotification
                  kind="error"
                  role="alert"
                  title="Incorrect user ID or password."
                  lowContrast
                />
              )}

              <TextInput
                id="user-id"
                labelText="User ID"
                value={username}
                onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                  setUsername(e.target.value)
                }
                invalid={credentialError}
              />

              <TextInput
                id="password"
                labelText="Password"
                type="password"
                value={password}
                onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                  setPassword(e.target.value)
                }
                invalid={credentialError}
              />

              <Button
                type="submit"
                kind="primary"
                renderIcon={ArrowRight}
                className={styles.continueButton}
                disabled={loading}
              >
                {loading ? "Logging in..." : "Log in"}
              </Button>
            </form>
          </div>
        </Column>

        <Column lg={8} md={4} sm={0} className={styles.loginRight} />
      </Grid>
    </Theme>
  );
};

export default LoginPage;
