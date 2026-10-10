import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Dices } from 'lucide-react';
import { Button, useToast } from '@spatika/react';
import { fetchRandomTitles } from '../queueActions';

// Header button that opens one random title: a low-effort way in when you don't know what you want.
export default function SurpriseButton() {
  const navigate = useNavigate();
  const { toast } = useToast();
  const [busy, setBusy] = useState(false);
  async function surprise() {
    setBusy(true);
    try {
      const [pick] = await fetchRandomTitles({}, 1);
      if (pick) navigate(`/library/${pick.id}`, { state: { from: '/' } });
      else toast({ title: 'Nothing to pick from yet' });
    } catch (err) { toast({ title: 'Could not pick a title', description: err.message, tone: 'danger' }); }
    finally { setBusy(false); }
  }
  return <Button variant="ghost" size="icon-sm" aria-label="Surprise me" title="Surprise me" disabled={busy} onClick={surprise}><Dices size={18} className={busy ? 'rs-spin' : undefined} /></Button>;
}
