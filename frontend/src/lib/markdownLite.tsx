import { Fragment } from 'react'

export function renderMarkdownLite(text: string) {
  const lines = text.split('\n')
  const blocks: React.ReactNode[] = []
  let listItems: string[] = []

  let codeLines: string[] | null = null

  function flushList() {
    if (listItems.length === 0) return
    blocks.push(
      <ul key={blocks.length}>
        {listItems.map((item, i) => (
          <li key={i}>{renderInline(item)}</li>
        ))}
      </ul>,
    )
    listItems = []
  }

  function flushCode() {
    if (codeLines === null) return
    blocks.push(
      <pre key={blocks.length} className="ai-chat-code">
        <code>{codeLines.join('\n')}</code>
      </pre>,
    )
    codeLines = null
  }

  for (const line of lines) {
    if (/^```/.test(line.trim())) {
      if (codeLines === null) {
        flushList()
        codeLines = []
      } else {
        flushCode()
      }
      continue
    }
    if (codeLines !== null) {
      codeLines.push(line)
      continue
    }
    const trimmed = line.trim()

    const headingMatch = trimmed.match(/^(#{1,3})\s+(.+)$/)
    if (headingMatch) {
      flushList()
      const Tag = (`h${headingMatch[1].length + 3}`) as 'h4' | 'h5' | 'h6'
      blocks.push(<Tag key={blocks.length}>{renderInline(headingMatch[2])}</Tag>)
      continue
    }
    if (trimmed.startsWith('- ') || trimmed.startsWith('* ')) {
      listItems.push(trimmed.slice(2))
      continue
    }
    flushList()
    if (trimmed !== '') {
      blocks.push(<p key={blocks.length}>{renderInline(trimmed)}</p>)
    }
  }
  flushCode()
  flushList()
  return <>{blocks}</>
}

function renderInline(text: string): React.ReactNode {
  const parts = text.split(/(\*\*[^*]+\*\*|`[^`]+`)/g)
  return (
    <Fragment>
      {parts.map((part, i) => {
        if (part.startsWith('**') && part.endsWith('**')) return <strong key={i}>{part.slice(2, -2)}</strong>
        if (part.startsWith('`') && part.endsWith('`') && part.length >= 2) {
          return (
            <code key={i} className="ai-chat-inline-code">
              {part.slice(1, -1)}
            </code>
          )
        }
        return <Fragment key={i}>{part}</Fragment>
      })}
    </Fragment>
  )
}
