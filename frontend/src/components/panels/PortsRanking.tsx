import { useT } from '../../i18n/context'
import { RankList } from '../RankList'
import type { PortStat } from '../../api/types'

export function PortsRanking({ items }: { items: PortStat[] }) {
  const t = useT()
  return (
    <div className="panel flex1">
      <div className="panel-head">
        <h2>
          <span className="panel-head-title">{t('portsTitle')}</span>
        </h2>
      </div>
      <RankList
        items={items}
        labelFn={(p) => `${(p.proto || '').toUpperCase()}/${p.port}${p.service ? ' ' + p.service : ''}`}
        color="#4FD0C0"
      />
    </div>
  )
}
