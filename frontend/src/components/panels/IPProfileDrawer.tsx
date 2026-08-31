import { useEffect, useMemo, useState } from 'react'
import { Drawer, Empty, Segmented, Spin } from 'antd'
import { usePolling } from '../../hooks/usePolling'
import { getIPProfile } from '../../api/client'
import type { IPProfile, TimeRange } from '../../api/types'
import { useT } from '../../i18n/context'
import { formatBytes } from '../../lib/format'
import { AlertKindBadge } from '../../lib/trafficColumns'
import { IPProfileTrend } from './IPProfileTrend'

const WINDOW_OPTIONS: { labelKey: 'ipProfileWindow1h' | 'ipProfileWindow6h' | 'ipProfileWindow1d' | 'ipProfileWindow7d'; seconds: number }[] = [
  { labelKey: 'ipProfileWindow1h', seconds: 3600 },
  { labelKey: 'ipProfileWindow6h', seconds: 6 * 3600 },
  { labelKey: 'ipProfileWindow1d', seconds: 24 * 3600 },
  { labelKey: 'ipProfileWindow7d', seconds: 7 * 24 * 3600 },
]

function RoleSplitBar({ initiatorBytes, receiverBytes }: { initiatorBytes: number; receiverBytes: number }) {
  const t = useT()
  const total = initiatorBytes + receiverBytes
  const initPct = total > 0 ? (initiatorBytes / total) * 100 : 50
  return (
    <div>
      <div className="role-split-bar">
        <div className="role-split-init" style={{ width: `${initPct}%` }} />
        <div className="role-split-recv" style={{ width: `${100 - initPct}%` }} />
      </div>
      <div className="role-split-legend">
        <span className="role-split-init-label">
          {t('ipProfileInitiator')} {initPct.toFixed(0)}%
        </span>
        <span className="role-split-recv-label">
          {t('ipProfileReceiver')} {(100 - initPct).toFixed(0)}%
        </span>
      </div>
    </div>
  )
}

function BarRow({ label, bytes, maxBytes, dpi, fill }: { label: string; bytes: number; maxBytes: number; dpi?: boolean; fill: string }) {
  const pct = maxBytes > 0 ? (bytes / maxBytes) * 100 : 0
  return (
    <div className="profile-bar-row">
      <div className="profile-bar-fill" style={{ width: `${pct}%`, background: fill }} />
      <div className="profile-bar-main" title={label}>
        {dpi && <span className="svc-badge-dpi">DPI</span>}
        <span className="profile-bar-label">{label}</span>
      </div>
      <div className="profile-bar-bytes">{formatBytes(bytes)}</div>
    </div>
  )
}

