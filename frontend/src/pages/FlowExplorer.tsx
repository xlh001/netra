import { useState } from 'react'
import { Input, Table, Tabs } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useT } from '../i18n/context'
import { usePolling } from '../hooks/usePolling'
import { usePagedState } from '../hooks/usePagedState'
import { getDomainsPaged, getFlowsPaged, getIPsPaged, getPortsPaged, getServiceCategories, getTimeseriesRange } from '../api/client'
import type { CategoryStat, DomainStat, FlowStat, IPStat, PortStat, TimeRange } from '../api/types'
import { TimeRangeSelector } from '../components/TimeRangeSelector'
import { TrendChart } from '../components/panels/TrendChart'
import { ProtocolPie } from '../components/panels/ProtocolPie'
import { formatBps, formatBytes, rangeToSeconds } from '../lib/format'
import { tablePagination } from '../lib/antdTable'
import { AssetLabel, CategoryBadge, protoColumn, ServiceBadge, serviceColumn } from '../lib/trafficColumns'

const PAGE_SIZE_CEILING = 20

const CHARTS_ROW_CHROME_PX = 202

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
          ]}
        />
      </div>
    </>
  )
}

function FlowsTab({ range }: { range: TimeRange }) {
  const t = useT()
  const { containerRef, page, pageSize, setPage, onPageChange } = usePagedState(PAGE_SIZE_CEILING, CHARTS_ROW_CHROME_PX)
  const [ipFilter, setIpFilter] = useState('')
  const { data, loading } = usePolling(() => getFlowsPaged(range, page, pageSize, ipFilter || undefined), 0, [range, page, pageSize, ipFilter])
  const { data: timeseries } = usePolling(() => getTimeseriesRange(range), 0, [range])
  const windowSeconds = rangeToSeconds(range)

  const columns: ColumnsType<FlowStat> = [
    {
      title: t('colSrc'),
      key: 'src',
      render: (_, f) => (
        <>
          <AssetLabel label={f.srcLabel} value={f.srcIP} />
          {f.srcPort ? ':' + f.srcPort : ''}
        </>
      ),
    },
    {
      title: t('colDst'),
      key: 'dst',
      render: (_, f) => (
        <>
          <AssetLabel label={f.dstLabel} value={f.dstIP} />
          {f.dstPort ? ':' + f.dstPort : ''}
        </>
      ),
    },
    protoColumn<FlowStat>(t('colProto'), 'proto'),
    serviceColumn<FlowStat>(t('colSvc'), 'service'),
    { title: t('colDomain'), dataIndex: 'domain', render: (v?: string) => v || '--' },
    { title: t('colPackets'), dataIndex: 'packets', align: 'right', render: (v: number) => v.toLocaleString() },
    { title: t('colBytes'), dataIndex: 'bytes', align: 'right', render: (v: number) => bytesWithRate(v, windowSeconds) },
  ]

  return (
    <div ref={containerRef} className="explorer-tab-body">
      <div className="explorer-charts-row">
        <div style={{ flex: 1.6, display: 'flex' }}>
          <TrendChart timeseries={timeseries ?? null} />
        </div>
        <div style={{ flex: 1, display: 'flex' }}>
          <ProtocolPie timeseries={timeseries ?? null} />
        </div>
      </div>
      <Input.Search
        placeholder={t('flowsFilterIPPlaceholder')}
        style={{ width: 260, marginBottom: 12 }}
        onSearch={(v) => {
          setPage(0)
          setIpFilter(v)
        }}
        allowClear
      />
      <Table
        rowKey={(f) => `${f.srcIP}:${f.srcPort}-${f.dstIP}:${f.dstPort}-${f.proto}`}
        columns={columns}
        dataSource={data?.flows ?? []}
        loading={loading}
        pagination={tablePagination(page, pageSize, data?.total ?? 0, onPageChange, t)}
        size="small"
      />
    </div>
  )
}

