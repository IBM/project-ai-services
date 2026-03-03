import { useEffect } from "react";
import styles from "./Logout.module.scss";
import { Theme } from "@carbon/react";
import { useNavigate } from "react-router-dom";

const Logout = () => {
  const navigate = useNavigate();

  useEffect(() => {
    const timer = setTimeout(() => {
      navigate("/login", { replace: true });
    }, 5000);

    return () => clearTimeout(timer);
  }, [navigate]);

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
