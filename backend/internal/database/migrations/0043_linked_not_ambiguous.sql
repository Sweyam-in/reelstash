-- A performer or studio linked to at least one provider is matched; an earlier
-- run could leave it flagged "choose match" because another provider had
-- several candidates.
UPDATE performer SET match_status = 'matched'
WHERE match_status IN ('ambiguous', 'unmatched', 'no_match')
  AND EXISTS (SELECT 1 FROM match_link l WHERE l.kind = 'performer' AND l.entity_id = performer.id);
UPDATE studio SET match_status = 'matched'
WHERE match_status IN ('ambiguous', 'unmatched', 'no_match')
  AND EXISTS (SELECT 1 FROM match_link l WHERE l.kind = 'studio' AND l.entity_id = studio.id);
