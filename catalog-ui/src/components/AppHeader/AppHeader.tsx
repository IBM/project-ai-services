import {
  Header,
  HeaderName,
  HeaderGlobalBar,
  HeaderGlobalAction,
  HeaderMenuButton,
  Theme,
} from "@carbon/react";
import { Help, Notification, User } from "@carbon/icons-react";

interface AppHeaderProps {
  isSideNavOpen: boolean;
  setIsSideNavOpen: React.Dispatch<React.SetStateAction<boolean>>;
}

const AppHeader = ({ isSideNavOpen, setIsSideNavOpen }: AppHeaderProps) => {
  return (
    <Theme theme="g100">
      <Header aria-label="IBM Power Operations Platform">
        <HeaderMenuButton
          aria-label="Open menu"
          onClick={() => setIsSideNavOpen(!isSideNavOpen)}
          isActive={isSideNavOpen}
          isCollapsible
        />

        <HeaderName prefix="IBM">Power Operations Platform</HeaderName>

        <HeaderGlobalBar>
          <HeaderGlobalAction aria-label="Help">
            <Help size={20} />
          </HeaderGlobalAction>

          <HeaderGlobalAction aria-label="Notifications">
            <Notification size={20} />
          </HeaderGlobalAction>

          <HeaderGlobalAction aria-label="User">
            <User size={20} />
          </HeaderGlobalAction>
        </HeaderGlobalBar>
      </Header>
    </Theme>
  );
};

export default AppHeader;
