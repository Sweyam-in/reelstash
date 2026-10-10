import { useToast } from '@spatika/react';

// Surfaces a caught API error as a danger toast, admin-wide.
// Pass `id` to collapse repeated failures (e.g. a polling loop) into one toast
// instead of stacking a new one on every retry.
export function useErrorToast() {
  const { toast } = useToast();
  return (err, title = 'Something went wrong', { id } = {}) => {
    toast({ id, title, description: err?.message || String(err), tone: 'danger' });
  };
}
