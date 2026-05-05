import { useEffect, useState } from 'react';
import { InlineNotification, Modal, TextInput } from '@carbon/react';
import './ApiKeyDialog.scss';

/**
 * API Key Dialog Component
 * Prompts user for vLLM API key and validates it before allowing chat access
 */
export function ApiKeyDialog({ isOpen, onApiKeyValidated }) {
  const [apiKey, setApiKey] = useState('');
  const [isValidating, setIsValidating] = useState(false);
  const [error, setError] = useState(null);
  const [showDialog, setShowDialog] = useState(isOpen);

  useEffect(() => {
    setShowDialog(isOpen);
  }, [isOpen]);

  const handleSubmit = async () => {
    if (!apiKey.trim()) {
      setError('Please enter an API key');
      return;
    }

    setIsValidating(true);
    setError(null);

    try {
      const response = await fetch('/v1/models', {
        method: 'GET',
        headers: {
          Authorization: `Bearer ${apiKey.trim()}`,
        },
      });

      if (response.ok) {
        setShowDialog(false);
        onApiKeyValidated(apiKey.trim());
      } else {
        let errorMessage = 'Invalid API key';
        try {
          const errorData = await response.json();
          errorMessage = errorData?.error?.message || errorMessage;
        } catch {
          // Ignore non-JSON error responses and keep fallback message
        }
        setError(errorMessage);
      }
    } catch (err) {
      setError(
        'Failed to validate API key. Please check your connection and try again.',
      );
      console.error('API key validation error:', err);
    } finally {
      setIsValidating(false);
    }
  };

  const handleKeyPress = (e) => {
    if (e.key === 'Enter' && !isValidating) {
      handleSubmit();
    }
  };

  return (
    <Modal
      open={showDialog}
      modalHeading="API Key Required"
      primaryButtonText={isValidating ? 'Validating...' : 'Submit'}
      secondaryButtonText="Cancel"
      onRequestSubmit={handleSubmit}
      onRequestClose={() => {
        // Prevent closing without valid key
        if (!isValidating) {
          setError('API key is required to use the chatbot');
        }
      }}
      primaryButtonDisabled={isValidating || !apiKey.trim()}
      preventCloseOnClickOutside={true}
      size="sm"
    >
      <div className="api-key-dialog-content">
        <p className="api-key-dialog-description">
          Please enter your vLLM API key to access the chatbot. You will need to
          re-enter your key each time you refresh the page.
        </p>

        {error && (
          <InlineNotification
            kind="error"
            title="Error"
            subtitle={error}
            lowContrast
            hideCloseButton
            className="api-key-error"
          />
        )}

        <TextInput
          id="api-key-input"
          labelText="API Key"
          placeholder="Enter your vLLM API key"
          value={apiKey}
          onChange={(e) => {
            setApiKey(e.target.value);
            setError(null);
          }}
          onKeyPress={handleKeyPress}
          disabled={isValidating}
          type="password"
          autoFocus
        />
      </div>
    </Modal>
  );
}

export default ApiKeyDialog;

// Made with Bob
