import { useT } from '../../i18n/context'
import { RankList } from '../RankList'
import { AssetLabel } from '../../lib/trafficColumns'
import type { IPStat } from '../../api/types'

export function IPsRanking({ items }: { items: IPStat[] }) {
  const t = useT()
  return (
    <div className="panel flex1">
      <div className="panel-head">
        <h2>
          <span className="panel-head-title">{t('ipsTitle')}</span>
        </h2>
      </div>
      <RankList
        items={items}
        labelFn={(ip) => ip.label || ip.ip}
        titleFn={(ip) => (ip.label ? `${ip.label} (${ip.ip})` : ip.ip)}
        renderLabel={(ip) => <AssetLabel label={ip.label} value={ip.ip} />}
        color="#35e0ff"
      />
    </div>
  )
}
