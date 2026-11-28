import { useCallback, useEffect, useRef, useState } from 'react';
import {
  BusEventType,
  ChatCustomElement,
  CornersType,
  FeedbackInteractionType,
  UserType,
} from '@carbon/ai-chat';
import './App.scss';
import { Column, Content, Grid, Theme } from '@carbon/react';
import {
  CALLBACK_PATH,
  clearServiceToken,
  isTokenExpiringSoon,
  startLoginFlow,
} from '../services/auth.js';
import { AIExplanationCard } from './AIExplanationCard.jsx';
import { AuthCallback } from './AuthCallback.jsx';
import { customSendMessage } from './customSendMessage.jsx';
import HeaderNav from './Header.jsx';
import { renderUserDefinedResponse } from './renderUserDefinedResponse.jsx';

const WELCOME_MESSAGE = `Hi, I'm your assistant! You can ask me anything related to your documents`;

const layout = {
  corners: CornersType.SQUARE,
  hasContentMaxWidth: false,
};

function App() {
  // The token is held in memory by services/auth.js, never in storage, so a page load starts
  // signed out and goes to the catalog. This state only says whether the chat may be shown.
  const [isCallback, setIsCallback] = useState(
    window.location.pathname === CALLBACK_PATH,
  );
  const [isSignedIn, setIsSignedIn] = useState(!isTokenExpiringSoon());
  const [conversationHistory, setConversationHistory] = useState([]);
  const chatInstanceRef = useRef(null);

  useEffect(() => {
    if (!isCallback && !isSignedIn) {
      startLoginFlow();
    }
  }, [isCallback, isSignedIn]);

  const handleCallbackDone = useCallback(() => {
    setIsCallback(false);
    setIsSignedIn(true);
  }, []);

  // Called after a 401 from the backend: the token is no longer accepted, sign in again.
  const handleAuthError = () => {
    clearServiceToken();
    startLoginFlow();
  };

  const handleResetChat = () => {
    // Perform a page refresh to start a new session. This signs in again, which the catalog
    // answers at once while its own session is still valid.
    window.location.reload();
  };

  const header = {
    title: 'DigitalAssistant',
    hideMinimizeButton: true,
    minimizeButtonIconType: undefined,
  };

  // Create messaging object with the auth-error handler and conversation history
  const messaging = {
    customSendMessage: (request, options, instance) =>
      customSendMessage(
        request,
        options,
        instance,
        handleAuthError,
        conversationHistory,
        setConversationHistory,
      ),
  };

  function onAfterRender(instance) {
    // Store the chat instance reference
    chatInstanceRef.current = instance;

    instance.on({ type: BusEventType.FEEDBACK, handler: feedbackHandler });

    instance.messaging.addMessage({
      output: {
        generic: [
          {
            response_type: 'text',
            text: WELCOME_MESSAGE,
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

  if (isCallback) {
    return (
      <Theme theme="white">
        <AuthCallback onDone={handleCallbackDone} />
      </Theme>
    );
  }

  return (
    <>
      <Theme theme="white">
        <Content id="main-content">
          <Grid fullWidth className="chat-page-grid">
            <Column sm={4} md={8} lg={12}>
              <Theme theme="g90">
                <HeaderNav
                  onNewChat={handleResetChat}
                  showNewChatButton={isSignedIn}
                />
              </Theme>
            </Column>
            <Column sm={4} md={8} lg={12}>
              <div className="chat-container">
                {isSignedIn && (
                  <ChatCustomElement
                    className="fullScreen"
                    messaging={messaging}
                    header={header}
                    layout={layout}
                    openChatByDefault={true}
                    onAfterRender={onAfterRender}
                    renderUserDefinedResponse={renderUserDefinedResponse}
                    strings={{
                      ai_slug_title: '',
                      ai_slug_description: <AIExplanationCard />,
                    }}
                  />
                )}
              </div>
            </Column>
          </Grid>
        </Content>
      </Theme>
    </>
  );
}

export default App;
