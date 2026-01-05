import axios from 'axios';
import express, { json } from 'express';

const app = express();
const PORT = process.env.PORT || 3001;

app.use(json());

// Proxy endpoint
app.post('/v1/chat/completions', async (req, res) => {
  const targetURL = process.env.TARGET_URL;
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

app.post('/reference', async (req, res) => {
  const { prompt } = req.body;
  const targetURL = process.env.TARGET_URL;
  console.log(`Forwarding request to: ${targetURL}, with message: ${prompt}`);

  try {
    const response = await axios.post(
      `${targetURL}/reference`,
      {
        prompt: prompt,
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
  const targetURL = process.env.TARGET_URL;
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

app.listen(PORT, () => {
  console.log(`Server listening on port ${PORT}`);
});
