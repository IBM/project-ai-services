import {
  Button,
  InlineNotification,
  TextInput,
  Theme,
  Grid,
  Column,
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

const LoginPage = () => {
  const navigate = useNavigate();
  const location = useLocation();

  const [username, setUsername] = useState<string>("");
  const [password, setPassword] = useState<string>("");

  const [error, setError] = useState<boolean>(false);
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
    setError(false);
    setLoading(true);

    try {
      await login({
        username,
        password,
      });

      navigate(ROUTES.DIGITAL_ASSISTANTS);
    } catch {
      setError(true);
    } finally {
      setLoading(false);
    }
  };

  return (
    <Theme theme="white">
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

              {error && (
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
                invalid={error}
              />

              <TextInput
                id="password"
                labelText="Password"
                type="password"
                value={password}
                onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                  setPassword(e.target.value)
                }
                invalid={error}
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
