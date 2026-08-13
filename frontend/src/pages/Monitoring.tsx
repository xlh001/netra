import { Card, Col, Progress, Row, Statistic } from 'antd'
import { useT } from '../i18n/context'
import { usePolling } from '../hooks/usePolling'
import { getMonitorSnapshot } from '../api/client'
import { formatBytes } from '../lib/format'

const POLL_MS = 3000

function usageColor(percent: number): string {
  if (percent >= 90) return 'var(--rose)'
  if (percent >= 70) return 'var(--amber)'
  return 'var(--good)'
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
      <div className="panel-body" style={{ maxWidth: '1000px' }}>
        {loading && !data && <div className="empty">{t('noData')}</div>}
        {error && (
          <div className="empty">
            {t('fetchFailed')}
            {error.message}
          </div>
        )}
        {data && (
          <>
            <Row gutter={[16, 16]}>
              <Col span={8}>
                <Card size="small">
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
                <Card size="small">
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
                <Card size="small">
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

            <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
              <Col span={12}>
                <Card size="small" title={t('monitorProcess')}>
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
                <Card size="small" title={t('monitorDatabase')}>
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
                <Card size="small" title={t('monitorCapture')}>
                  <Statistic
                    title={t('monitorReadFailures')}
                    value={data.readFailuresRecent}
                    valueStyle={data.readFailuresRecent > 0 ? { color: 'var(--amber)' } : undefined}
                  />
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
