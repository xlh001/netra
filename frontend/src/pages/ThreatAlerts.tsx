import { useState } from 'react'
import { Input, Select, Space, Table } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useT } from '../i18n/context'
import { usePolling } from '../hooks/usePolling'
import { usePagedState } from '../hooks/usePagedState'
import { getThreatAlertsPaged } from '../api/client'
import type { AlertKind, ThreatAlertRecord } from '../api/types'
import { tablePagination } from '../lib/antdTable'
import { AssetLabel } from '../lib/trafficColumns'
import { formatBytes } from '../lib/format'

const PAGE_SIZE_CEILING = 20

const KIND_LABEL_KEY: Record<AlertKind, 'threatsKindScan' | 'threatsKindDDoS' | 'threatsKindVolume'> = {
  scan: 'threatsKindScan',
  ddos: 'threatsKindDDoS',
  volume: 'threatsKindVolume',
}

function KindBadge({ kind }: { kind: AlertKind }) {
  const t = useT()
  const color = kind === 'scan' ? 'var(--amber)' : 'var(--rose)'
  return (
    <span className="svc-badge" style={{ color, borderColor: color }}>
      {t(KIND_LABEL_KEY[kind])}
    </span>
  )
}

export function ThreatAlerts() {
  const t = useT()
  const [ipFilter, setIpFilter] = useState('')
  const [kindFilter, setKindFilter] = useState<AlertKind | ''>('')
  const { containerRef, page, pageSize, setPage, onPageChange } = usePagedState(PAGE_SIZE_CEILING)

  const { data, loading } = usePolling(
    () => getThreatAlertsPaged(page, pageSize, ipFilter || undefined, kindFilter || undefined),
    0,
    [page, pageSize, ipFilter, kindFilter],
  )

  const columns: ColumnsType<ThreatAlertRecord> = [
    { title: t('threatsColTime'), dataIndex: 'time', render: (v: string) => new Date(v).toLocaleString() },
    { title: t('threatsColKind'), dataIndex: 'kind', render: (v: AlertKind) => <KindBadge kind={v} /> },
    { title: t('threatsColIP'), dataIndex: 'ip', render: (v: string, a) => <AssetLabel label={a.label} value={v} /> },
    {
      title: t('threatsColPeers'),
      key: 'peersOrVolume',
      align: 'right',
      render: (_, a) => (a.kind === 'volume' ? formatBytes(a.volumeBytes ?? 0) : (a.distinctPeers ?? 0).toLocaleString()),
    },
  ]

  return (
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
            ]}
          />
        </Space>
        <Table
          rowKey={(a) => `${a.time}-${a.kind}-${a.ip}`}
          columns={columns}
          dataSource={data?.alerts ?? []}
          loading={loading}
          pagination={tablePagination(page, pageSize, data?.total ?? 0, onPageChange, t)}
          size="small"
        />
      </div>
    </div>
  )
}
