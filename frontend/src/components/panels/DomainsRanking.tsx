import { useT } from '../../i18n/context'
import { RankList } from '../RankList'
import type { DomainStat } from '../../api/types'

export function DomainsRanking({ items }: { items: DomainStat[] }) {
  const t = useT()
  return (
    <div className="panel flex1">
      <div className="panel-head">
        <h2>
          <span className="panel-head-title">{t('domainsTitle')}</span>
        </h2>
      </div>
      <RankList items={items} labelFn={(d) => d.domain} color="#35e0ff" />
    </div>
  )
}
