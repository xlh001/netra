import { useMemo, useState } from 'react'
import { Input, Select, Space, Table } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useT } from '../i18n/context'
import { usePolling } from '../hooks/usePolling'
import { usePagedState } from '../hooks/usePagedState'
import { getThreatAlertsPaged } from '../api/client'
import type { AlertKind, ThreatAlertRecord } from '../api/types'
import { DataPagination } from '../components/DataPagination'
import { AlertKindBadge, AssetLabel } from '../lib/trafficColumns'
import { formatBytes } from '../lib/format'

export function ThreatAlerts() {
  const t = useT()
  const [ipFilter, setIpFilter] = useState('')
  const [kindFilter, setKindFilter] = useState<AlertKind | ''>('')
  const { containerRef, page, pageSize, setPage, onPageChange } = usePagedState()

  const { data, loading } = usePolling(
    () => getThreatAlertsPaged(page, pageSize, ipFilter || undefined, kindFilter || undefined),
    0,
    [page, pageSize, ipFilter, kindFilter],
  )

  const counts = useMemo(() => {
    const c: Record<AlertKind, number> = { scan: 0, ddos: 0, volume: 0, ioc: 0 }
    for (const a of data?.alerts ?? []) if (a.kind in c) c[a.kind] += 1
    return c
  }, [data])

  const sevCards: { kind: AlertKind; color: string; label: string; sub: string }[] = [
    { kind: 'ddos', color: 'var(--rose)', label: t('threatsKindDDoS'), sub: t('threatsColPeers') },
    { kind: 'scan', color: 'var(--amber)', label: t('threatsKindScan'), sub: t('threatsColPeers') },
    { kind: 'ioc', color: 'var(--iris)', label: t('threatsKindIOC'), sub: t('threatsColKind') },
    { kind: 'volume', color: 'var(--scan)', label: t('threatsKindVolume'), sub: t('threatsColPeers') },
  ]
  const toggleKind = (k: AlertKind) => {
    setPage(0)
    setKindFilter((cur) => (cur === k ? '' : k))
  }

  const columns: ColumnsType<ThreatAlertRecord> = [
    { title: t('threatsColTime'), dataIndex: 'time', render: (v: string) => new Date(v).toLocaleString() },
    { title: t('threatsColKind'), dataIndex: 'kind', render: (v: AlertKind) => <AlertKindBadge kind={v} /> },
    { title: t('threatsColIP'), dataIndex: 'ip', render: (v: string, a) => <AssetLabel label={a.label} value={v} /> },
    {
      title: t('threatsColPeers'),
      key: 'peersOrVolume',
      align: 'right',
      render: (_, a) => (a.kind === 'volume' ? formatBytes(a.volumeBytes ?? 0) : (a.distinctPeers ?? 0).toLocaleString()),
    },
  ]

  return (
    <>
    <div className="sevstrip">
      {sevCards.map((c) => (
        <button
          key={c.kind}
          type="button"
          className="sevcard"
          onClick={() => toggleKind(c.kind)}
          style={{ cursor: 'pointer', textAlign: 'left', font: 'inherit', outline: kindFilter === c.kind ? `1px solid ${c.color}` : 'none' }}
        >
          <span className="sev-bar" style={{ background: c.color }} />
          <div className="sev-k">{c.label}</div>
          <div className="sev-v" style={{ color: c.color }}>{counts[c.kind]}</div>
          <div className="sev-s">{c.sub}</div>
        </button>
      ))}
    </div>
    <div className="panel flex1">
      <div className="panel-head">
        <h2>
          <span className="panel-head-title">{t('threatsPageTitle')}</span>
        </h2>
      </div>
      <div className="panel-body explorer-tab-body" ref={containerRef}>
        <Space style={{ marginBottom: 12 }}>
          <Input.Search
            placeholder={t('threatsFilterIPPlaceholder')}
            style={{ width: 260 }}
            onSearch={(v) => {
              setPage(0)
              setIpFilter(v)
            }}
            allowClear
          />
          <Select<AlertKind | ''>
            value={kindFilter}
            style={{ width: 160 }}
            onChange={(v) => {
              setPage(0)
              setKindFilter(v)
            }}
            options={[
              { value: '', label: t('threatsKindAll') },
              { value: 'scan', label: t('threatsKindScan') },
              { value: 'ddos', label: t('threatsKindDDoS') },
              { value: 'volume', label: t('threatsKindVolume') },
              { value: 'ioc', label: t('threatsKindIOC') },
            ]}
          />
        </Space>
        <Table
          rowKey={(a) => `${a.time}-${a.kind}-${a.ip}`}
          columns={columns}
          dataSource={loading ? [] : (data?.alerts ?? [])}
          loading={loading}
          pagination={false}
          size="small"
        />
        <DataPagination page={page} pageSize={pageSize} total={data?.total ?? 0} onPageChange={onPageChange} t={t} />
      </div>
    </div>
    </>
  )
}
