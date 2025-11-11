import React from "react";
import ReactDOM from "react-dom/client";
import {BrowserRouter} from "react-router-dom" 
import "./index.scss";
import App from "./components/App.jsx";
import { GlobalTheme } from "@carbon/react";

const root = ReactDOM.createRoot(document.getElementById('root'));
root.render(
  <React.StrictMode>
    <GlobalTheme theme={"g100"}>
      <BrowserRouter forceRefresh={false}>
        <App />
      </BrowserRouter>
    </GlobalTheme>
  </React.StrictMode>
);
