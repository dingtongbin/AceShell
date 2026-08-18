// useShellEditor.ts — Shell 脚本/多行粘贴共用的 CodeMirror 编辑器封装。
// TabPane 的"多行粘贴确认"与"执行脚本对话框"共用同一主题与创建逻辑,集中于此避免重复。
import { EditorState } from '@codemirror/state'
import { EditorView, keymap, lineNumbers } from '@codemirror/view'
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands'
import { StreamLanguage, syntaxHighlighting, HighlightStyle } from '@codemirror/language'
import { shell } from '@codemirror/legacy-modes/mode/shell'
import { tags } from '@lezer/highlight'

// shellSyntaxTheme 深色 Shell 语法高亮(与既有粘贴确认/脚本对话框样式一致)。
export const shellSyntaxTheme = HighlightStyle.define([
  { tag: tags.keyword, color: '#569cd6' },
  { tag: tags.operator, color: '#d4d4d4' },
  { tag: tags.number, color: '#b5cea8' },
  { tag: tags.string, color: '#ce9178' },
  { tag: tags.comment, color: '#6a9955' },
  { tag: tags.variableName, color: '#9cdcfe' },
  { tag: tags.function(tags.variableName), color: '#dcdcaa' },
  { tag: tags.constant(tags.variableName), color: '#dcdcaa' },
  { tag: tags.invalid, color: '#f44747' },
])

// shellEditorTheme 深色编辑器外观,字体与终端一致。
export const shellEditorTheme = EditorView.theme({
  '&': { backgroundColor: '#1e1e1e', color: '#d4d4d4', fontSize: '14px', fontFamily: '"Cascadia Code", Consolas, "Courier New", monospace', lineHeight: '1.5' },
  '.cm-content': { caretColor: '#fff', padding: '4px 0' },
  '.cm-cursor, .cm-dropCursor': { borderLeftColor: '#fff' },
  '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection': { backgroundColor: '#264f78' },
  '.cm-gutters': { backgroundColor: '#1e1e1e', color: '#858585', border: 'none', borderRight: '1px solid #3c3c3c' },
  '.cm-activeLine': { backgroundColor: 'rgba(255, 255, 255, 0.04)' },
  '.cm-activeLineGutter': { backgroundColor: 'rgba(255, 255, 255, 0.04)', color: '#c6c6c6' },
  '.cm-selectionMatch': { backgroundColor: 'rgba(38, 79, 120, 0.4)' },
  '&.cm-focused .cm-matchingBracket': { backgroundColor: 'rgba(38, 79, 120, 0.4)' },
}, { dark: true })

// createShellEditor 在指定容器内创建带行号/历史/Shell 语法高亮的编辑器。
// onChange 在文档内容变化时回调(同步编辑器内容到外部 ref)。
export function createShellEditor(el: HTMLElement, initialDoc: string, onChange: (doc: string) => void): EditorView {
  return new EditorView({
    state: EditorState.create({
      doc: initialDoc,
      extensions: [
        lineNumbers(),
        history(),
        StreamLanguage.define(shell),
        syntaxHighlighting(shellSyntaxTheme),
        shellEditorTheme,
        EditorView.lineWrapping,
        keymap.of([...defaultKeymap, ...historyKeymap]),
        EditorView.updateListener.of(u => {
          if (u.docChanged) onChange(u.state.doc.toString())
        }),
      ],
    }),
    parent: el,
  })
}
