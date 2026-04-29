import {
  Button,
  InlineNotification,
  TextInput,
  Theme,
  Grid,
  Column,
} from "@carbon/react";
import { ArrowRight } from "@carbon/icons-react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import styles from "./Login.module.scss";
import { login } from "@/services/auth";

const LoginPage = () => {
  const navigate = useNavigate();

  const [username, setUsername] = useState<string>("");
  const [password, setPassword] = useState<string>("");

  const [error, setError] = useState<boolean>(false);
  const [loading, setLoading] = useState<boolean>(false);

  const handleLogin = async (): Promise<void> => {
    setError(false);
    setLoading(true);

    try {
      await login({
        username,
        password,
      });

      navigate("/ai-deployments");
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
              Log in to IBM <strong>Open-Source AI Foundation for Power</strong>
            </h1>

            <form
              className={styles.inputFields}
              onSubmit={(e) => {
                e.preventDefault();
                handleLogin();
              }}
            >
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
