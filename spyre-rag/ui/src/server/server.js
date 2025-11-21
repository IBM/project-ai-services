import express, { json } from 'express';
import axios from 'axios';

const app = express();
const PORT = process.env.PORT || 3001;

app.use(json());

// Proxy endpoint
app.post('/v1/chat/completions', async (req, res) => {
  const targetURL = process.env.TARGET_URL
  console.log(`Forwarding request to: ${targetURL}`);
  try {
      const upstreamResponse = await axios({
      method: 'post',
      url: `${targetURL}/v1/chat/completions`,
      headers: { 'Content-Type': 'application/json' },
      responseType: 'stream',
      data: JSON.stringify(req.body)
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
    res.status(500).json({ error: 'Failed to fetch response from model API' });
  }
});

app.post('/reference', async (req, res) => {
  const { prompt } = req.body;
  const targetURL = process.env.TARGET_URL
  console.log(`Forwarding request to: ${targetURL}, with message: ${prompt}`);

  try {
      const response = await axios.post(`${targetURL}/reference`, {
        prompt: prompt,
    }, {
      headers: { 'Content-Type': 'application/json' }
    });

    res.json(response.data);

  } catch (error) {
    console.error('OpenAI API Error:', error.message);
    res.status(500).json({ error: 'Failed to fetch response from model API' });
  }
});

app.listen(PORT, () => {
  console.log(`Server listening on port ${PORT}`);
});