function IPsTab({ range }: { range: TimeRange }) {
  const t = useT()
  const { containerRef, page, pageSize, setPage, onPageChange } = usePagedState(PAGE_SIZE_CEILING)
  const [q, setQ] = useState('')
  const { data, loading } = usePolling(() => getIPsPaged(range, page, pageSize, q), 0, [range, page, pageSize, q])
  const windowSeconds = rangeToSeconds(range)

  const columns: ColumnsType<IPStat> = [
    { title: t('colIP'), dataIndex: 'ip', render: (v: string, ip) => <AssetLabel label={ip.label} value={v} /> },
    { title: t('colPackets'), dataIndex: 'packets', align: 'right', render: (v: number) => v.toLocaleString() },
    { title: t('colBytes'), dataIndex: 'bytes', align: 'right', render: (v: number) => bytesWithRate(v, windowSeconds) },
  ]

  return (
    <div ref={containerRef} className="explorer-tab-body">
      <Input.Search
        placeholder={t('ipsFilterPlaceholder')}
        style={{ width: 260, marginBottom: 12 }}
        onSearch={(v) => {
          setPage(0)
          setQ(v)
        }}
        allowClear
      />
      <Table
        rowKey="ip"
        columns={columns}
        dataSource={data?.ips ?? []}
        loading={loading}
        pagination={tablePagination(page, pageSize, data?.total ?? 0, onPageChange, t)}
        size="small"
      />
    </div>
  )
}

function PortsTab({ range }: { range: TimeRange }) {
  const t = useT()
  const { containerRef, page, pageSize, setPage, onPageChange } = usePagedState(PAGE_SIZE_CEILING)
  const [q, setQ] = useState('')
  const { data, loading } = usePolling(() => getPortsPaged(range, page, pageSize, q), 0, [range, page, pageSize, q])
  const windowSeconds = rangeToSeconds(range)

  const columns: ColumnsType<PortStat> = [
    protoColumn<PortStat>(t('colProto'), 'proto'),
    { title: t('colPort'), dataIndex: 'port' },
    serviceColumn<PortStat>(t('colSvc'), 'service'),
    { title: t('colPackets'), dataIndex: 'packets', align: 'right', render: (v: number) => v.toLocaleString() },
    { title: t('colBytes'), dataIndex: 'bytes', align: 'right', render: (v: number) => bytesWithRate(v, windowSeconds) },
  ]

  return (
    <div ref={containerRef} className="explorer-tab-body">
      <Input.Search
        placeholder={t('portsFilterPlaceholder')}
        style={{ width: 260, marginBottom: 12 }}
        onSearch={(v) => {
          setPage(0)
          setQ(v)
        }}
        allowClear
      />
      <Table
        rowKey={(p) => `${p.proto}-${p.port}`}
        columns={columns}
        dataSource={data?.ports ?? []}
        loading={loading}
        pagination={tablePagination(page, pageSize, data?.total ?? 0, onPageChange, t)}
        size="small"
      />
    </div>
  )
}

function DomainsTab({ range }: { range: TimeRange }) {
  const t = useT()
  const { containerRef, page, pageSize, setPage, onPageChange } = usePagedState(PAGE_SIZE_CEILING)
  const [q, setQ] = useState('')
  const { data, loading } = usePolling(() => getDomainsPaged(range, page, pageSize, q), 0, [range, page, pageSize, q])
  const windowSeconds = rangeToSeconds(range)

  const columns: ColumnsType<DomainStat> = [
    { title: t('colDomain'), dataIndex: 'domain' },
    { title: t('colPackets'), dataIndex: 'packets', align: 'right', render: (v: number) => v.toLocaleString() },
    { title: t('colBytes'), dataIndex: 'bytes', align: 'right', render: (v: number) => bytesWithRate(v, windowSeconds) },
  ]

  return (
    <div ref={containerRef} className="explorer-tab-body">
      <Input.Search
        placeholder={t('domainsFilterPlaceholder')}
        style={{ width: 260, marginBottom: 12 }}
        onSearch={(v) => {
          setPage(0)
          setQ(v)
        }}
        allowClear
      />
      <Table
        rowKey="domain"
        columns={columns}
        dataSource={data?.domains ?? []}
        loading={loading}
        pagination={tablePagination(page, pageSize, data?.total ?? 0, onPageChange, t)}
        size="small"
      />
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
  const { data, loading } = usePolling(() => getServiceCategories(range), 0, [range])
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
      <Table rowKey="key" columns={columns} dataSource={dataSource} loading={loading} pagination={false} size="small" />
    </div>
  )
}
