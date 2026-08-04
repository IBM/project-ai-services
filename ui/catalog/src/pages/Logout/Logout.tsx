import { useEffect, useState } from "react";
import styles from "./Logout.module.scss";
import { Theme, ToastNotification } from "@carbon/react";
import { useNavigate, Link } from "react-router-dom";
import { logout } from "@/services/auth";

const Logout = () => {
  const navigate = useNavigate();
  const [isLoggingOut, setIsLoggingOut] = useState(true);
  const [showErrorToast, setShowErrorToast] = useState(false);

  useEffect(() => {
    const performLogout = async () => {
      try {
        await logout();
      } catch (error) {
        console.error("Logout error:", error);
        setShowErrorToast(true);
      } finally {
        setIsLoggingOut(false);
      }
    };

    performLogout();
  }, []);

  useEffect(() => {
    if (!isLoggingOut) {
      const timer = setTimeout(() => {
        navigate("/login", { replace: true });
      }, 5000);

      return () => clearTimeout(timer);
    }
  }, [isLoggingOut, navigate]);

  return (
    <Theme theme="white">
      {showErrorToast && (
        <ToastNotification
          kind="error"
          title="Logout failed"
          subtitle="Unable to connect to server. You have been logged out locally."
          timeout={5000}
          onClose={() => setShowErrorToast(false)}
          className={styles.toastNotification}
        />
      )}
      <div className={styles.pageContent}>
        <h1 className={styles.heading}>
          <span>
            <strong>AI Services</strong>
          </span>
          <span>
            {isLoggingOut ? "Logging out..." : "You are now logged out."}
          </span>
        </h1>

        {!isLoggingOut && (
          <Link to="/login" className={styles.loginLink}>
            Return to the log in page now
          </Link>
        )}
      </div>
    </Theme>
  );
};

export default Logout;
