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
import { useReducer, useRef, useEffect } from "react";
import { useNavigate } from "react-router-dom";
import { logout } from "@/services/auth";

type AppHeaderProps =
  | {
      minimal: true;
    }
  | {
      minimal?: false;
      isSideNavOpen: boolean;
      setIsSideNavOpen: React.Dispatch<React.SetStateAction<boolean>>;
    };

interface HeaderState {
  isProfileOpen: boolean;
  isLogoutModalOpen: boolean;
}

type HeaderAction =
  | { type: "SET_PROFILE_OPEN"; payload: boolean }
  | { type: "TOGGLE_PROFILE" }
  | { type: "SET_LOGOUT_MODAL_OPEN"; payload: boolean }
  | { type: "CLOSE_PROFILE_AND_OPEN_LOGOUT" }
  | { type: "CLOSE_LOGOUT_MODAL" };

const initialState: HeaderState = {
  isProfileOpen: false,
  isLogoutModalOpen: false,
};

function headerReducer(state: HeaderState, action: HeaderAction): HeaderState {
  switch (action.type) {
    case "SET_PROFILE_OPEN":
      return { ...state, isProfileOpen: action.payload };
    case "TOGGLE_PROFILE":
      return { ...state, isProfileOpen: !state.isProfileOpen };
    case "SET_LOGOUT_MODAL_OPEN":
      return { ...state, isLogoutModalOpen: action.payload };
    case "CLOSE_PROFILE_AND_OPEN_LOGOUT":
      return { ...state, isProfileOpen: false, isLogoutModalOpen: true };
    case "CLOSE_LOGOUT_MODAL":
      return { ...state, isLogoutModalOpen: false };
    default:
      return state;
  }
}

const AppHeader = (props: AppHeaderProps) => {
  const minimal = props.minimal === true;
  const [state, dispatch] = useReducer(headerReducer, initialState);
  const panelRef = useRef<HTMLDivElement>(null);
  const userIconRef = useRef<HTMLButtonElement | null>(null);
  const navigate = useNavigate();

  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as Node;
      if (
        panelRef.current &&
        !panelRef.current.contains(target) &&
        !(userIconRef.current && userIconRef.current.contains(target))
      ) {
        dispatch({ type: "SET_PROFILE_OPEN", payload: false });
      }
    };

    if (state.isProfileOpen) {
      document.addEventListener("mousedown", handleClickOutside);
    }

    return () => {
      document.removeEventListener("mousedown", handleClickOutside);
    };
  }, [state.isProfileOpen]);

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
              aria-haspopup="menu"
              aria-expanded={state.isProfileOpen}
              className={styles.iconWidth}
              isActive={state.isProfileOpen}
              onClick={() => dispatch({ type: "TOGGLE_PROFILE" })}
              ref={userIconRef}
            >
              <User size={20} />
            </HeaderGlobalAction>
            <HeaderPanel ref={panelRef} expanded={state.isProfileOpen}>
              <div>
                <div className={styles.userprofile}>
                  <div>
                    <strong>Admin</strong>
                  </div>
                  <div className={styles.usercircle}>
                    <User size={16} />
                  </div>
                </div>

                <button
                  type="button"
                  className={styles.logout}
                  onClick={() => {
                    dispatch({ type: "CLOSE_PROFILE_AND_OPEN_LOGOUT" });
                  }}
                >
                  <span>Log out</span>
                  <Logout size={16} />
                </button>
              </div>
            </HeaderPanel>
            <Theme theme="g10">
              <Modal
                open={state.isLogoutModalOpen}
                size="xs"
                primaryButtonText="Log out"
                secondaryButtonText="Cancel"
                onRequestClose={() => {
                  dispatch({ type: "CLOSE_LOGOUT_MODAL" });
                }}
                onRequestSubmit={async () => {
                  dispatch({ type: "CLOSE_LOGOUT_MODAL" });

                  const token = localStorage.getItem("access_token");

                  try {
                    if (token) {
                      await logout(token);
                    }
                  } catch (err) {
                    console.error("Logout API failed:", err);
                  } finally {
                    localStorage.removeItem("access_token");
                    localStorage.removeItem("refresh_token");
                    navigate("/logout", { replace: true });
                  }
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
