import { UserType } from '@carbon/ai-chat';
import axios from 'axios';
import { OpenAI } from 'openai';

const client = new OpenAI({
  baseURL: window.location.origin + '/v1',
  apiKey: 'not-needed',
  dangerouslyAllowBrowser: true, // Required for browser-side use to allow api-key
});

async function customSendMessage(request, _options, instance) {
  const userInput = request.input.text;

  try {
    const res = await axios.get('/db-status');
    if (res.data.ready === false) {
      await instance.messaging.addMessage({
        output: {
          generic: [
            {
              response_type: 'text',
              text: '⚠️ The knowledge database is not ready. Please ingest documents first.',
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
      return;
    }
  } catch {
    // No action needed
  }

  const ResponseUserProfile = {
    id: 'assistant',
    nickname: 'Assistant',
    user_type: UserType.BOT,
  };

  function finalizeResponse(fullText) {
    let trimmed = fullText.trim(); // to remove trailing white-space
    // Define acceptable sentence-ending punctuation (both Hindi + English)
    const validEndings = ['।', '.', '?', '!', '…']; // also includes ellipsis itself
    const lastChar = trimmed.charAt(trimmed.length - 1);
    if (!validEndings.includes(lastChar)) {
      trimmed += ' ...';
    }
    return trimmed;
  }

  if (userInput === '') {
    if (
      instance.messaging &&
      instance.messaging.addMessage &&
      typeof instance.messaging.addMessage === 'function'
    ) {
      // sendWelcomeMessage(instance);
      return;
    }
  }
  const responseId = String(Date.now()); // or any unique ID
  const itemId = '1'; // single item per response, or generate if multiple

  //Adding initial partial chunk (this triggers the bubble with "stop streaming" button)
  await instance.messaging.addMessageChunk({
    partial_item: {
      response_type: 'text',
      text: '', // start empty
      streaming_metadata: {
        id: itemId,
        cancellable: true,
      },
    },
    streaming_metadata: {
      response_id: responseId,
    },
    partial_response: {
      message_options: {
        response_user_profile: ResponseUserProfile,
      },
    },
  });

  const payload = {
    messages: [{ role: 'user', content: userInput }],
    model: 'ibm-granite/granite-3.3-8b-instruct',
    temperature: 0.0,
    stream: true,
  };

  let isCanceled = false;
  const abortHandler = () => {
    isCanceled = true;
  };
  // Listen to abort signal (handles stop button, restart/clear, and timeout)
  _options.signal?.addEventListener('abort', abortHandler);

  try {
    instance.updateIsMessageLoadingCounter('increase');

    const stream = await client.chat.completions.create(payload);

    const context_response = await axios.post('/reference', {
      prompt: userInput,
      headers: {
        'Content-Type': 'application/json',
      },
    });

    instance.updateIsMessageLoadingCounter('decrease');

    let fullText = ''; // to accumulate final message

    for await (const chunk of stream) {
      if (isCanceled) break;

      // to extract the content from the parsed JSON chunk
      const textChunk = chunk.choices[0]?.delta?.content || '';

      if (textChunk) {
        fullText += textChunk;

        await instance.messaging.addMessageChunk({
          partial_item: {
            response_type: 'text',
            text: textChunk,
            streaming_metadata: {
              id: itemId,
              cancellable: true,
            },
          },
          streaming_metadata: {
            response_id: responseId,
          },
          partial_response: {
            message_options: {
              response_user_profile: ResponseUserProfile,
            },
          },
        });
      }
    }

    fullText = finalizeResponse(fullText);
    // Complete item chunk (used if we want to replace bubble content at end)
    await instance.messaging.addMessageChunk({
      complete_item: {
        response_type: 'text',
        text: fullText,
        streaming_metadata: {
          id: itemId,
          stream_stopped: false,
        },
      },
      streaming_metadata: {
        response_id: responseId,
      },
      partial_response: {
        message_options: {
          response_user_profile: ResponseUserProfile,
        },
      },
    });

    // get docs out of context_response
    const docs = context_response.data?.documents || [];

    const responseBlocks = [
      {
        response_type: 'text',
        text: fullText,
        streaming_metadata: {
          id: itemId,
          stream_stopped: false,
        },
      },
    ];

    if (docs.length > 0) {
      responseBlocks.push({
        response_type: 'user_defined',
        user_defined: {
          user_defined_type: 'reference_docs_button',
          docs,
          original_text: fullText,
          button_label: 'Get reference documents',
        },
      });
    }

    // Final response (wraps the message in final format)
    await instance.messaging.addMessageChunk({
      final_response: {
        id: responseId,
        output: {
          generic: responseBlocks,
        },
        message_options: {
          response_user_profile: ResponseUserProfile,
        },
      },
    });
  } catch (err) {
    instance.updateIsMessageLoadingCounter('decrease');

    let errorMessage = 'Error occurred during active stream.';

    if (err.status === 429) {
      errorMessage = '⚠️ Server busy. Try again shortly.';
    }

    await instance.messaging.addMessageChunk({
      final_response: {
        id: responseId,
        output: {
          generic: [
            {
              response_type: 'text',
              text: errorMessage,
              streaming_metadata: {
                id: itemId,
                stream_stopped: true,
              },
            },
          ],
        },
        message_options: {
          response_user_profile: ResponseUserProfile,
        },
      },
    });
  } finally {
    _options.signal?.removeEventListener('abort', abortHandler);
  }
}

export { customSendMessage };
