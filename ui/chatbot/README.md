# Starting out with the project

getup the retreival and generate answer service by running

python flask_retrieval.py
and python generate_answer.py

docker build -f Dockerfile -t rag-frontend .

docker run --network="host" -p 3000:3000 rag-frontend:latest


# Getting Started with Vite and React

This project was built with Vite and uses @vitejs/plugin-react for a fast development experience.

## Available Scripts

In the project directory, you can run:

### `npm run dev`

Runs the app in development mode using Vite's development server.\
Open http://localhost:3000 to view it in your browser.

The page will update instantly as you edit your code with Hot Module Replacement (HMR).\

### `npm run build`

Builds the app for production to the build folder. \
It bundles React and your other assets into optimized, minified files for the best performance. \
The build output is ready to be deployed.

### `npm run preview`
Serves the production build locally.
After running npm run build, use this command to test your production output before deploying.
Open http://localhost:3010 to view the production build.
