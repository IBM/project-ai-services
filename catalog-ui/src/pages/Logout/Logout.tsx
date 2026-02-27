import styles from "./Logout.module.scss";
import { Theme } from "@carbon/react";

const Logout = () => {
  return (
    <Theme theme="white">
      <div className={styles.pageContent}>
        <h1 className={styles.heading}>
          <span>
            IBM <strong>Open-Source AI Foundation for Power</strong>
          </span>
          <span>You are now logged out.</span>
        </h1>
        <a className={styles.loginLink} href="/login">
          Return to the log in page now
        </a>
      </div>
    </Theme>
  );
};

export default Logout;
