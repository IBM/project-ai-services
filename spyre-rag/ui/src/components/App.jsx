import {
  BusEventType,
  ChatCustomElement,
  CornersType,
  FeedbackInteractionType,
  UserType,
} from '@carbon/ai-chat';
import './App.scss';
import { Column, Content, Grid, Theme } from '@carbon/react';
import { AIExplanationCard } from './AIExplanationCard.jsx';
import { customSendMessage } from './customSendMessage.jsx';
import HeaderNav from './Header.jsx';
import { renderUserDefinedResponse } from './renderUserDefinedResponse.jsx';

const messaging = {
  customSendMessage,
};

const header = {
  title: 'DigitalAssistant',
  hideMinimizeButton: true,
  minimizeButtonIconType: undefined,
};

const layout = {
  corners: CornersType.SQUARE,
  hasContentMaxWidth: false,
};

function App() {
  function onAfterRender(instance) {
    instance.on({ type: BusEventType.FEEDBACK, handler: feedbackHandler });

    instance.messaging.addMessage({
      output: {
        generic: [
          {
            response_type: 'text',
            text: `Hi, I'm your assistant! You can ask me anything related to your documents`,
          },
        ],
      },
      message_options: {
        response_user_profile: {
          id: 'assistant',
          nickname: 'Assistant',
          user_type: UserType.BOT,
        },
      },
    });
  }

  function feedbackHandler(event) {
    if (event.interactionType === FeedbackInteractionType.SUBMITTED) {
      const {
        message: _message,
        messageItem: _messageItem,
        ...reportData
      } = event;
      setTimeout(() => {
        window.alert(JSON.stringify(reportData, null, 2));
      });
    }
  }

  return (
    <>
      <Theme theme="white">
        <Content id="main-content">
          <Grid fullWidth className="chat-page-grid">
            <Column sm={4} md={8} lg={12}>
              <Theme theme="g90">
                <HeaderNav />
              </Theme>
            </Column>
            <Column sm={4} md={8} lg={12}>
              <div className="chat-container">
                <ChatCustomElement
                  className="fullScreen"
                  messaging={messaging}
                  header={header}
                  layout={layout}
                  openChatByDefault={true}
                  onAfterRender={onAfterRender}
                  renderUserDefinedResponse={renderUserDefinedResponse}
                  strings={{
                    ai_slug_title: undefined,
                    ai_slug_description: <AIExplanationCard />,
                  }}
                />
              </div>
            </Column>
          </Grid>
        </Content>
      </Theme>
    </>
  );
}

export default App;
