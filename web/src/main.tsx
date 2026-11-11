import React from 'react';
import ReactDOM from 'react-dom/client';
import App from './App';
import { isDesktop, loadDesktopPreferences } from './preferences';
import './index.css';

function renderApp() {
  ReactDOM.createRoot(document.getElementById('root')!).render(
    <React.StrictMode>
      <App />
    </React.StrictMode>,
  );
}

if (isDesktop) {
  void loadDesktopPreferences().then(renderApp);
} else {
  renderApp();
}
