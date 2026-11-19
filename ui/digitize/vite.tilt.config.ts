// Vite config used by Tiltfile.digitize for local development.
// It reuses vite.config.ts and adds the proxy that nginx provides in the container image:
// the browser posts the authorization code to /auth/exchange on the digitize origin and the
// request is forwarded to the catalog's code-exchange endpoint.
import { defineConfig, type UserConfig } from "vite";
import baseConfig from "./vite.config.ts";

const catalogApiUrl = process.env.CATALOG_API_URL || "http://localhost:8080";

export default defineConfig((env) => {
  const base = baseConfig(env) as UserConfig;
  return {
    ...base,
    server: {
      ...base.server,
      proxy: {
        ...base.server?.proxy,
        "/auth/exchange": {
          target: catalogApiUrl,
          changeOrigin: true,
          rewrite: () => "/api/v1/auth/code/exchange",
        },
      },
    },
  };
});
