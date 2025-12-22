import React, { useEffect, useState } from 'react';
import axios from 'axios';

const FeedbackWidget = ({ userInput, aiResponse, projectId }) => {
  const [iframeUrl, setIframeUrl] = useState('');

  useEffect(() => {
    const init = async () => {
      const res = await axios.get('/feedback-token');
      const token = res.data.token;
      if (token) {
        const baseUrl = '/feedback.html';

        const params = new URLSearchParams({
          auth_token: token,
          project_id: projectId,
          question: userInput,
          answer: aiResponse,
        });

        setIframeUrl(`${baseUrl}?${params.toString()}`);
      }
    };
    init();
  }, [userInput, aiResponse, projectId]);

  if (!iframeUrl) return null;

  return (
    <div id="widget-container" style={{ width: '100%' }}>
      <iframe
        src={iframeUrl}
        sandbox="allow-scripts allow-forms allow-same-origin"
        width="100%"
        title="Feedback"
      />
    </div>
  );
};

export default FeedbackWidget;
