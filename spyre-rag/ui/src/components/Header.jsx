import { Renew } from '@carbon/icons-react';
import {
  Header,
  HeaderGlobalAction,
  HeaderGlobalBar,
  HeaderName,
} from '@carbon/react';

const HeaderNav = ({ onNewChat, showNewChatButton }) => {
  return (
    <Header>
      <HeaderName to="/" prefix="">
        DigitalAssistant
      </HeaderName>
      {showNewChatButton && (
        <HeaderGlobalBar>
          <HeaderGlobalAction
            aria-label="Start new conversation"
            tooltipAlignment="end"
            onClick={onNewChat}
          >
            <Renew size={20} />
          </HeaderGlobalAction>
        </HeaderGlobalBar>
      )}
    </Header>
  );
};

export default HeaderNav;
