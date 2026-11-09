(() => {
  const menu = document.getElementById('docs-menu');
  const wide = matchMedia('(min-width: 60rem)');
  const setMenu = () => { menu.open = wide.matches; };
  setMenu();
  wide.addEventListener('change', setMenu);
  const theme = document.getElementById('theme');
  const renderTheme = () => { theme.textContent = `Theme: ${document.documentElement.dataset.theme || 'auto'}`; };
  renderTheme();
  theme.addEventListener('click', () => {
    const modes = ['auto', 'light', 'dark'];
    const next = modes[(modes.indexOf(document.documentElement.dataset.theme || 'auto') + 1) % modes.length];
    if (next === 'auto') delete document.documentElement.dataset.theme;
    else document.documentElement.dataset.theme = next;
    try { if (next === 'auto') localStorage.removeItem('melodix-theme'); else localStorage.setItem('melodix-theme', next); } catch {}
    renderTheme();
  });
  for (const pre of document.querySelectorAll('article pre')) {
    if (pre.querySelector('.language-mermaid') || pre.closest('.language-mermaid')) continue;
    const row = document.createElement('div');
    row.className = 'copy-row';
    const button = document.createElement('button');
    button.type = 'button';
    button.textContent = 'Copy code';
    row.append(button);
    pre.before(row);
    button.addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(pre.textContent);
        document.getElementById('copy-status').textContent = 'Code copied.';
      } catch { document.getElementById('copy-status').textContent = 'Copy failed. Select the code and copy it manually.'; }
    });
  }
  const diagrams = [...document.querySelectorAll('article .language-mermaid')].map(element => element.matches('code') ? element : element.querySelector('code')).filter(Boolean);
  if (diagrams.length) {
    import('https://cdn.jsdelivr.net/npm/mermaid@12.3.0/dist/mermaid.esm.min.mjs').then(async ({ default: mermaid }) => {
      mermaid.initialize({ startOnLoad: false, securityLevel: 'strict', theme: 'neutral' });
      for (const [index, code] of diagrams.entries()) {
        const { svg } = await mermaid.render(`diagram-${index}`, code.textContent);
        const figure = document.createElement('figure');
        figure.className = 'diagram';
        figure.innerHTML = svg;
        const caption = document.createElement('figcaption');
        caption.textContent = 'Playback architecture. The surrounding text explains the flow.';
        figure.append(caption);
        const pre = code.closest('pre');
        pre.before(figure);
        pre.hidden = true;
      }
    }).catch(() => { /* Keep the source and surrounding explanation when diagrams cannot load. */ });
  }
})();
