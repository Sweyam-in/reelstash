import React, { createContext, useContext } from 'react';

const SessionContext = createContext(null);
export const SessionProvider = ({ session, children }) => <SessionContext.Provider value={session}>{children}</SessionContext.Provider>;
export const useSession = () => useContext(SessionContext) || {};
