import http from 'http';
import ncmutils from 'ncm-audio-recognize';

const PORT = process.env.PORT || 3737;

const server = http.createServer(async (req, res) => {
  res.setHeader('Content-Type', 'application/json');
  res.setHeader('Access-Control-Allow-Origin', '*');

  if (req.method === 'OPTIONS') {
    res.writeHead(200);
    res.end();
    return;
  }

  if (req.method === 'POST' && req.url === '/recognize') {
    let body = [];
    
    req.on('data', chunk => {
      body.push(chunk);
    });

    req.on('end', async () => {
      try {
        const buffer = Buffer.concat(body);
        
        if (buffer.length === 0) {
          res.writeHead(400);
          res.end(JSON.stringify({ code: -1, message: 'Empty audio data' }));
          return;
        }

        const encoded = await ncmutils.encode(buffer);
        const result = await ncmutils.recognize(encoded);

        if (!result || result.length === 0) {
          res.writeHead(200);
          res.end(JSON.stringify({ 
            code: 200, 
            data: { result: [] } 
          }));
          return;
        }

        res.writeHead(200);
        res.end(JSON.stringify({
          code: 200,
          data: { result }
        }));
      } catch (error) {
        console.error('Recognition error:', error);
        res.writeHead(500);
        res.end(JSON.stringify({ 
          code: -1, 
          message: error.message || 'Recognition failed' 
        }));
      }
    });

    req.on('error', (error) => {
      console.error('Request error:', error);
      res.writeHead(500);
      res.end(JSON.stringify({ code: -1, message: 'Request error' }));
    });
  } else if (req.method === 'GET' && req.url === '/health') {
    res.writeHead(200);
    res.end(JSON.stringify({ status: 'ok' }));
  } else {
    res.writeHead(404);
    res.end(JSON.stringify({ code: -1, message: 'Not found' }));
  }
});

server.listen(PORT, '127.0.0.1', () => {
  console.log(`Audio recognition service running on http://127.0.0.1:${PORT}`);
});

const shutdown = () => {
  console.log('Shutting down gracefully...');
  server.close(() => {
    console.log('Server closed');
    process.exit(0);
  });
  
  setTimeout(() => {
    console.error('Shutdown timeout, forcing exit');
    process.exit(1);
  }, 3000);
};

process.on('SIGTERM', shutdown);
process.on('SIGINT', shutdown);
