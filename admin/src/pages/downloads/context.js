import { createContext, useContext } from 'react';

// The live Downloads feed ({ snap, error, reload, torrents, openCategory }), provided by DownloadsApp.
export const DownloadsContext = createContext(null);
export const useDownloads = () => useContext(DownloadsContext);
