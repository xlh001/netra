import { useState } from 'react'
import { Button, Checkbox, Input, Select, Table, Tabs, Tag, Tooltip, message } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import { useT } from '../i18n/context'
import { usePolling } from '../hooks/usePolling'
import { usePagedState } from '../hooks/usePagedState'
import { getDomainsPaged, getFlowsPaged, getIPsPaged, getPortsPaged, getServiceCategories, getSQLAuditPaged, getTimeseriesRange, getWeakAuthFindingsPaged, revealWeakAuthPassword } from '../api/client'
import type { CategoryStat, DomainStat, FlowStat, IPStat, PortStat, SQLAuditDBType, SQLAuditRecord, TimeRange, WeakAuthConfidence, WeakAuthFinding } from '../api/types'
import { TimeRangeSelector } from '../components/TimeRangeSelector'
import { TrendChart } from '../components/panels/TrendChart'
import { ProtocolPie } from '../components/panels/ProtocolPie'
import { IPProfileDrawer } from '../components/panels/IPProfileDrawer'
import { DataPagination } from '../components/DataPagination'
import { formatBps, formatBytes, rangeToSeconds } from '../lib/format'
import { AssetLabel, CategoryBadge, DBTypeBadge, DomainBadge, FlowRoleIcon, protoColumn, ServiceBadge } from '../lib/trafficColumns'

function RefreshButton({ onClick, loading }: { onClick: () => void; loading?: boolean }) {
  return <Button icon={<ReloadOutlined />} onClick={onClick} loading={loading} />
}

function bytesWithRate(bytes: number, windowSeconds: number) {
  const bps = (bytes * 8) / windowSeconds
  return (
    <>
      {formatBytes(bytes)} <span className="bps-hint">({formatBps(bps)})</span>
    </>
  )
}

export function FlowExplorer() {
  const t = useT()
  const [range, setRange] = useState<TimeRange>({ kind: 'window', window: '15m' })

  return (
    <>
      <div className="hdr">
        <TimeRangeSelector value={range} onChange={setRange} />
      </div>
      <div className="panel flex1">
        <div className="panel-head">
          <h2>
            <span className="panel-head-title">{t('flowsPageTitle')}</span>
          </h2>
        </div>
        <Tabs
          className="explorer-tabs"
          items={[
            { key: 'flows', label: t('tabFlows'), children: <FlowsTab range={range} /> },
            { key: 'ips', label: t('tabIPs'), children: <IPsTab range={range} /> },
            { key: 'ports', label: t('tabPorts'), children: <PortsTab range={range} /> },
            { key: 'domains', label: t('tabDomains'), children: <DomainsTab range={range} /> },
            { key: 'categories', label: t('tabCategories'), children: <CategoriesTab range={range} /> },
            { key: 'sqlAudit', label: t('tabSQLAudit'), children: <SQLAuditTab range={range} /> },
            { key: 'weakAuth', label: t('tabWeakAuth'), children: <WeakAuthFindingsTab range={range} /> },
          ]}
        />
      </div>
    </>
  )
}

