import { Tooltip } from 'antd'
import type { ColumnType } from 'antd/es/table'
import type { SVGProps } from 'react'
import { useT } from '../i18n/context'
import type { AlertKind } from '../api/types'
import { categoryColor, protoColor, readableTextColor, serviceColor } from './format'

const ALERT_KIND_LABEL_KEY: Record<AlertKind, 'threatsKindScan' | 'threatsKindDDoS' | 'threatsKindVolume' | 'threatsKindIOC'> = {
  scan: 'threatsKindScan',
  ddos: 'threatsKindDDoS',
  volume: 'threatsKindVolume',
  ioc: 'threatsKindIOC',
}

export function AlertKindBadge({ kind }: { kind: AlertKind }) {
  const t = useT()
  const color = kind === 'scan' ? 'var(--amber)' : 'var(--rose)'
  return (
    <span className="svc-badge" style={{ color, borderColor: color }}>
      {t(ALERT_KIND_LABEL_KEY[kind])}
    </span>
  )
}

export function protoColumn<T>(title: string, dataIndex: keyof T & string): ColumnType<T> {
  return {
    title,
    dataIndex,
    onCell: (record) => ({
      className: 'proto-cell',
      style: { ['--dot' as string]: protoColor(String(record[dataIndex])) },
    }),
    render: (v: string) => v.toUpperCase(),
  }
}

export function ServiceBadge({ svc, dpi }: { svc: string; dpi?: boolean }) {
  const sc = serviceColor(svc)
  return (
    <span className="svc-badge-group" title={svc}>
      {dpi && <span className="svc-badge-dpi">DPI</span>}
      <span className="svc-badge" style={sc ? { color: sc.fg, background: sc.bg, borderColor: sc.bd } : undefined}>
        {svc}
      </span>
    </span>
  )
}

export function DomainBadge({ domain }: { domain: string }) {
  return (
    <span className="domain-badge" title={domain}>
      🌍 {domain}
    </span>
  )
}

export function CategoryBadge({ category, index }: { category: string; index: number }) {
  const bg = categoryColor(category, index)
  return (
    <span className="category-badge" style={{ background: bg, color: readableTextColor(bg) }}>
      {category}
    </span>
  )
}

function InitiatorIcon(props: SVGProps<SVGSVGElement>) {
  return (
    <svg {...props} className="flow-role-icon flow-role-icon-init" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M12 19V6M6 11l6-6 6 6" />
    </svg>
  )
}

function ReceiverIcon(props: SVGProps<SVGSVGElement>) {
  return (
    <svg {...props} className="flow-role-icon flow-role-icon-recv" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M12 5v13M6 13l6 6 6-6" />
    </svg>
  )
}

export function FlowRoleIcon({ initiator }: { initiator: boolean }) {
  return (
    <Tooltip title={initiator ? '发起方' : '接收方'}>
      {initiator ? <InitiatorIcon /> : <ReceiverIcon />}
    </Tooltip>
  )
}

export function AssetLabel({ label, value }: { label?: string; value: string }) {
  return (
    <>
      {label && <span className="ip-label">{label}</span>}
      {value}
    </>
  )
}
