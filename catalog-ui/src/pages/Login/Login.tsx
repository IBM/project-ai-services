import { useState, useRef, useEffect } from "react";
import AppHeader from "@/components/AppHeader";
import Navbar from "@/components/Navbar";

const Login = () => {
  const [isSideNavOpen, setIsSideNavOpen] = useState(false);
  const sideNavRef = useRef<HTMLElement>(null!);

  useEffect(() => {
    function handleOutsideClick(e: MouseEvent) {
      if (!isSideNavOpen) return;
      const target = e.target as Node;
      if (sideNavRef.current && sideNavRef.current.contains(target)) return;
      setIsSideNavOpen(false);
    }
    document.addEventListener("mousedown", handleOutsideClick);
    return () => document.removeEventListener("mousedown", handleOutsideClick);
  }, [isSideNavOpen]);
  return (
    <>
      <AppHeader
        isSideNavOpen={isSideNavOpen}
        setIsSideNavOpen={setIsSideNavOpen}
      />

      <Navbar isSideNavOpen={isSideNavOpen} sideNavRef={sideNavRef} />

      <main></main>
    </>
  );
};

export default Login;