function FlowsTab({ range }: { range: TimeRange }) {
  const t = useT()
  const { containerRef, page, pageSize, setPage, onPageChange } = usePagedState()
  const [q, setQ] = useState('')
  const [dpiOnly, setDpiOnly] = useState(false)
  const [profileIP, setProfileIP] = useState<string>()
  const [profileOpen, setProfileOpen] = useState(false)
  const { data, loading, error, refetch } = usePolling(() => getFlowsPaged(range, page, pageSize, q || undefined, dpiOnly), 0, [range, page, pageSize, q, dpiOnly])
  const { data: timeseries, loading: timeseriesLoading } = usePolling(() => getTimeseriesRange(range), 0, [range])
  const windowSeconds = rangeToSeconds(range)

  const columns: ColumnsType<FlowStat> = [
    {
      title: t('colSrc'),
      key: 'src',
      render: (_, f) => (
        <>
          <FlowRoleIcon initiator={!f.svcOnSrc} />
          <AssetLabel label={f.srcLabel} value={f.srcIP} />
          {f.srcPort ? ':' + f.srcPort : ''}
          {f.service && f.svcOnSrc && <ServiceBadge svc={f.service} dpi={f.dpi} />}
        </>
      ),
    },
    {
      title: t('colDst'),
      key: 'dst',
      render: (_, f) => (
        <>
          <FlowRoleIcon initiator={!!f.svcOnSrc} />
          <AssetLabel label={f.dstLabel} value={f.dstIP} />
          {f.dstPort ? ':' + f.dstPort : ''}
          {f.service && !f.svcOnSrc && <ServiceBadge svc={f.service} dpi={f.dpi} />}
        </>
      ),
    },
    protoColumn<FlowStat>(t('colProto'), 'proto'),
    { title: t('colDomain'), dataIndex: 'domain', render: (v?: string) => (v ? <DomainBadge domain={v} /> : '--') },
    { title: t('colPackets'), dataIndex: 'packets', align: 'right', render: (v: number) => v.toLocaleString() },
    { title: t('colBytes'), dataIndex: 'bytes', align: 'right', render: (v: number) => bytesWithRate(v, windowSeconds) },
  ]

  return (
    <div ref={containerRef} className="explorer-tab-body">
      <div className="explorer-charts-row">
        <div style={{ flex: 1.6, display: 'flex' }}>
          <TrendChart timeseries={timeseries ?? null} loading={timeseriesLoading} />
        </div>
        <div style={{ flex: 1, display: 'flex' }}>
          <ProtocolPie timeseries={timeseries ?? null} loading={timeseriesLoading} />
        </div>
      </div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 12 }}>
        <Input.Search
          placeholder={t('flowsFilterIPPlaceholder')}
          style={{ width: 260 }}
          onSearch={(v) => {
            setPage(0)
            setQ(v)
          }}
          allowClear
        />
        <Input.Search
          placeholder={t('ipProfilePlaceholder')}
          enterButton={t('ipProfileButton')}
          style={{ width: 260 }}
          onSearch={(v) => {
            if (!v) return
            setProfileIP(v)
            setProfileOpen(true)
          }}
        />
        <Checkbox
          checked={dpiOnly}
          onChange={(e) => {
            setPage(0)
            setDpiOnly(e.target.checked)
          }}
        >
          {t('dpiOnlyFilterLabel')}
        </Checkbox>
        <RefreshButton onClick={refetch} loading={loading} />
      </div>
      <IPProfileDrawer ip={profileIP} open={profileOpen} onClose={() => setProfileOpen(false)} />
      <Table
        rowKey={(f) => `${f.srcIP}:${f.srcPort}-${f.dstIP}:${f.dstPort}-${f.proto}`}
        columns={columns}
        dataSource={loading ? [] : (data?.flows ?? [])}
        loading={loading}
        pagination={false}
        size="small"
        locale={error ? { emptyText: error.message } : undefined}
      />
      <DataPagination page={page} pageSize={pageSize} total={data?.total ?? 0} onPageChange={onPageChange} t={t} sequentialOnly />
    </div>
  )
}

function IPsTab({ range }: { range: TimeRange }) {
  const t = useT()
  const { containerRef, page, pageSize, setPage, onPageChange } = usePagedState()
  const [q, setQ] = useState('')
  const { data, loading, refetch } = usePolling(() => getIPsPaged(range, page, pageSize, q), 0, [range, page, pageSize, q])
  const windowSeconds = rangeToSeconds(range)

  const columns: ColumnsType<IPStat> = [
    { title: t('colIP'), dataIndex: 'ip', render: (v: string, ip) => <AssetLabel label={ip.label} value={v} /> },
    { title: t('colPackets'), dataIndex: 'packets', align: 'right', render: (v: number) => v.toLocaleString() },
    { title: t('colBytes'), dataIndex: 'bytes', align: 'right', render: (v: number) => bytesWithRate(v, windowSeconds) },
  ]

  return (
    <div ref={containerRef} className="explorer-tab-body">
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 12 }}>
        <Input.Search
          placeholder={t('ipsFilterPlaceholder')}
          style={{ width: 260 }}
          onSearch={(v) => {
            setPage(0)
            setQ(v)
          }}
          allowClear
        />
        <RefreshButton onClick={refetch} loading={loading} />
      </div>
      <Table
        rowKey="ip"
        columns={columns}
        dataSource={loading ? [] : (data?.ips ?? [])}
        loading={loading}
        pagination={false}
        size="small"
      />
      <DataPagination page={page} pageSize={pageSize} total={data?.total ?? 0} onPageChange={onPageChange} t={t} sequentialOnly />
    </div>
  )
}