export function IPProfileDrawer({ ip, open, onClose }: { ip?: string; open: boolean; onClose: () => void }) {
  const t = useT()
  const [windowSeconds, setWindowSeconds] = useState(WINDOW_OPTIONS[0].seconds)

  useEffect(() => {
    setWindowSeconds(WINDOW_OPTIONS[0].seconds)
  }, [ip])

  const range: TimeRange = useMemo(() => {
    const to = Math.floor(Date.now() / 1000)
    return { kind: 'custom', from: to - windowSeconds, to }
  }, [open, ip, windowSeconds])

  const { data, loading, error } = usePolling<IPProfile | null>(
    () => (open && ip ? getIPProfile(range, ip) : Promise.resolve(null)),
    0,
    [open, ip, range],
  )

  const topPeers = data?.topPeers ?? []
  const topServices = data?.topServices ?? []
  const trend = data?.trend ?? []
  const alerts = data?.alerts ?? []
  const maxPeerBytes = Math.max(1, ...topPeers.map((p) => p.bytes))
  const maxSvcBytes = Math.max(1, ...topServices.map((s) => s.bytes))

  return (
    <Drawer
      title={t('ipProfileTitle')}
      open={open}
      onClose={onClose}
      width="66%"
      extra={
        <Segmented
          value={windowSeconds}
          onChange={(v) => setWindowSeconds(v as number)}
          options={WINDOW_OPTIONS.map((o) => ({ label: t(o.labelKey), value: o.seconds }))}
        />
      }
    >
      {loading && (
        <div className="panel-loading-overlay">
          <Spin />
        </div>
      )}
      {error && <Empty description={t('ipProfileLoadFailed') + error.message} />}
      {data && (
        <div className="ip-profile-body">
          <div className="ip-profile-head">
            <div className="ip-profile-ip">{data.ip}</div>
            <div className="ip-profile-badges">
              {data.label && <span className="asset-pill">{data.label}</span>}
              {(data.country || data.org) && (
                <span className="geo-pill">
                  {data.country}
                  {data.country && data.org ? ' · ' : ''}
                  {data.org}
                </span>
              )}
              {!data.country && !data.org && <span className="geo-pill">{t('ipProfileInternal')}</span>}
            </div>
            <div className="ip-profile-seen">
              {data.firstSeen ? `${t('ipProfileFirstSeen')} ${new Date(data.firstSeen * 1000).toLocaleString()}` : ''}
              {data.lastSeen ? ` · ${t('ipProfileLastSeen')} ${new Date(data.lastSeen * 1000).toLocaleString()}` : ''}
            </div>
          </div>

          <div className="ip-profile-stats">
            <div className="ip-profile-stat-card">
              <div className="ip-profile-stat-label">{t('ipProfileTotalBytes')}</div>
              <div className="ip-profile-stat-value">{formatBytes(data.totalBytes)}</div>
            </div>
            <div className="ip-profile-stat-card">
              <div className="ip-profile-stat-label">{t('ipProfileTotalPackets')}</div>
              <div className="ip-profile-stat-value">{data.totalPackets.toLocaleString()}</div>
            </div>
            <div className="ip-profile-stat-card">
              <div className="ip-profile-stat-label">{t('ipProfilePeerCount')}</div>
              <div className="ip-profile-stat-value">
                {data.peerCount} <span className="ip-profile-stat-unit">{t('ipProfileUnit')}</span>
              </div>
            </div>
            <div className="ip-profile-stat-card">
              <div className="ip-profile-stat-label">{t('ipProfileRoleSplit')}</div>
              <RoleSplitBar initiatorBytes={data.initiatorBytes} receiverBytes={data.receiverBytes} />
            </div>
          </div>

          <h3 className="ip-profile-section-title">{t('ipProfileTrend')}</h3>
          <div className="ip-profile-section-card">
            <IPProfileTrend trend={trend} />
          </div>

          <div className="ip-profile-two-col">
            <div className="ip-profile-section-card">
              <div className="ip-profile-col-title">{t('ipProfilePeers', { n: 10, total: data.peerCount })}</div>
              {topPeers.length === 0 && <Empty description={t('noDataShort')} image={Empty.PRESENTED_IMAGE_SIMPLE} />}
              {topPeers.map((p) => (
                <BarRow key={p.peer} label={p.peer} bytes={p.bytes} maxBytes={maxPeerBytes} fill="rgba(53,224,255,0.16)" />
              ))}
            </div>
            <div className="ip-profile-section-card">
              <div className="ip-profile-col-title">{t('ipProfileServices', { n: 10, total: data.totalServiceCount })}</div>
              {topServices.length === 0 && <Empty description={t('noDataShort')} image={Empty.PRESENTED_IMAGE_SIMPLE} />}
              {topServices.map((s) => (
                <BarRow key={s.service} label={s.service} bytes={s.bytes} maxBytes={maxSvcBytes} dpi={s.dpi} fill="rgba(155,140,255,0.18)" />
              ))}
            </div>
          </div>

          <h3 className="ip-profile-section-title">{t('ipProfileAlerts', { n: 20, total: data.totalAlertCount })}</h3>
          <div className="ip-profile-section-card">
            {alerts.length === 0 && <div className="ip-profile-empty-note">{t('ipProfileNoAlerts')}</div>}
            {alerts.map((a, i) => (
              <div className="ip-profile-alert-item" key={i}>
                <AlertKindBadge kind={a.kind} />
                <span>{a.kind === 'volume' ? formatBytes(a.volumeBytes ?? 0) : `${t('threatsColPeers')} ${a.distinctPeers ?? 0}`}</span>
                <span className="ip-profile-alert-time">{new Date(a.time).toLocaleString()}</span>
              </div>
            ))}
          </div>
        </div>
      )}
    </Drawer>
  )
}
