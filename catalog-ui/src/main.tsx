import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import "@carbon/styles/css/styles.css";
import "./index.scss";

import App from "./App";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
