import { useT } from '../../i18n/context'
import { formatBytes } from '../../lib/format'
import type { FlowStat } from '../../api/types'

function endpoint(label: string | undefined, ip: string, port: number): { label?: string; addr: string } {
  return { label: label || undefined, addr: port ? `${ip}:${port}` : ip }
}

function protoChipClass(proto: string): string {
  const p = proto.toLowerCase()
  if (p === 'ssh' || p === 'rdp' || p === 'telnet') return 'ssh'
  if (p === 'mysql' || p === 'postgres' || p === 'redis' || p === 'oracle') return 'mysql'
  if (p === 'http' || p === 'https' || p === 'tls') return 'http'
  return ''
}

function FlowChip({ f }: { f: FlowStat }) {
  const src = endpoint(f.srcLabel, f.srcIP, f.srcPort)
  const dst = endpoint(f.dstLabel, f.dstIP, f.dstPort)
  const proto = (f.service || f.proto || '').toLowerCase()
  return (
    <span className="flow">
      <span className="ep-group">
        {src.label && <span className="biz">{src.label}</span>}
        <span className="ep">{src.addr}</span>
      </span>
      <span className="ar">→</span>
      <span className="ep-group">
        {dst.label && <span className="biz">{dst.label}</span>}
        <span className="ep">{dst.addr}</span>
      </span>
      {proto && <span className={'pr ' + protoChipClass(proto)}>{proto}</span>}
      <span className="bw">{formatBytes(f.bytes)}</span>
    </span>
  )
}

export function FlowTicker({ flows }: { flows: FlowStat[] }) {
  const t = useT()
  const doubled = flows.length ? flows.concat(flows) : []

  return (
    <div className="ticker">
      <div className="ticker-cap">
        <span className="ticker-dot" />
        {t('liveFlowTitle')}
      </div>
      <div className="ticker-track-wrap">
        {doubled.length ? (
          <div className="ticker-track">
            {doubled.map((f, i) => (
              <FlowChip key={i} f={f} />
            ))}
          </div>
        ) : (
          <div className="ticker-empty">{t('noData')}</div>
        )}
      </div>
    </div>
  )
}
