import { useState } from "react";
import AppHeader from "@/components/AppHeader";
import Navbar from "@/components/Navbar";

const Login = () => {
  const [isSideNavOpen, setIsSideNavOpen] = useState(false);

  return (
    <>
      <AppHeader
        isSideNavOpen={isSideNavOpen}
        setIsSideNavOpen={setIsSideNavOpen}
      />

      <Navbar isSideNavOpen={isSideNavOpen} />

      <main
        style={{
          marginLeft: isSideNavOpen ? "256px" : "0",
          padding: "2rem",
          transition: "margin 0.2s ease",
        }}
      ></main>
    </>
  );
};

export default Login;
