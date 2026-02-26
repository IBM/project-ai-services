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

type AppHeaderProps = {
  isSideNavOpen: boolean;
  setIsSideNavOpen: React.Dispatch<React.SetStateAction<boolean>>;
};

const AppHeader = (props: AppHeaderProps) => {
  const { isSideNavOpen, setIsSideNavOpen } = props;
  return (
    <Theme theme="g100">
      <Header aria-label="IBM Power Operations Platform">
        <HeaderMenuButton
          aria-label="Open menu"
          onClick={(e) => {
            e.stopPropagation();
            setIsSideNavOpen((prev) => !prev);
          }}
          isActive={isSideNavOpen}
          isCollapsible
          className={styles.menuBtn}
        />

        <HeaderName prefix="IBM">Power Operations Platform</HeaderName>

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
            <User size={20} className={styles.iconWidth} />
          </HeaderGlobalAction>
        </HeaderGlobalBar>
      </Header>
    </Theme>
  );
};

export default AppHeader;
