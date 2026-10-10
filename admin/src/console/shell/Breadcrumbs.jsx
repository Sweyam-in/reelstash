import React, { createContext, useContext, useEffect, useMemo, useState } from 'react';

// Lets a detail page add its own trailing crumb ("Users / Asha") to the top bar.
const CrumbContext = createContext({ crumb: null, setCrumb: () => {} });

export function CrumbProvider({ children }) {
  const [crumb, setCrumb] = useState(null);
  const value = useMemo(() => ({ crumb, setCrumb }), [crumb]);
  return <CrumbContext.Provider value={value}>{children}</CrumbContext.Provider>;
}

export const useCrumbValue = () => useContext(CrumbContext).crumb;

export function useCrumb(label) {
  const { setCrumb } = useContext(CrumbContext);
  useEffect(() => {
    setCrumb(label ?? null);
    return () => setCrumb(null);
  }, [label, setCrumb]);
}
