import axios from "axios";
import { UserType } from "@carbon/ai-chat";

async function customSendMessage(request, _options, instance) {
  const userInput = request.input.text;

  const ResponseUserProfile = {
    id: "assistant",
    nickname: "DocuAgent",
    user_type: UserType.BOT,
  }

  function finalizeResponse(fullText) {
    let trimmed = fullText.trim(); // to remove trailing white-space
    // Define acceptable sentence-ending punctuation (both Hindi + English)
    const validEndings = ["।", ".", "?", "!", "…"]; // also includes ellipsis itself
    const lastChar = trimmed.charAt(trimmed.length - 1);
    if (!validEndings.includes(lastChar)) {
      trimmed += " ...";
    }
    return trimmed;
  }

  if (userInput === '') {
    if (instance.messaging && instance.messaging.addMessage && typeof instance.messaging.addMessage === 'function') {
      // sendWelcomeMessage(instance);
      return;
    }
  }
  const responseId = String(Date.now()); // or any unique ID
  const itemId = "1"; // single item per response, or generate if multiple

  //Adding initial partial chunk (this triggers the bubble with "stop streaming" button)
  await instance.messaging.addMessageChunk({
    partial_item: {
      response_type: "text",
      text: "", // start empty
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

  try {

    instance.updateIsLoadingCounter('increase');

    const response = await fetch("/stream", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ prompt: userInput }),
    });

    const context_response = await axios.post('/reference',  {
      prompt: userInput,
      headers: {
        "Content-Type": "application/json",
      },
    });

    instance.updateIsLoadingCounter('decrease');
    const reader = response.body.getReader();
    const decoder = new TextDecoder("utf-8");

    let fullText = ""; // to accumulate final message
    let done = false;

    while (!done) {
      const { value, done: doneReading } = await reader.read();
      done = doneReading;

      if (value) {
        const chunk = decoder.decode(value);
        fullText += chunk;

        // Send each streamed partial item chunk
        await instance.messaging.addMessageChunk({
          partial_item: {
            response_type: "text",
            text: chunk,
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
        response_type: "text",
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

    // Final response (wraps the message in final format)
    await instance.messaging.addMessageChunk({
      final_response: {
        id: responseId,
        output: {
          generic: [
            {
              response_type: "text",
              text: fullText,
              streaming_metadata: {
                id: itemId,
                stream_stopped: false,
              },
            },
            {
              response_type: "user_defined",
              user_defined: {
                user_defined_type: "reference_docs_button",
                docs,
                original_text: fullText,
                button_label: "Get reference documents",
            },
            },
          ],
        },
        message_options: {
          response_user_profile: ResponseUserProfile
        },
      },
    });

  } catch (err) {
    console.error("Error during streaming:", err);

    await instance.messaging.addMessageChunk({
      final_response: {
        id: responseId,
        output: {
          generic: [
            {
              response_type: "text",
              text: "Sorry, something went wrong during streaming.",
              streaming_metadata: {
                id: itemId,
                stream_stopped: true,
              },
            },
          ],
        },
        message_options: {
          response_user_profile: ResponseUserProfile
        },
      },
    });

  }
}

export { customSendMessage };
