import { useMemo } from 'react';
import { useToast } from '@spatika/react';
import { describeError } from './errors';

// Toasts for the outcome of an action. Failures say what failed and what to do; a repeated
// failure (a polling loop) can pass an `id` so it collapses into one toast.
export function useNotify() {
  const { toast } = useToast();
  return useMemo(() => ({
    success: (title, description) => toast({ title, description, tone: 'success' }),
    info: (title, description) => toast({ title, description }),
    error: (action, error, { id } = {}) => toast({ id, title: describeError(action, error), tone: 'danger' }),
  }), [toast]);
}
