const express = require('express');
const axios = require('axios');
const cors = require('cors');

const app = express();
const PORT = process.env.PORT || 3001;

app.use(cors());
app.use(express.json());

// Proxy endpoint
app.post('/generate', async (req, res) => {
  const { prompt } = req.body;
  const targetURL = process.env.TARGET_URL
  console.log(targetURL);
  console.log(`Forwarding request to: ${targetURL}, with message: ${prompt}`);

  try {
      const response = await axios.post(`${targetURL}/generate`, {
        prompt: prompt,
        temperature: 0.2,
    }, {
      headers: { 'Content-Type': 'application/json' }
    });

    res.json(response.data);
  } catch (error) {
    console.error('OpenAI API Error:', error.message);
    res.status(500).json({ error: 'Failed to fetch response from model API' });
  }
});




app.post('/stream', async (req, res) => {
  const { prompt } = req.body;
  const targetURL = process.env.TARGET_URL
  console.log(`Forwarding request to: ${targetURL}, with message: ${prompt}`);
  try {
      const upstreamResponse = await axios({
      method: 'post',
      url: `${targetURL}/stream`,
      data: { prompt },
      responseType: 'stream',
      headers: { 'Content-Type': 'application/json' }
    });

    res.setHeader('Content-Type', 'text/event-stream');
    res.setHeader('Cache-Control', 'no-cache');
    res.setHeader('Connection', 'keep-alive');
    res.setHeader('Access-Control-Allow-Origin', '*');

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