function PortsTab({ range }: { range: TimeRange }) {
  const t = useT()
  const { containerRef, page, pageSize, setPage, onPageChange } = usePagedState()
  const [q, setQ] = useState('')
  const [dpiOnly, setDpiOnly] = useState(false)
  const { data, loading, refetch } = usePolling(() => getPortsPaged(range, page, pageSize, q, dpiOnly), 0, [range, page, pageSize, q, dpiOnly])
  const windowSeconds = rangeToSeconds(range)

  const columns: ColumnsType<PortStat> = [
    protoColumn<PortStat>(t('colProto'), 'proto'),
    { title: t('colPort'), dataIndex: 'port' },
    { title: t('colSvc'), dataIndex: 'service', render: (v: string, r) => (v ? <ServiceBadge svc={v} dpi={r.dpi} /> : '--') },
    { title: t('colPackets'), dataIndex: 'packets', align: 'right', render: (v: number) => v.toLocaleString() },
    { title: t('colBytes'), dataIndex: 'bytes', align: 'right', render: (v: number) => bytesWithRate(v, windowSeconds) },
  ]

  return (
    <div ref={containerRef} className="explorer-tab-body">
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 12 }}>
        <Input.Search
          placeholder={t('portsFilterPlaceholder')}
          style={{ width: 260 }}
          onSearch={(v) => {
            setPage(0)
            setQ(v)
          }}
          allowClear
        />
        <Checkbox
          checked={dpiOnly}
          onChange={(e) => {
            setPage(0)
            setDpiOnly(e.target.checked)
          }}
        >
          {t('dpiOnlyFilterLabel')}
        </Checkbox>
        <RefreshButton onClick={refetch} loading={loading} />
      </div>
      <Table
        rowKey={(p) => `${p.proto}-${p.port}`}
        columns={columns}
        dataSource={loading ? [] : (data?.ports ?? [])}
        loading={loading}
        pagination={false}
        size="small"
      />
      <DataPagination page={page} pageSize={pageSize} total={data?.total ?? 0} onPageChange={onPageChange} t={t} sequentialOnly />
    </div>
  )
}

function DomainsTab({ range }: { range: TimeRange }) {
  const t = useT()
  const { containerRef, page, pageSize, setPage, onPageChange } = usePagedState()
  const [q, setQ] = useState('')
  const { data, loading, refetch } = usePolling(() => getDomainsPaged(range, page, pageSize, q), 0, [range, page, pageSize, q])
  const windowSeconds = rangeToSeconds(range)

  const columns: ColumnsType<DomainStat> = [
    { title: t('colDomain'), dataIndex: 'domain' },
    { title: t('colPackets'), dataIndex: 'packets', align: 'right', render: (v: number) => v.toLocaleString() },
    { title: t('colBytes'), dataIndex: 'bytes', align: 'right', render: (v: number) => bytesWithRate(v, windowSeconds) },
  ]

  return (
    <div ref={containerRef} className="explorer-tab-body">
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 12 }}>
        <Input.Search
          placeholder={t('domainsFilterPlaceholder')}
          style={{ width: 260 }}
          onSearch={(v) => {
            setPage(0)
            setQ(v)
          }}
          allowClear
        />
        <RefreshButton onClick={refetch} loading={loading} />
      </div>
      <Table
        rowKey="domain"
        columns={columns}
        dataSource={loading ? [] : (data?.domains ?? [])}
        loading={loading}
        pagination={false}
        size="small"
      />
      <DataPagination page={page} pageSize={pageSize} total={data?.total ?? 0} onPageChange={onPageChange} t={t} sequentialOnly />
    </div>
  )
}

interface CategoryTreeRow {
  key: string
  label: string
  packets: number
  bytes: number
  isCategory: boolean
  categoryIndex: number
  children?: CategoryTreeRow[]
}

