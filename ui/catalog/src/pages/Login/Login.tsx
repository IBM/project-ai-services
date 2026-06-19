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
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import styles from "./Login.module.scss";
import { login } from "@/services/auth";
import { ROUTES } from "@/constants/endpoints.constants";
import axios from "axios";

const LoginPage = () => {
  const navigate = useNavigate();

  const [username, setUsername] = useState<string>("");
  const [password, setPassword] = useState<string>("");

  const [credentialError, setCredentialError] = useState<boolean>(false);
  const [networkError, setNetworkError] = useState<boolean>(false);
  const [loading, setLoading] = useState<boolean>(false);

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
      // Check if it's a 401 Unauthorized (wrong credentials)
      if (axios.isAxiosError(error) && error.response?.status === 401) {
        setCredentialError(true);
      } else {
        // Network error or other server errors
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
