import { Fragment } from 'react'

// A deliberately small Markdown subset. Text remains React text nodes;
// raw HTML, images and arbitrary generated links are never executed.
function inline(text: string) {
  return text.split(/(\*\*[^*\n]+\*\*|`[^`\n]+`)/g).map((part, index) => {
    if (part.startsWith('**') && part.endsWith('**')) {
      return <strong key={index}>{part.slice(2, -2)}</strong>
    }
    if (part.startsWith('`') && part.endsWith('`')) {
      return <code key={index}>{part.slice(1, -1)}</code>
    }
    return <Fragment key={index}>{part}</Fragment>
  })
}

export function MessageText({ text }: { text: string }) {
  const lines = text.replace(/\r\n/g, '\n').split('\n')
  const blocks = []
  for (let i = 0; i < lines.length;) {
    const line = lines[i]
    if (!line.trim()) { i++; continue }
    const ordered = /^\s*\d+\\?[.)]\s+/.test(line)
    const unordered = /^\s*[-*]\s+/.test(line)
    if (ordered || unordered) {
      const pattern = ordered ? /^\s*\d+\\?[.)]\s+/ : /^\s*[-*]\s+/
      const start = Number(line.trim().match(/^\d+/)?.[0] ?? 1)
      const items = []
      const key = i
      while (i < lines.length && pattern.test(lines[i])) {
        items.push(<li key={i}>{inline(lines[i].replace(pattern, ''))}</li>)
        i++
      }
      blocks.push(ordered ? <ol key={key} start={start}>{items}</ol> : <ul key={key}>{items}</ul>)
      continue
    }
    blocks.push(<p key={i}>{inline(line.replace(/^#{1,6}\s+/, ''))}</p>)
    i++
  }
  return <div className="message-text">{blocks}</div>
}
