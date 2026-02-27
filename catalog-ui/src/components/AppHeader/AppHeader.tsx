import {
  Header,
  HeaderName,
  HeaderGlobalBar,
  HeaderGlobalAction,
  HeaderMenuButton,
  HeaderPanel,
  Theme,
  Modal,
} from "@carbon/react";
import { Help, Notification, User, Logout } from "@carbon/icons-react";
import styles from "./AppHeader.module.scss";
import { useState, useRef, useEffect } from "react";
import { useNavigate } from "react-router-dom";

type AppHeaderProps =
  | {
      minimal: true;
    }
  | {
      minimal?: false;
      isSideNavOpen: boolean;
      setIsSideNavOpen: React.Dispatch<React.SetStateAction<boolean>>;
    };

const AppHeader = (props: AppHeaderProps) => {
  const minimal = props.minimal === true;
  const [isProfileOpen, setIsProfileOpen] = useState(false);
  const panelRef = useRef<HTMLDivElement>(null);
  const userIconRef = useRef<HTMLButtonElement | null>(null);
  const navigate = useNavigate();
  const [isLogoutModalOpen, setIsLogoutModalOpen] = useState(false);
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as Node;
      if (
        panelRef.current &&
        !panelRef.current.contains(target) &&
        !(userIconRef.current && userIconRef.current.contains(target))
      ) {
        setIsProfileOpen(false);
      }
    };

    if (isProfileOpen) {
      document.addEventListener("mousedown", handleClickOutside);
    }

    return () => {
      document.removeEventListener("mousedown", handleClickOutside);
    };
  }, [isProfileOpen]);

  return (
    <Theme theme="g100">
      <Header aria-label="IBM Power Operations Platform">
        {!minimal && (
          <HeaderMenuButton
            aria-label="Open menu"
            onClick={(e) => {
              e.stopPropagation();
              props.setIsSideNavOpen((prev) => !prev);
            }}
            isActive={props.isSideNavOpen}
            isCollapsible
            className={styles.menuBtn}
          />
        )}

        <HeaderName prefix="IBM">Power Operations Platform</HeaderName>

        {!minimal && (
          <HeaderGlobalBar>
            <HeaderGlobalAction aria-label="Help" className={styles.iconWidth}>
              <Help size={20} />
            </HeaderGlobalAction>

            <HeaderGlobalAction
              aria-label="Notifications"
              className={styles.iconWidth}
            >
              <Notification size={20} />
            </HeaderGlobalAction>

            <HeaderGlobalAction
              aria-label="User"
              className={styles.iconWidth}
              isActive={isProfileOpen}
              onClick={() => setIsProfileOpen((prev) => !prev)}
              ref={userIconRef}
            >
              <User size={20} />
            </HeaderGlobalAction>
            <HeaderPanel ref={panelRef} expanded={isProfileOpen}>
              <div>
                <div className={styles.userprofile}>
                  <div>
                    <strong>Admin</strong>
                  </div>
                  <div className={styles.usercircle}>
                    <User size={16} />
                  </div>
                </div>

                <div
                  className={styles.logout}
                  onClick={() => {
                    setIsProfileOpen(false);
                    setIsLogoutModalOpen(true);
                  }}
                >
                  <div>Log out</div>
                  <div>
                    <Logout size={16} />
                  </div>
                </div>
              </div>
            </HeaderPanel>
            <Theme theme="g10">
              <Modal
                open={isLogoutModalOpen}
                size="xs"
                primaryButtonText="Log out"
                secondaryButtonText="Cancel"
                onRequestClose={() => setIsLogoutModalOpen(false)}
                onRequestSubmit={() => {
                  setIsLogoutModalOpen(false);
                  navigate("/logout");
                }}
              >
                <p>
                  Are you sure you want to log out of IBM Open-Source AI
                  Foundation for Power?
                </p>
              </Modal>
            </Theme>
          </HeaderGlobalBar>
        )}
      </Header>
    </Theme>
  );
};

export default AppHeader;
