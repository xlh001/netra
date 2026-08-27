import type { ReactNode } from 'react'
import { Card, Col, Progress, Row, Statistic, Tag, Tooltip } from 'antd'
import { CloudUploadOutlined, DatabaseOutlined, InfoCircleOutlined, LinkOutlined, ThunderboltOutlined, WifiOutlined } from '@ant-design/icons'
import { useT } from '../i18n/context'
import { usePolling } from '../hooks/usePolling'
import { getMonitorSnapshot } from '../api/client'
import { formatBps, formatBytes, formatCount } from '../lib/format'

const POLL_MS = 3000

const KAFKA_QUEUE_BUDGET_BYTES = 64 * 1024 * 1024

function usageColor(percent: number): string {
  if (percent >= 90) return 'var(--rose)'
  if (percent >= 70) return 'var(--amber)'
  return 'var(--good)'
}

function warnColor(value: number): { color: string } | undefined {
  return value > 0 ? { color: 'var(--amber)' } : undefined
}

function kafkaQueueColor(bytes: number): { color: string } | undefined {
  const pct = bytes / KAFKA_QUEUE_BUDGET_BYTES
  if (pct >= 0.95) return { color: 'var(--rose)' }
  if (pct >= 0.8) return { color: 'var(--amber)' }
  return undefined
}

function cardTitle(icon: ReactNode, color: string, label: string): ReactNode {
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
      <span style={{ color, display: 'inline-flex', fontSize: 14 }}>{icon}</span>
      {label}
    </span>
  )
}

