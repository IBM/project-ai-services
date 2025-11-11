import React, { useState } from "react";
import {
  ChatCustomElement,
  BusEventType,
  FeedbackInteractionType,
  CornersType,
  UserType,
} from "@carbon/ai-chat";
import "./App.scss"
import HeaderNav from "./Header.jsx"
import { Theme, Content, Grid, Column } from "@carbon/react";
import { customSendMessage } from "./customSendMessage.jsx";
import { renderUserDefinedResponse } from "./renderUserDefinedResponse.jsx";

const config = {
  messaging: {
    customSendMessage,
  },
  headerConfig: {
    hideMinimizeButton: true,
    minimizeButtonIconType: undefined,
  },
  themeConfig: {
    corners: CornersType.SQUARE,
  },
  layout: {
    hasContentMaxWidth: false,
  },
  openChatByDefault: true,
};

function App() {
  const [chatInstance, setChatInstance] = useState();

  function onBeforeRender(instance) {

    instance.updateMainHeaderTitle("DocuAssistant");
    instance.on({ type: BusEventType.FEEDBACK, handler: feedbackHandler });
    setChatInstance(instance);

    // Delay welcome message to avoid triggering initial loading indicator
    setTimeout(() => {
      instance.messaging.addMessage({
        output: {
          generic: [
            {
              response_type: "text",
              text: `Hi, I'm your assistant! You can ask me anything related to your documents`,
            },
          ],
        },
        message_options: {
          response_user_profile: {
            id: "assistant",
            nickname: "DocuAgent",
            user_type: UserType.BOT,
          },
        },
      });
    }, 300);
  }

  function feedbackHandler(event) {
    if (event.interactionType === FeedbackInteractionType.SUBMITTED) {
      const { message, messageItem, ...reportData } = event;
      setTimeout(() => {
        window.alert(JSON.stringify(reportData, null, 2));
      });
    }
  }


  return (
      <React.Fragment>
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
                    config={config}
                    className="fullScreen"
                    onBeforeRender={onBeforeRender}
                    renderUserDefinedResponse={renderUserDefinedResponse}
                  />
                </div>
              </Column>
            </Grid>
          </Content>
        </Theme>
      </React.Fragment>
  );
}

export default App;
