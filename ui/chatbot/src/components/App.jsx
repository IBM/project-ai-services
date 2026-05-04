import { useEffect, useState } from 'react';
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
import { ApiKeyDialog } from './ApiKeyDialog.jsx';
import { customSendMessage } from './customSendMessage.jsx';
import HeaderNav from './Header.jsx';
import { renderUserDefinedResponse } from './renderUserDefinedResponse.jsx';

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
  const [apiKey, setApiKey] = useState(null);
  const [showApiKeyDialog, setShowApiKeyDialog] = useState(false);
  const [authRequired, setAuthRequired] = useState(null); // null = checking, true/false = determined
  const [isReady, setIsReady] = useState(false);

  useEffect(() => {
    // Check if authentication is required by probing the models endpoint
    const checkAuthRequirement = async () => {
      try {
        const response = await fetch('/v1/models');

        if (response.status === 401) {
          setAuthRequired(true);
          setShowApiKeyDialog(true);
          return;
        }

        if (!response.ok) {
          throw new Error(
            `Unexpected status while checking auth requirement: ${response.status}`,
          );
        }

        setAuthRequired(false);
        setApiKey('not-needed');
        setIsReady(true);
      } catch (error) {
        console.error('Error checking auth requirement:', error);
        // On error, assume auth is required to be safe
        setAuthRequired(true);
        setShowApiKeyDialog(true);
      }
    };

    checkAuthRequirement();
  }, []);

  const handleApiKeyValidated = (validatedKey) => {
    setApiKey(validatedKey);
    setShowApiKeyDialog(false);
    setIsReady(true);
  };

  const handleAuthError = () => {
    // Only handle auth errors if auth is actually required
    if (authRequired) {
      // Clear invalid API key and show dialog again
      setApiKey(null);
      setShowApiKeyDialog(true);
      setIsReady(false);
    }
  };

  // Create messaging object with API key
  const messaging = {
    customSendMessage: (request, options, instance) =>
      customSendMessage(request, options, instance, apiKey, handleAuthError),
  };

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
      <ApiKeyDialog
        isOpen={showApiKeyDialog}
        onApiKeyValidated={handleApiKeyValidated}
      />
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
                {isReady && apiKey && (
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