function CategoriesTab({ range }: { range: TimeRange }) {
  const t = useT()
  const { data, loading, refetch } = usePolling(() => getServiceCategories(range), 0, [range])
  const windowSeconds = rangeToSeconds(range)

  const dataSource: CategoryTreeRow[] = (data?.categories ?? []).map((c: CategoryStat, i: number) => ({
    key: c.category,
    label: c.category,
    packets: c.packets,
    bytes: c.bytes,
    isCategory: true,
    categoryIndex: i,
    children: c.services.map((s) => ({
      key: c.category + '/' + s.service,
      label: s.service,
      packets: s.packets,
      bytes: s.bytes,
      isCategory: false,
      categoryIndex: i,
    })),
  }))

  const columns: ColumnsType<CategoryTreeRow> = [
    {
      title: t('colCategory'),
      dataIndex: 'label',
      render: (label: string, row: CategoryTreeRow) =>
        row.isCategory ? <CategoryBadge category={label} index={row.categoryIndex} /> : <ServiceBadge svc={label} />,
    },
    { title: t('colPackets'), dataIndex: 'packets', align: 'right', render: (v: number) => v.toLocaleString() },
    { title: t('colBytes'), dataIndex: 'bytes', align: 'right', render: (v: number) => bytesWithRate(v, windowSeconds) },
  ]

  return (
    <div className="explorer-tab-body">
      <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 12 }}>
        <RefreshButton onClick={refetch} loading={loading} />
      </div>
      <Table rowKey="key" columns={columns} dataSource={dataSource} loading={loading} pagination={false} size="small" />
    </div>
  )
}

function SQLAuditTab({ range }: { range: TimeRange }) {
  const t = useT()
  const { containerRef, page, pageSize, setPage, onPageChange } = usePagedState()
  const [q, setQ] = useState('')
  const [dbType, setDbType] = useState<SQLAuditDBType | ''>('')
  const { data, loading, refetch } = usePolling(
    () => getSQLAuditPaged(range, page, pageSize, q || undefined, dbType || undefined),
    0,
    [range, page, pageSize, q, dbType],
  )

  const columns: ColumnsType<SQLAuditRecord> = [
    { title: t('sqlAuditColTime'), dataIndex: 'time', width: 160, render: (v: string) => new Date(v).toLocaleString() },
    { title: t('sqlAuditColType'), dataIndex: 'dbType', width: 100, render: (v: string) => <DBTypeBadge dbType={v} /> },
    {
      title: t('sqlAuditColSrc'),
      key: 'src',
      width: 180,
      render: (_, r) => `${r.srcIP}:${r.srcPort}`,
    },
    {
      title: t('sqlAuditColDst'),
      key: 'dst',
      width: 180,
      render: (_, r) => `${r.dstIP}:${r.dstPort}`,
    },
    {
      title: t('sqlAuditColQuery'),
      dataIndex: 'queryText',
      render: (v: string, r) => (
        <Tooltip title={<span style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>{v}</span>}>
          <span className="sql-audit-query-cell">
            {v}
            {r.truncated && (
              <span className="svc-badge-dpi" style={{ marginLeft: 6 }}>
                {t('sqlAuditTruncatedTag')}
              </span>
            )}
          </span>
        </Tooltip>
      ),
    },
    { title: t('sqlAuditColCount'), dataIndex: 'count', width: 90, align: 'right', render: (v: number) => v.toLocaleString() },
  ]

  return (
    <div ref={containerRef} className="explorer-tab-body">
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 12 }}>
        <Input.Search
          placeholder={t('sqlAuditFilterPlaceholder')}
          style={{ width: 280 }}
          onSearch={(v) => {
            setPage(0)
            setQ(v)
          }}
          allowClear
        />
        <Select<SQLAuditDBType | ''>
          value={dbType}
          style={{ width: 160 }}
          onChange={(v) => {
            setPage(0)
            setDbType(v)
          }}
          options={[
            { value: '', label: t('sqlAuditTypeAll') },
            { value: 'mysql', label: 'MySQL' },
            { value: 'mongodb', label: 'MongoDB' },
          ]}
        />
        <RefreshButton onClick={refetch} loading={loading} />
      </div>
      <Table
        rowKey={(r) => `${r.time}-${r.srcIP}:${r.srcPort}-${r.dstIP}:${r.dstPort}`}
        columns={columns}
        dataSource={loading ? [] : (data?.records ?? [])}
        loading={loading}
        pagination={false}
        size="small"
      />
      <DataPagination page={page} pageSize={pageSize} total={data?.total ?? 0} onPageChange={onPageChange} t={t} />
    </div>
  )
}

