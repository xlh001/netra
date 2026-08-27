import { useEffect, useRef, useState } from 'react'
import { useT } from '../../i18n/context'
import { formatBps, formatBytes, protoColor, serviceColor, windowToSeconds } from '../../lib/format'
import { pagesFor, ROTATE_MS } from '../../lib/pagination'
import { AssetLabel, DomainBadge } from '../../lib/trafficColumns'
import type { FlowStat, Report } from '../../api/types'

const PAGE_SIZE_CEILING = 10

export function FlowsTable({ report }: { report: Report | null }) {
  const t = useT()
  const tableRef = useRef<HTMLTableElement>(null)
  const [page, setPage] = useState(0)
  const [cap, setCap] = useState(PAGE_SIZE_CEILING)

  const flows = report?.topFlows ?? []
  const windowSeconds = windowToSeconds(report?.window ?? '15m')

  useEffect(() => {
    const table = tableRef.current
    if (!table) return
    const body = table.closest<HTMLElement>('.panel-body')
    const measure = () => {
      const theadRow = table.querySelector('thead tr')
      const rowHeight = theadRow ? theadRow.getBoundingClientRect().height : 0
      if (!body || !rowHeight) {
        setCap(PAGE_SIZE_CEILING)
        return
      }
      const dataHeight = body.clientHeight - rowHeight
      setCap(Math.max(1, Math.min(PAGE_SIZE_CEILING, Math.floor(dataHeight / rowHeight))))
    }
    measure()
    const ro = new ResizeObserver(measure)
    if (body) ro.observe(body)
    return () => ro.disconnect()
  }, [])

  const { pages: totalPages, perPage } = pagesFor(flows.length, cap)

  useEffect(() => {
    if (page >= totalPages) setPage(0)
  }, [page, totalPages])

  useEffect(() => {
    if (totalPages <= 1) return
    const id = setInterval(() => setPage((p) => (p + 1) % totalPages), ROTATE_MS)
    return () => clearInterval(id)
  }, [totalPages])

  const start = page * perPage
  const pageFlows = flows.slice(start, start + perPage)

  return (
    <div className="panel" style={{ height: '100%' }}>
      <div className="panel-head">
        <h2>
          <span className="panel-head-title">{t('flowsTitle')}</span>
        </h2>
      </div>
      <div className="panel-body">
        <table id="flows-table" ref={tableRef}>
          <thead>
            <tr>
              <th>{t('colSrc')}</th>
              <th>{t('colDst')}</th>
              <th>{t('colProto')}</th>
              <th>{t('colSvc')}</th>
              <th>{t('colDomain')}</th>
              <th className="num">{t('colPackets')}</th>
              <th className="num">{t('colBytes')}</th>
            </tr>
          </thead>
          <tbody>
            {!pageFlows.length ? (
              <tr>
                <td colSpan={7} className="empty">
                  {t('noData')}
                </td>
              </tr>
            ) : (
              pageFlows.map((f, i) => <FlowRow key={start + i} f={f} windowSeconds={windowSeconds} />)
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

export function FlowRow({ f, windowSeconds }: { f: FlowStat; windowSeconds: number }) {
  const sc = f.service ? serviceColor(f.service) : null
  const avgBps = (f.bytes * 8) / windowSeconds

  return (
    <tr>
      <td className="ip-cell" title={f.srcLabel ? `${f.srcLabel} (${f.srcIP})` : undefined}>
        {f.srcCountry && <img className="flag-icon" src={`/vendor/flags/${f.srcCountry.toLowerCase()}.svg`} alt={f.srcCountry} onError={(e) => (e.currentTarget.style.display = 'none')} />}
        <AssetLabel label={f.srcLabel} value={f.srcIP} />
        {f.srcPort ? ':' + f.srcPort : ''}
      </td>
      <td className="ip-cell" title={f.dstLabel ? `${f.dstLabel} (${f.dstIP})` : undefined}>
        {f.dstCountry && <img className="flag-icon" src={`/vendor/flags/${f.dstCountry.toLowerCase()}.svg`} alt={f.dstCountry} onError={(e) => (e.currentTarget.style.display = 'none')} />}
        <AssetLabel label={f.dstLabel} value={f.dstIP} />
        {f.dstPort ? ':' + f.dstPort : ''}
      </td>
      <td className="proto-cell" style={{ ['--dot' as string]: protoColor(f.proto) }}>
        {f.proto.toUpperCase()}
      </td>
      <td className="svc" title={f.service || ''}>
        {f.service ? (
          <span className="svc-badge" style={sc ? { color: sc.fg, background: sc.bg, borderColor: sc.bd } : undefined}>
            {f.service}
          </span>
        ) : (
          '--'
        )}
      </td>
      <td className="domain-cell">{f.domain ? <DomainBadge domain={f.domain} /> : '--'}</td>
      <td className="num">{f.packets.toLocaleString()}</td>
      <td className="num" title={`${formatBytes(f.bytes)}, avg ${formatBps(avgBps)} over the selected window`}>
        {formatBytes(f.bytes)} <span className="bps-hint">({formatBps(avgBps)})</span>
      </td>
    </tr>
  )
}
