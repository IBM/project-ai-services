import styles from "./Login.module.scss";
import Header from "@/components/Header";

const Login = () => {
  return (
    <div className={styles.pageContent}>
      <Header />
      <h1 className={styles.heading}>Login Page</h1>
    </div>
  );
};

export default Login;
