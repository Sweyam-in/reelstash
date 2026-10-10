import React, { useState } from 'react';
import { getAppearance, updateAppearance } from '../../api';
import { COLOR_THEMES, colorThemeById } from '../../colorThemes';
import { ErrorState, PageHeader, PageStack, Panel, SkeletonRows } from '../components';
import { useConfirm } from '../components';
import { useNotify } from '../lib/notify';
import { useResource } from '../lib/useResource';

export function AppearancePage() {
  const notify = useNotify();
  const confirm = useConfirm();
  const theme = useResource(() => getAppearance().then(d => colorThemeById(d.colorTheme).id), []);
  const [saving, setSaving] = useState(false);
  const { data: current, setData } = theme;

  async function choose(id) {
    if (id === current || saving) return;
    const ok = await confirm({ title: `Use ${colorThemeById(id).name} for the library?`, consequence: 'Play buttons, progress bars and highlights change colour for everyone who uses the library app. Light and dark stay a personal choice.', affects: 'Everyone who uses ReelStash', reversible: true, confirmLabel: `Use ${colorThemeById(id).name}` });
    if (!ok) return;
    const previous = current;
    setData(id);
    setSaving(true);
    try { await updateAppearance(id); notify.success(`${colorThemeById(id).name} is now the library theme`, 'Open the library to see it.'); }
    catch (err) { setData(previous); notify.error('save the theme', err); }
    finally { setSaving(false); }
  }

  return <PageStack>
    <PageHeader title="Appearance" subtitle="The colour theme of the library app. Each person still chooses light or dark for themselves. This console’s own theme is in the top bar." />
    <Panel title="Library colour theme" subtitle="One look for play buttons, progress and highlights across the library.">
      {theme.error && !current ? <ErrorState title="Couldn't load the library theme" error={theme.error} onRetry={theme.reload} /> : current == null ? <SkeletonRows rows={2} />
        : <div className="console-theme-grid" role="radiogroup" aria-label="Library colour theme">
          {COLOR_THEMES.map(t => <button key={t.id} type="button" role="radio" aria-checked={current === t.id} className="console-theme-card" disabled={saving} onClick={() => choose(t.id)}>
            <span className="console-theme-card__swatch" style={{ background: `linear-gradient(120deg, ${t.from} 0%, ${t.to} 100%)` }} />
            <span className="console-theme-card__dots" aria-hidden="true">{t.tones.map(c => <i key={c} style={{ background: c }} />)}</span>
            <strong>{t.name}{current === t.id && <span className="console-theme-card__current"> · In use</span>}</strong>
            <span className="console-muted console-small">{t.blurb}</span>
          </button>)}
        </div>}
    </Panel>
  </PageStack>;
}