const weakAuthConfidenceColor: Record<WeakAuthConfidence, string> = { high: 'success', medium: 'gold', low: 'default' }

function WeakAuthFindingsTab({ range }: { range: TimeRange }) {
  const t = useT()
  const { containerRef, page, pageSize, setPage, onPageChange } = usePagedState()
  const [q, setQ] = useState('')
  const [confidence, setConfidence] = useState<WeakAuthConfidence | ''>('')
  const [revealed, setRevealed] = useState<Record<number, string>>({})
  const [revealing, setRevealing] = useState<number | null>(null)
  const { data, loading, refetch } = usePolling(
    () => getWeakAuthFindingsPaged(range, page, pageSize, q || undefined, confidence || undefined),
    0,
    [range, page, pageSize, q, confidence],
  )

  async function handleReveal(id: number) {
    setRevealing(id)
    try {
      const res = await revealWeakAuthPassword(id)
      setRevealed((prev) => ({ ...prev, [id]: res.password }))
    } catch (err) {
      message.error(t('weakAuthRevealFailed') + (err instanceof Error ? err.message : String(err)))
    } finally {
      setRevealing(null)
    }
  }

  const columns: ColumnsType<WeakAuthFinding> = [
    { title: t('weakAuthColTime'), dataIndex: 'time', width: 160, render: (v: string) => new Date(v).toLocaleString() },
    {
      title: t('weakAuthColSrc'),
      key: 'src',
      width: 180,
      render: (_, r) => `${r.srcIP}:${r.srcPort}`,
    },
    {
      title: t('weakAuthColDst'),
      key: 'dst',
      width: 180,
      render: (_, r) => `${r.dstIP}:${r.dstPort}`,
    },
    { title: t('weakAuthColUsername'), dataIndex: 'username', width: 160 },
    {
      title: t('weakAuthColPassword'),
      key: 'password',
      width: 200,
      render: (_, r) =>
        revealed[r.id] !== undefined ? (
          <span>{revealed[r.id]}</span>
        ) : (
          <Button type="link" size="small" loading={revealing === r.id} onClick={() => handleReveal(r.id)}>
            {t('weakAuthRevealButton')}
          </Button>
        ),
    },
    { title: t('weakAuthColRule'), dataIndex: 'matchedRule', width: 160 },
    {
      title: t('weakAuthColConfidence'),
      dataIndex: 'confidence',
      width: 110,
      render: (v: WeakAuthConfidence) => <Tag color={weakAuthConfidenceColor[v]}>{t(`weakAuthConfidence_${v}` as Parameters<typeof t>[0])}</Tag>,
    },
    { title: t('weakAuthColStatus'), dataIndex: 'statusCode', width: 90, align: 'right' },
  ]

  return (
    <div ref={containerRef} className="explorer-tab-body">
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 12 }}>
        <Input.Search
          placeholder={t('weakAuthFilterPlaceholder')}
          style={{ width: 280 }}
          onSearch={(v) => {
            setPage(0)
            setQ(v)
          }}
          allowClear
        />
        <Select<WeakAuthConfidence | ''>
          value={confidence}
          style={{ width: 160 }}
          onChange={(v) => {
            setPage(0)
            setConfidence(v)
          }}
          options={[
            { value: '', label: t('weakAuthConfidenceAll') },
            { value: 'high', label: t('weakAuthConfidence_high') },
            { value: 'medium', label: t('weakAuthConfidence_medium') },
            { value: 'low', label: t('weakAuthConfidence_low') },
          ]}
        />
        <RefreshButton onClick={refetch} loading={loading} />
      </div>
      <Table
        rowKey="id"
        columns={columns}
        dataSource={loading ? [] : (data?.findings ?? [])}
        loading={loading}
        pagination={false}
        size="small"
      />
      <DataPagination page={page} pageSize={pageSize} total={data?.total ?? 0} onPageChange={onPageChange} t={t} />
    </div>
  )
}
