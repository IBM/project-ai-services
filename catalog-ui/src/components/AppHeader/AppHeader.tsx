import {
  Header,
  HeaderName,
  HeaderGlobalBar,
  HeaderGlobalAction,
  HeaderMenuButton,
  Theme,
} from "@carbon/react";
import { Help, Notification, User } from "@carbon/icons-react";
import styles from "./AppHeader.module.scss";

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

            <HeaderGlobalAction aria-label="User" className={styles.iconWidth}>
              <User size={20} />
            </HeaderGlobalAction>
          </HeaderGlobalBar>
        )}
      </Header>
    </Theme>
  );
};

export default AppHeader;
