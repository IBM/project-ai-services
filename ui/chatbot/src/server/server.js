import axios from 'axios';
import express, { json } from 'express';
import {
  getChatbotConfig,
  getServerPort,
  getTargetURL,
} from '../config/chatbotConfig.js';

const app = express();
const PORT = getServerPort();

app.use(json());

// Proxy endpoint
app.post('/v1/chat/completions', async (req, res) => {
  const targetURL = getTargetURL();
  console.log(`Forwarding request to: ${targetURL}`);
  try {
    const upstreamResponse = await axios({
      method: 'post',
      url: `${targetURL}/v1/chat/completions`,
      headers: { 'Content-Type': 'application/json' },
      responseType: 'stream',
      data: JSON.stringify(req.body),
    });

    res.setHeader('Content-Type', 'text/event-stream');
    res.setHeader('Cache-Control', 'no-cache');
    res.setHeader('Connection', 'keep-alive');

    // Pipe the stream directly from upstream to browser
    upstreamResponse.data.pipe(res);

    // to handle errors
    upstreamResponse.data.on('error', (err) => {
      console.error('Stream error:', err);
      res.end();
    });
  } catch (error) {
    console.error('OpenAI API Error:', error.message);
    res
      .status(error.response.status)
      .json({ error: 'Failed to fetch response from model API' });
  }
});

app.post('/v1/similarity-search', async (req, res) => {
  const { query } = req.body;
  const targetURL = getTargetURL();

  // Get chatbot configuration using shared utility
  const { searchMode: mode, topK, rerank } = getChatbotConfig();

  console.log(
    `Forwarding request to: ${targetURL}, with message: ${query}, mode: ${mode}, top_k: ${topK}, rerank: ${rerank}`,
  );

  try {
    const response = await axios.post(
      `${targetURL}/v1/similarity-search`,
      {
        query: query,
        mode: mode,
        top_k: topK,
        rerank: rerank,
      },
      {
        headers: { 'Content-Type': 'application/json' },
      },
    );

    res.json(response.data);
  } catch (error) {
    console.error('OpenAI API Error:', error.message);
    res
      .status(error.response.status)
      .json({ error: error.response.data.error });
  }
});

app.get('/db-status', async (req, res) => {
  const targetURL = getTargetURL();
  console.log(`Checking DB status at: ${targetURL}`);

  try {
    const response = await axios.get(`${targetURL}/db-status`, {
      headers: { 'Content-Type': 'application/json' },
    });

    res.json(response.data); // return server response to React client
  } catch (error) {
    console.error('DB Status Check Error:', error.message);

    // If backend is unreachable, returning false instead of crashing
    res.status(200).json({ status: false });
  }
});

app.get('/config', (req, res) => {
  // Return chatbot configuration using shared utility
  const { searchMode, topK, rerank } = getChatbotConfig();
  const config = {
    searchMode,
    numChunksPostReranker: String(topK),
    rerank: String(rerank),
  };
  console.log('Returning config:', config);
  res.json(config);
});

app.listen(PORT, () => {
  console.log(`Server listening on port ${PORT}`);
});
