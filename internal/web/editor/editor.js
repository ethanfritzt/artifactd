import { EditorState, EditorSelection } from '@codemirror/state';
import { EditorView, keymap, drawSelection } from '@codemirror/view';
import { defaultKeymap, history, historyKeymap, indentWithTab, redo } from '@codemirror/commands';
import { markdown, markdownLanguage } from '@codemirror/lang-markdown';
import { syntaxHighlighting, HighlightStyle, indentUnit } from '@codemirror/language';
import { searchKeymap } from '@codemirror/search';
import { tags } from '@lezer/highlight';

const textarea = document.querySelector('.artifactd-md-editor');
const main = document.querySelector('.artifactd-markdown');
const shell = document.querySelector('.artifactd-md-shell');
const status = document.querySelector('.artifactd-md-status');

if (textarea && main && shell && status) initialize();

function initialize() {
  const save = shell.querySelector('[data-action="save"]');
  const toggleView = shell.querySelector('[data-action="toggle-view"]');
  const modeLabel = document.querySelector('.artifactd-md-mode');
  const exports = shell.querySelector('.artifactd-md-export');
  const exportToggle = exports.querySelector('summary');
  const parent = document.createElement('div');
  parent.className = 'artifactd-md-code-editor';
  textarea.before(parent);
  let saved = textarea.value;
  let draft = saved;
  let savedHTML = main.innerHTML;
  let renderedSource = saved;
  let mode = 'preview';
  let saving = false;
  let saveError = '';
  let previewError = '';
  let renderTimer;
  let renderController;
  let generation = 0;
  let printing = false;
  let printDraftHTML = '';
  let printScroll = 0;
  const scrollPositions = { preview: 0, edit: 0 };

  const view = new EditorView({
    parent,
    state: EditorState.create({
      doc: saved,
      extensions: [
        EditorView.cspNonce.of(shell.dataset.styleNonce),
        EditorView.contentAttributes.of({
          id: 'artifactd-md-source',
          'aria-labelledby': 'artifactd-md-source-label',
          spellcheck: 'false',
        }),
        drawSelection(),
        history(), indentUnit.of('  '), EditorState.tabSize.of(2), EditorView.lineWrapping,
        EditorView.domEventHandlers({
          beforeinput(event, view) {
            // Chromium can insert styled spans even for plain text in a highlighted
            // selection. Handle ordinary text through CodeMirror, keeping the CSP
            // strict; composition/paste keep CodeMirror's native handling.
            if (event.inputType !== 'insertText' || event.isComposing || event.data === null) return false;
            view.dispatch({ ...view.state.replaceSelection(event.data), scrollIntoView: true, userEvent: 'input.type' });
            return true;
          },
        }),
        markdown({ base: markdownLanguage, completeHTMLTags: false }),
        syntaxHighlighting(HighlightStyle.define([
          { tag: tags.heading1, fontSize: '2em', fontWeight: '650', lineHeight: '1.3' },
          { tag: tags.heading2, fontSize: '1.5em', fontWeight: '600', lineHeight: '1.4' },
          { tag: tags.heading3, fontSize: '1.2em', fontWeight: '600' },
          { tag: tags.heading, fontWeight: '600' },
          { tag: tags.emphasis, fontStyle: 'italic' },
          { tag: tags.strong, fontWeight: '700' },
          { tag: [tags.link, tags.url], color: 'var(--md-accent)', textDecoration: 'underline' },
          { tag: [tags.monospace, tags.string], color: 'var(--md-code-ink)', fontFamily: 'ui-monospace, SFMono-Regular, Consolas, monospace' },
          { tag: [tags.meta, tags.comment, tags.processingInstruction], color: 'var(--md-muted)' },
          { tag: tags.strikethrough, textDecoration: 'line-through' },
        ])),
        EditorView.theme({
          '&': { color: 'var(--md-ink)', backgroundColor: 'var(--md-paper)' },
          '.cm-content': { caretColor: 'var(--md-accent)' },
          '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--md-accent)' },
          '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection': {
            backgroundColor: 'var(--md-selection)',
          },
          '.cm-panels': { backgroundColor: 'var(--md-chrome)', color: 'var(--md-ink)' },
          '.cm-panels-top': { borderBottom: '1px solid var(--md-line)' },
          '.cm-textfield': { color: 'var(--md-ink)', backgroundColor: 'var(--md-paper)', border: '1px solid var(--md-line)' },
          '.cm-button': { color: 'var(--md-ink)', background: 'var(--md-soft)', border: '1px solid var(--md-line)' },
          '.cm-searchMatch': { backgroundColor: 'var(--md-selection)', outline: '1px solid var(--md-accent)' },
        }),
        keymap.of([
          { key: 'Mod-s', run: () => { void saveDocument(); return true; } },
          { key: 'Mod-e', run: () => { setMode('preview'); toggleView.focus(); return true; } },
          { key: 'Mod-b', run: () => format('bold') },
          { key: 'Mod-i', run: () => format('italic') },
          { key: 'Mod-Shift-z', run: redo, preventDefault: true },
          ...historyKeymap, ...searchKeymap, ...defaultKeymap, indentWithTab,
        ]),
        EditorView.updateListener.of((update) => {
          if (!update.docChanged) return;
          draft = update.state.doc.toString();
          saveError = '';
          previewError = '';
          scheduleRender();
          updateStatus();
        }),
      ],
    }),
  });

  // Hide the escaped source only after the enhanced editor initializes successfully.
  textarea.id = 'artifactd-md-source-fallback';
  textarea.hidden = true;

  const content = () => draft;
  const changed = () => content() !== saved;
  const showHTML = (html) => {
    if (printing) printDraftHTML = html;
    else main.innerHTML = html; // Only HTML returned by the daemon's safe Markdown renderer.
  };
  function updateStatus() {
    save.disabled = saving || !changed();
    save.textContent = saving ? 'Saving…' : 'Save';
    let message = changed() ? 'Unsaved changes' : 'All changes saved';
    let state = changed() ? 'unsaved' : 'saved';
    if (content() !== renderedSource && mode !== 'edit') message += ' · Updating preview…';
    if (previewError) { message = 'Preview failed: ' + previewError; state = 'error'; }
    if (saveError) { message = 'Save failed: ' + saveError; state = 'error'; }
    if (saving) { message = 'Saving changes…'; state = 'saving'; }
    status.textContent = message;
    status.dataset.state = state;
  }

  async function request(action, source, signal) {
    const response = await fetch('/_artifactd/' + action, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: 'Bearer ' + shell.dataset.saveToken,
      },
      body: JSON.stringify({ content: source }),
      signal,
    });
    const result = await response.json();
    if (!response.ok) throw new Error(result.error || 'Request failed');
    return result;
  }

  function cancelRender() {
    clearTimeout(renderTimer);
    renderController?.abort();
    generation++;
  }
  function scheduleRender() {
    cancelRender();
    if (content() === saved) {
      showHTML(savedHTML);
      renderedSource = saved;
      return;
    }
    renderTimer = setTimeout(() => { void renderDraft(); }, 300);
  }
  async function renderDraft() {
    const source = content();
    if (source === renderedSource) return;
    cancelRender();
    const current = generation;
    renderController = new AbortController();
    try {
      const result = await request('render', source, renderController.signal);
      if (current !== generation || source !== content()) return;
      showHTML(result.html);
      renderedSource = source;
      previewError = '';
    } catch (error) {
      if (current !== generation || error.name === 'AbortError') return;
      previewError = error.message || 'Unable to render draft';
    }
    updateStatus();
  }

  function setMode(next) {
    scrollPositions[mode] = mode === 'edit' ? view.scrollDOM.scrollTop : main.scrollTop;
    mode = next;
    const editing = mode === 'edit';
    document.body.classList.toggle('artifactd-md-editing', editing);
    toggleView.setAttribute('aria-pressed', String(editing));
    toggleView.setAttribute('aria-label', editing ? 'Preview document' : 'Edit document');
    toggleView.querySelector('span').textContent = editing ? 'Preview' : 'Edit';
    modeLabel.textContent = editing ? 'Editing' : 'Preview';
    if (editing) {
      view.requestMeasure();
      view.focus();
      view.scrollDOM.scrollTop = scrollPositions.edit;
    } else {
      main.scrollTop = scrollPositions.preview;
      void renderDraft();
    }
    updateStatus();
  }
  toggleView.addEventListener('click', () => setMode(mode === 'preview' ? 'edit' : 'preview'));

  async function saveDocument() {
    if (saving || !changed()) return;
    const submitted = content();
    saving = true;
    saveError = '';
    updateStatus();
    try {
      const result = await request('save', submitted, AbortSignal.timeout(30000));
      saved = submitted;
      savedHTML = result.html;
      if (content() === submitted) {
        cancelRender();
        showHTML(savedHTML);
        renderedSource = submitted;
        previewError = '';
      }
      // Later keystrokes remain a draft; never replace the editor or its undo history.
    } catch (error) {
      saveError = error.message || 'Unable to save';
    } finally {
      saving = false;
      updateStatus();
    }
  }
  save.addEventListener('click', () => { void saveDocument(); });
  document.addEventListener('keydown', (event) => {
    if (event.defaultPrevented) return;
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'e') {
      event.preventDefault();
      setMode(mode === 'preview' ? 'edit' : 'preview');
      if (mode === 'preview') toggleView.focus();
    }
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's') {
      event.preventDefault();
      void saveDocument();
    }
    if (event.key === 'Escape' && exports.open) closeExports();
  });

  function wrap(before, after, placeholder) {
    const { from, to } = view.state.selection.main;
    const doc = view.state.doc;
    const selected = doc.sliceString(from, to);
    const alreadyWrapped = doc.sliceString(Math.max(0, from - before.length), from) === before &&
      doc.sliceString(to, to + after.length) === after;
    if (alreadyWrapped) {
      view.dispatch({
        changes: [{ from: from - before.length, to: from, insert: '' }, { from: to, to: to + after.length, insert: '' }],
        selection: EditorSelection.range(from - before.length, to - before.length),
        userEvent: 'input',
      });
      return;
    }
    const text = selected || placeholder;
    view.dispatch({
      changes: { from, to, insert: before + text + after },
      selection: EditorSelection.range(from + before.length, from + before.length + text.length),
      userEvent: 'input',
    });
  }
  function format(action) {
    if (action === 'bold') wrap('**', '**', 'bold text');
    if (action === 'italic') wrap('*', '*', 'italic text');
    view.focus();
    return true;
  }

  function closeExports() {
    exports.open = false;
    exportToggle.focus();
  }
  document.addEventListener('click', (event) => { if (!exports.contains(event.target)) exports.open = false; });
  shell.querySelector('[data-action="pdf"]').addEventListener('click', () => {
    if (changed() && !window.confirm('PDF export uses the last saved preview. Continue without unsaved edits?')) return;
    closeExports();
    window.print();
  });
  shell.querySelector('a[download]').addEventListener('click', (event) => {
    if (changed() && !window.confirm('DOCX export uses the last saved version. Continue without unsaved edits?')) {
      event.preventDefault();
      return;
    }
    closeExports();
  });
  window.addEventListener('beforeprint', () => {
    if (printing) return;
    printDraftHTML = main.innerHTML;
    printScroll = main.scrollTop;
    printing = true;
    main.innerHTML = savedHTML;
  });
  window.addEventListener('afterprint', () => {
    if (!printing) return;
    printing = false;
    main.innerHTML = printDraftHTML;
    requestAnimationFrame(() => { main.scrollTop = printScroll; });
  });
  window.addEventListener('beforeunload', (event) => {
    if (!changed()) return;
    event.preventDefault();
    event.returnValue = '';
  });
  updateStatus();
}
