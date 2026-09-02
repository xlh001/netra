import { useState } from 'react'
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
  )
}
