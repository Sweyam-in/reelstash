import React from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import { ThemeScope, Toaster } from './ui';
import { LinkDevicePage } from './components/Devices';
import './styles.css';

// The standalone "sign in a TV" page (preview-link.html).
createRoot(document.getElementById('root')).render(<ThemeScope theme="dark" className="rs-standalone"><Toaster><BrowserRouter><LinkDevicePage /></BrowserRouter></Toaster></ThemeScope>);
