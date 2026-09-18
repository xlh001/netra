import { useEffect, useState, type ReactNode } from 'react'
import { useT } from '../i18n/context'
import { flagIconSrc, formatBytes } from '../lib/format'

export interface RankItem {
  bytes: number
  country?: string
}

interface RankListProps<T extends RankItem> {
  items: T[]
  labelFn: (item: T) => string
  titleFn?: (item: T) => string
  renderLabel?: (item: T) => ReactNode
  color: string
  colorFn?: (item: T, idx: number) => string
}

export function RankList<T extends RankItem>({ items, labelFn, titleFn, renderLabel, color, colorFn }: RankListProps<T>) {
  const t = useT()

  if (!items.length) {
    return (
      <div className="rank-list">
        <div className="empty">{t('noData')}</div>
      </div>
    )
  }

  const max = items.reduce((m, it) => Math.max(m, it.bytes), 0) || 1

  return (
    <div className="rank-list">
      {items.map((it, i) => (
        <RankRow
          key={i}
          idx={i}
          item={it}
          labelFn={labelFn}
          titleFn={titleFn}
          renderLabel={renderLabel}
          color={colorFn ? colorFn(it, i) : color}
          max={max}
        />
      ))}
    </div>
  )
}

function RankRow<T extends RankItem>({
  idx,
  item,
  labelFn,
  titleFn,
  renderLabel,
  color,
  max,
}: {
  idx: number
  item: T
  labelFn: (item: T) => string
  titleFn?: (item: T) => string
  renderLabel?: (item: T) => ReactNode
  color: string
  max: number
}) {
  const [width, setWidth] = useState(0)
  useEffect(() => {
    const raf = requestAnimationFrame(() => setWidth((item.bytes / max) * 100))
    return () => cancelAnimationFrame(raf)
  }, [item.bytes, max])

  const label = labelFn(item)
  const flagSrc = item.country ? flagIconSrc(item.country) : ''

  return (
    <div className="rank-row">
      <div className="rank-bg-fill" style={{ width: width + '%', background: color }} />
      <div className="rank-idx">{idx + 1}</div>
      <div className="rank-label" title={titleFn ? titleFn(item) : label}>
        {flagSrc && <img className="flag-icon" src={flagSrc} alt={item.country} onError={(e) => (e.currentTarget.style.display = 'none')} />}
        {renderLabel ? renderLabel(item) : label}
      </div>
      <div className="rank-value" style={{ color }}>
        {formatBytes(item.bytes)}
      </div>
    </div>
  )
}
