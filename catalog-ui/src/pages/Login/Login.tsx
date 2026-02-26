import { Button, InlineNotification, TextInput } from "@carbon/react";
import { Theme } from "@carbon/react";
import { ArrowRight } from "@carbon/icons-react";
import styles from "./Login.module.scss";
import { useState } from "react";

const LoginPage = () => {
  const [error, setError] = useState<boolean>(false);

  return (
    <Theme theme="white">
      <div className={styles.loginPage}>
        <div className={styles.loginLeft}>
          <div className={styles.loginForm}>
            <h1 className={styles.heading}>
              Log in to IBM <strong>Open-Source AI Foundation for Power</strong>
            </h1>
            <div className={styles.inputFields}>
              {error && (
                <InlineNotification
                  kind="error"
                  title="Incorrect user ID or password."
                  hideCloseButton
                  lowContrast
                />
              )}
              <TextInput
                id="user-id"
                labelText="User ID"
                placeholder="username@example.com"
                type="text"
                invalid={error}
              />
              <TextInput
                id="password"
                labelText="Password"
                type="password"
                invalid={error}
              />
              <Button
                kind="primary"
                className={styles.continueButton}
                renderIcon={ArrowRight}
                onClick={() => {
                  setError((prev) => !prev);
                }}
              >
                Continue
              </Button>
            </div>
          </div>
        </div>
        <div className={styles.loginRight}></div>
      </div>
    </Theme>
  );
};

export default LoginPage;
