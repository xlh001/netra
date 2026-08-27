import type { ColumnType } from 'antd/es/table'
import { categoryColor, protoColor, readableTextColor, serviceColor } from './format'

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
    <span title={svc}>
      {dpi && <span className="svc-badge-dpi">DPI</span>}
      <span className="svc-badge" style={sc ? { color: sc.fg, background: sc.bg, borderColor: sc.bd } : undefined}>
        {svc}
      </span>
    </span>
  )
}

export function serviceColumn<T>(title: string, dataIndex: keyof T & string): ColumnType<T> {
  return {
    title,
    dataIndex,
    render: (v?: string) => (v ? <ServiceBadge svc={v} /> : '--'),
  }
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

export function AssetLabel({ label, value }: { label?: string; value: string }) {
  return (
    <>
      {label && <span className="ip-label">{label}</span>}
      {value}
    </>
  )
}
