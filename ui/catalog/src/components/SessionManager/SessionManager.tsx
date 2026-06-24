import type { ReactNode } from "react";
import { useSessionTimeout } from "@/hooks/useSessionTimeout";
import SessionTimeoutModal from "@/components/SessionTimeoutModal";

interface SessionManagerProps {
  children: ReactNode;
}

const SessionManager = ({ children }: SessionManagerProps) => {
  const { showWarning, timeRemaining, extendSession } = useSessionTimeout();

  return (
    <>
      {children}
      <SessionTimeoutModal
        open={showWarning}
        timeRemaining={timeRemaining}
        onExtendSession={extendSession}
      />
    </>
  );
};

export default SessionManager;
