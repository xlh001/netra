import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useT } from '../i18n/context'
import { flagIconSrc, formatBytes } from '../lib/format'
import { pagesFor, ROTATE_MS } from '../lib/pagination'

const ROW_HEIGHT = 18
const PAGE_SIZE_CEILING = 10

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
  const containerRef = useRef<HTMLDivElement>(null)
  const [page, setPage] = useState(0)
  const [cap, setCap] = useState(PAGE_SIZE_CEILING)

  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const measure = () => {
      const h = el.clientHeight
      setCap(h ? Math.max(1, Math.min(PAGE_SIZE_CEILING, Math.floor(h / ROW_HEIGHT))) : PAGE_SIZE_CEILING)
    }
    measure()
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const { pages: totalPages, perPage } = pagesFor(items.length, cap)

  useEffect(() => {
    if (page >= totalPages) setPage(0)
  }, [page, totalPages])

  useEffect(() => {
    if (totalPages <= 1) return
    const id = setInterval(() => {
      setPage((p) => (p + 1) % totalPages)
    }, ROTATE_MS)
    return () => clearInterval(id)
  }, [totalPages])

  if (!items.length) {
    return (
      <div className="rank-list" ref={containerRef}>
        <div className="empty">{t('noData')}</div>
      </div>
    )
  }

  const start = page * perPage
  const pageItems = items.slice(start, start + perPage)

  const max = items.reduce((m, it) => Math.max(m, it.bytes), 0) || 1

  return (
    <div className="rank-list" ref={containerRef}>
      {pageItems.map((it, i) => (
        <RankRow
          key={start + i}
          idx={start + i}
          item={it}
          labelFn={labelFn}
          titleFn={titleFn}
          renderLabel={renderLabel}
          color={colorFn ? colorFn(it, start + i) : color}
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