export function Monitoring() {
  const t = useT()
  const { data, loading, error } = usePolling(getMonitorSnapshot, POLL_MS)

  return (
    <div className="panel flex1">
      <div className="panel-head">
        <h2>
          <span className="panel-head-title">{t('monitorPageTitle')}</span>
        </h2>
      </div>
      <div className="panel-body">
        {loading && !data && <div className="empty">{t('noData')}</div>}
        {error && (
          <div className="empty">
            {t('fetchFailed')}
            {error.message}
          </div>
        )}
        {data && (
          <>
            <Row gutter={[16, 16]} align="stretch">
              <Col span={8}>
                <Card size="small" style={{ height: '100%' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
                    <Progress type="dashboard" size={90} percent={Math.round(data.cpuPercent)} strokeColor={usageColor(data.cpuPercent)} />
                    <div>
                      <div style={{ fontWeight: 600 }}>{t('monitorCPU')}</div>
                      <div className="settings-section-desc" style={{ margin: 0 }}>
                        {t('monitorCores', { n: data.numCPU })}
                      </div>
                      <div className="settings-section-desc" style={{ margin: 0 }}>
                        {t('monitorLoadAvg')}: {data.loadAvg1.toFixed(2)} / {data.loadAvg5.toFixed(2)} / {data.loadAvg15.toFixed(2)}
                      </div>
                    </div>
                  </div>
                </Card>
              </Col>
              <Col span={8}>
                <Card size="small" style={{ height: '100%' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
                    <Progress type="dashboard" size={90} percent={Math.round(data.memUsedPercent)} strokeColor={usageColor(data.memUsedPercent)} />
                    <div>
                      <div style={{ fontWeight: 600 }}>{t('monitorMemory')}</div>
                      <div className="settings-section-desc" style={{ margin: 0 }}>
                        {formatBytes(data.memUsedBytes)} / {formatBytes(data.memTotalBytes)}
                      </div>
                    </div>
                  </div>
                </Card>
              </Col>
              <Col span={8}>
                <Card size="small" style={{ height: '100%' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
                    <Progress type="dashboard" size={90} percent={Math.round(data.diskUsedPercent)} strokeColor={usageColor(data.diskUsedPercent)} />
                    <div>
                      <div style={{ fontWeight: 600 }}>{t('monitorDisk')}</div>
                      <div className="settings-section-desc" style={{ margin: 0 }}>
                        {formatBytes(data.diskUsedBytes)} / {formatBytes(data.diskTotalBytes)}
                      </div>
                    </div>
                  </div>
                </Card>
              </Col>
            </Row>

            <Row gutter={[16, 16]} align="stretch" style={{ marginTop: 16 }}>
              <Col span={12}>
                <Card size="small" style={{ height: '100%' }} title={cardTitle(<ThunderboltOutlined />, 'var(--scan)', t('monitorProcess'))}>
                  <Row gutter={16}>
                    <Col span={12}>
                      <Statistic title={t('monitorGoroutines')} value={data.goroutines} />
                    </Col>
                    <Col span={12}>
                      <Statistic title={t('monitorHeap')} value={formatBytes(data.heapAllocBytes)} />
                    </Col>
                    <Col span={12} style={{ marginTop: 16 }}>
                      <Statistic title={t('monitorGCCount')} value={data.numGC} />
                    </Col>
                    <Col span={12} style={{ marginTop: 16 }}>
                      <Statistic title={t('monitorUptime')} value={formatUptime(data.processUptimeSec, t)} />
                    </Col>
                  </Row>
                </Card>
              </Col>
              <Col span={12}>
                <Card size="small" style={{ height: '100%' }} title={cardTitle(<DatabaseOutlined />, 'var(--iris)', t('monitorDatabase'))}>
                  {data.persistenceEnabled ? (
                    <Row gutter={16}>
                      <Col span={12}>
                        <Statistic title={t('monitorDBFileSize')} value={formatBytes(data.dbFileSizeBytes ?? 0)} />
                      </Col>
                      <Col span={12}>
                        <Statistic title={t('monitorDBWalSize')} value={formatBytes(data.dbWalSizeBytes ?? 0)} />
                      </Col>
                      <Col span={12} style={{ marginTop: 16 }}>
                        <Statistic title={t('monitorTSStoreSize')} value={formatBytes(data.tsStoreSizeBytes ?? 0)} />
                      </Col>
                      <Col span={12} style={{ marginTop: 16 }}>
                        <Statistic
                          title={t('monitorDBConns')}
                          value={`${data.dbInUseConns ?? 0} / ${data.dbOpenConns ?? 0}`}
                        />
                      </Col>
                      <Col span={12} style={{ marginTop: 16 }}>
                        <Statistic title={t('monitorDBWaitCount')} value={data.dbWaitCount ?? 0} />
                      </Col>
                    </Row>
                  ) : (
                    <div className="empty">{t('monitorDBDisabled')}</div>
                  )}
                </Card>
              </Col>
            </Row>

            <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
              <Col span={24}>
                <Card size="small" title={cardTitle(<WifiOutlined />, 'var(--good)', t('monitorCapture'))}>
                  <Statistic
                    title={
                      <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
                        {t('monitorReadFailures')}
                        <Tooltip title={t('monitorReadFailuresHint')}>
                          <InfoCircleOutlined style={{ color: 'var(--ink-dim)' }} />
                        </Tooltip>
                      </span>
                    }
                    value={data.readFailuresRecent}
                    valueStyle={warnColor(data.readFailuresRecent)}
                  />
                </Card>
              </Col>
            </Row>

            <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
              <Col span={24}>
                <Card size="small" title={cardTitle(<CloudUploadOutlined />, 'var(--highlight)', t('monitorKafka'))}>
                  {data.kafkaEnabled ? (
                    <Row gutter={16}>
                      <Col span={8}>
                        <Statistic
                          title={t('monitorKafkaQueueBytes')}
                          value={formatBytes(data.kafkaQueueBytes)}
                          valueStyle={kafkaQueueColor(data.kafkaQueueBytes)}
                        />
                      </Col>
                      <Col span={8}>
                        <Statistic title={t('monitorKafkaDropped')} value={data.kafkaDroppedTicksTotal} valueStyle={warnColor(data.kafkaDroppedTicksTotal)} />
                      </Col>
                      <Col span={8}>
                        <Statistic
                          title={t('monitorKafkaWriteErrors')}
                          value={data.kafkaWriteErrorsTotal}
                          valueStyle={warnColor(data.kafkaWriteErrorsTotal)}
                        />
                      </Col>
                    </Row>
                  ) : (
                    <div className="empty">{t('monitorKafkaDisabled')}</div>
                  )}
                </Card>
              </Col>
            </Row>

            <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
              <Col span={24}>
                <Card
                  size="small"
                  title={cardTitle(<LinkOutlined />, 'var(--scan)', t('monitorIfaces'))}
                  extra={
                    <Tag color={data.xdpGenericMode ? 'default' : 'processing'}>
                      {data.xdpGenericMode ? t('monitorIfaceModeGeneric') : t('monitorIfaceModeNative')}
                    </Tag>
                  }
                >
                  {data.ifaces.map((ifc, i) => (
                    <div
                      key={ifc.name}
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        flexWrap: 'wrap',
                        gap: 16,
                        padding: '10px 0',
                        borderBottom: i < data.ifaces.length - 1 ? '1px solid var(--line)' : undefined,
                      }}
                    >
                      <span style={{ fontWeight: 600, minWidth: 90 }}>{ifc.name}</span>
                      <Tag color={ifc.carrierUp ? 'success' : 'error'}>{ifc.carrierUp ? t('monitorIfaceUp') : t('monitorIfaceDown')}</Tag>
                      {!!ifc.speedMbps && (
                        <span className="settings-section-desc" style={{ margin: 0 }}>
                          {ifc.speedMbps >= 1000 ? `${ifc.speedMbps / 1000}Gbps` : `${ifc.speedMbps}Mbps`}
                        </span>
                      )}
                      {ifc.promiscEnabledByNetra && <Tag color="processing">{t('monitorIfacePromiscByNetra')}</Tag>}
                      <span className="settings-section-desc" style={{ margin: '0 0 0 auto' }}>
                        {t('monitorIfaceRx')}: {formatCount(ifc.rxPPS)} pps / {formatBps(ifc.rxBPS)}
                      </span>
                    </div>
                  ))}
                </Card>
              </Col>
            </Row>
          </>
        )}
      </div>
    </div>
  )
}

function formatUptime(sec: number, t: ReturnType<typeof useT>): string {
  const days = Math.floor(sec / 86400)
  const hours = Math.floor((sec % 86400) / 3600)
  const minutes = Math.floor((sec % 3600) / 60)
  if (days > 0) return t('monitorUptimeDays', { d: days, h: hours })
  if (hours > 0) return t('monitorUptimeHours', { h: hours, m: minutes })
  return t('monitorUptimeMinutes', { m: minutes })
}
