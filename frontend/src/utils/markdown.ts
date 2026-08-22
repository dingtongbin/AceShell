// Markdown 渲染(AI/用户消息正文)。
// marked 解析 + DOMPurify 清理(XSS 防护) + highlight.js 代码块高亮。
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import hljs from 'highlight.js/lib/common'

marked.setOptions({
  breaks: true, // 单换行 → <br>(聊天场景)
  gfm: true,
})

/** renderMd markdown → 安全 HTML(代码块高亮,未命中语言降级纯文本)。 */
export function renderMd(text: string): string {
  const raw = marked.parse(text || '', { async: false }) as string
  const safe = DOMPurify.sanitize(raw, {
    ALLOWED_TAGS: [
      'p', 'br', 'b', 'strong', 'i', 'em', 'u', 's', 'del', 'code', 'pre',
      'blockquote', 'ul', 'ol', 'li', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
      'a', 'hr', 'table', 'thead', 'tbody', 'tr', 'th', 'td', 'span', 'input',
    ],
    ALLOWED_ATTR: ['href', 'target', 'class', 'type', 'checked', 'disabled', 'rel'],
  })
  return highlightIn(safe)
}

function highlightIn(html: string): string {
  try {
    const doc = new DOMParser().parseFromString(html, 'text/html')
    doc.querySelectorAll('pre code').forEach(el => {
      const lang = Array.from(el.classList).find(c => c.startsWith('language-'))?.slice(9) || ''
      if (lang && hljs.getLanguage(lang)) {
        try {
          el.innerHTML = hljs.highlight(el.textContent || '', { language: lang }).value
          el.classList.add('hljs')
        } catch { /* 高亮失败保留原文 */ }
      }
    })
    return doc.body.innerHTML
  } catch {
    return html
  }
}
