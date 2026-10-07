package web

import _ "embed"

// A quiet note workspace: the document, not a simulated sheet of paper, is the surface.
const markdownEditorStyles = `:root {
  color-scheme: light;
  --md-ink: #29292e;
  --md-muted: #696970;
  --md-line: #e6e6e9;
  --md-chrome: #f6f6f7;
  --md-paper: #ffffff;
  --md-soft: #eeeef1;
  --md-accent: #7055bb;
  --md-code-ink: #975430;
  --md-selection: #b6a0e54d;
  --md-inset: max(2rem, calc((100% - 70rem) / 2));
}
body {
  display: flex; flex-direction: column; height: 100vh; height: 100dvh;
  overflow: hidden; color: var(--md-ink); background: var(--md-paper);
  font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
}
.artifactd-navigation { display: none; }
.artifactd-md-shell { flex: none; z-index: 20; border-bottom: 1px solid var(--md-line); background: var(--md-chrome); }
.artifactd-md-bar { display: flex; align-items: center; gap: 1rem; min-height: 49px; padding: .45rem 1rem; }
.artifactd-md-breadcrumb { display: flex; align-items: center; gap: .75rem; min-width: 0; flex: 1; }
.artifactd-md-back { display: grid; place-items: center; flex: none; width: 30px; height: 30px; border-radius: 5px; color: var(--md-muted); }
.artifactd-md-back:hover { background: var(--md-soft); color: var(--md-ink); }
.artifactd-md-back svg, .artifactd-md-toggle svg { width: 16px; height: 16px; fill: none; stroke: currentColor; stroke-width: 1.6; stroke-linecap: round; stroke-linejoin: round; }
.artifactd-md-filename { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; font-weight: 500; }
.artifactd-md-actions { display: flex; align-items: center; gap: .35rem; flex: none; }
.artifactd-md-shell button, .artifactd-md-export summary, .artifactd-md-export-panel a {
  display: inline-flex; align-items: center; justify-content: center; gap: .45rem;
  min-height: 30px; padding: .3rem .65rem; border: 1px solid transparent; border-radius: 5px;
  color: var(--md-muted); background: transparent; font: 500 12px/1.5 ui-sans-serif, system-ui, sans-serif;
  text-decoration: none; white-space: nowrap; cursor: pointer;
}
.artifactd-md-shell button:hover:not(:disabled), .artifactd-md-export summary:hover, .artifactd-md-export-panel a:hover {
  color: var(--md-ink); background: var(--md-soft);
}
.artifactd-md-shell button:disabled { opacity: .45; cursor: default; }
.artifactd-md-shell :is(a, button, summary):focus-visible { outline: 2px solid var(--md-accent); outline-offset: 2px; }
.artifactd-md-actions .artifactd-md-toggle { color: var(--md-ink); border-color: var(--md-line); background: var(--md-paper); }
.artifactd-md-toggle .artifactd-md-preview-icon { display: none; }
body.artifactd-md-editing .artifactd-md-toggle .artifactd-md-preview-icon { display: block; }
body.artifactd-md-editing .artifactd-md-toggle .artifactd-md-edit-icon { display: none; }
.artifactd-md-actions .artifactd-md-save:not(:disabled) { color: var(--md-accent); background: var(--md-soft); }
.artifactd-md-export { position: relative; }
.artifactd-md-export summary { list-style: none; }
.artifactd-md-export summary::-webkit-details-marker { display: none; }
.artifactd-md-export summary svg { width: 12px; height: 12px; fill: none; stroke: currentColor; stroke-width: 1.5; }
.artifactd-md-export[open] summary svg { transform: rotate(180deg); }
.artifactd-md-export-panel {
  position: absolute; right: 0; top: calc(100% + .6rem); width: min(280px, calc(100vw - 2rem));
  padding: .45rem; border: 1px solid var(--md-line); border-radius: 7px;
  background: var(--md-paper); box-shadow: 0 6px 24px #00000018;
}
.artifactd-md-menu-label { display: block; padding: .5rem .6rem; font-size: 11px; color: var(--md-muted); }
.artifactd-md-export-panel button, .artifactd-md-export-panel a { justify-content: flex-start; width: 100%; padding: .6rem; text-align: left; white-space: normal; }
.artifactd-md-export-panel strong, .artifactd-md-export-panel small { display: block; }
.artifactd-md-export-panel strong { font-weight: 500; color: var(--md-ink); }
.artifactd-md-export-panel small { margin-top: 2px; font-size: 11px; }
.artifactd-md-format { flex: none; color: var(--md-accent); font: 600 10px/1 ui-monospace, monospace; width: 35px; }
.artifactd-md-export-panel p { margin: .5rem .6rem; padding-top: .6rem; border-top: 1px solid var(--md-line); color: var(--md-muted); font: 11px/1.5 system-ui, sans-serif; }
.artifactd-md-workspace { flex: 1; min-height: 0; min-width: 0; }
.artifactd-markdown {
  width: 100%; height: 100%; margin: 0; padding: 2.5rem var(--md-inset) 5rem;
  overflow: auto; overflow-wrap: anywhere; scrollbar-gutter: stable;
  color: var(--md-ink); background: var(--md-paper); font-size: 16px; line-height: 1.7;
}
.artifactd-markdown h1 { font-family: inherit; font-size: 2rem; font-weight: 650; line-height: 1.3; letter-spacing: -.025em; margin: 0 0 1rem; }
.artifactd-markdown h2 { font-size: 1.5rem; font-weight: 600; border: 0; padding: 0; }
.artifactd-markdown h3 { font-size: 1.2rem; font-weight: 600; }
.artifactd-markdown :is(h2, h3, h4, h5, h6) { margin: 1.5em 0 .6em; scroll-margin-top: 1rem; }
.artifactd-markdown a { color: var(--md-accent); }
.artifactd-markdown li { margin: .25rem 0; }
.artifactd-markdown blockquote { border-left: 2px solid var(--md-accent); padding: .1rem 1.25rem; margin: 1.25rem 0; color: var(--md-muted); }
.artifactd-markdown blockquote p { margin: .4rem 0; }
.artifactd-markdown pre { border: 0; border-radius: 5px; background: var(--md-chrome); font-size: 14px; line-height: 1.6; }
.artifactd-markdown code { color: var(--md-code-ink); background: var(--md-soft); }
.artifactd-markdown pre code { color: inherit; background: transparent; }
.artifactd-markdown table { display: block; width: 100%; overflow-x: auto; overflow-wrap: normal; font-size: 14px; }
.artifactd-markdown :is(th, td) { border-color: var(--md-line); }
.artifactd-markdown th { background: var(--md-chrome); }
.artifactd-markdown hr { border-color: var(--md-line); }
.artifactd-markdown > :last-child { margin-bottom: 0; }
.artifactd-md-editor-wrap { display: none; height: 100%; min-width: 0; flex-direction: column; }
.artifactd-md-editor-label { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
.artifactd-md-editor[hidden] { display: none; }
.artifactd-md-code-editor { flex: 1; min-height: 0; }
.artifactd-md-code-editor .cm-editor { height: 100%; }
.artifactd-md-code-editor .cm-editor.cm-focused { outline: none; }
.artifactd-md-code-editor .cm-scroller { overflow: auto; scrollbar-gutter: stable; padding-inline: var(--md-inset); font: 16px/1.7 ui-sans-serif, system-ui, sans-serif; }
.artifactd-md-code-editor .cm-content { min-width: 0; padding: 2.5rem 0 5rem; }
.artifactd-md-code-editor .cm-line { padding: 0; }
.artifactd-md-code-editor .cm-search { font: 12px/1.5 ui-sans-serif, system-ui, sans-serif; padding: .5rem 1rem; }
.artifactd-md-code-editor .cm-search input { max-width: 100%; }
.artifactd-md-editor { width: 100%; flex: 1; padding: 2.5rem var(--md-inset); border: 0; background: var(--md-paper); color: var(--md-ink); font: 16px/1.7 ui-monospace, monospace; resize: none; }
.artifactd-md-context { display: flex; align-items: center; justify-content: space-between; gap: 1rem; flex: none; min-height: 29px; padding: .3rem 1rem; border-top: 1px solid var(--md-line); background: var(--md-chrome); color: var(--md-muted); font: 11px/1.5 system-ui, sans-serif; }
.artifactd-md-status { min-width: 0; overflow-wrap: anywhere; }
.artifactd-md-status[data-state='unsaved'] { color: #876315; }
.artifactd-md-status[data-state='error'] { color: #b03838; }
.artifactd-md-context > span { flex: none; }
.artifactd-md-editing-hint { display: none; }
body.artifactd-md-editing .artifactd-markdown { display: none; }
body.artifactd-md-editing .artifactd-md-editor-wrap { display: flex; }
body.artifactd-md-editing .artifactd-md-editing-hint { display: inline; }
@media (max-width: 600px) {
  :root { --md-inset: 1.25rem; }
  .artifactd-md-bar { flex-wrap: wrap; gap: .25rem; padding: .4rem .65rem; }
  .artifactd-md-breadcrumb { flex-basis: 100%; gap: .5rem; }
  .artifactd-md-actions { margin-left: auto; }
  .artifactd-md-shell button, .artifactd-md-export summary { min-height: 36px; }
  .artifactd-markdown { padding-top: 1.5rem; }
  .artifactd-md-code-editor .cm-content { padding-top: 1.5rem; }
  .artifactd-md-context { padding-inline: .8rem; }
  .artifactd-md-editing-hint { display: none !important; }
}
@media (prefers-color-scheme: dark) {
  :root {
    color-scheme: dark; --md-ink: #dadadd; --md-muted: #a6a6af; --md-line: #363639;
    --md-chrome: #202022; --md-paper: #262628; --md-soft: #333337;
    --md-accent: #b29ae8; --md-code-ink: #d9ae88; --md-selection: #9674cf55;
  }
  .artifactd-md-status[data-state='unsaved'] { color: #dfba76; }
  .artifactd-md-status[data-state='error'] { color: #efa3a3; }
}
@media print {
  :root { color-scheme: light; --md-ink: #222; --md-muted: #555; --md-line: #ddd; --md-paper: white; --md-chrome: #f8f8f8; --md-soft: #f4f4f4; --md-accent: #6543a3; }
  body { display: block; height: auto; overflow: visible; background: white; }
  .artifactd-md-shell, .artifactd-md-editor-wrap, .artifactd-md-context, .artifactd-navigation { display: none !important; }
  .artifactd-md-workspace { display: block; width: 100%; height: auto; margin: 0; }
  .artifactd-markdown { display: block !important; width: 100%; height: auto; overflow: visible; margin: 0; padding: 0; }
  .artifactd-markdown h1, .artifactd-markdown h2, .artifactd-markdown h3 { break-after: avoid; }
  .artifactd-markdown pre { white-space: pre-wrap; overflow-wrap: anywhere; }
  .artifactd-markdown table { display: table; }
}
`

//go:embed assets/markdown-editor.js
var markdownEditorScript string
