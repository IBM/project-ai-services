import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { Theme } from "@carbon/react";

import "@carbon/styles/css/styles.css";
import "@carbon/ibm-products/css/index.css";
import "./index.scss";

import App from "./App";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Theme theme="g10">
      <div className="appLayout">
        <App />
      </div>
    </Theme>
  </StrictMode>,
);
