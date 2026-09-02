import { useMemo } from 'react'
import { useI18n } from '../../i18n/context'
import { RankList } from '../RankList'
import { TOPN } from '../../api/client'
import { aggregateByCountry } from '../../lib/geo'
import { countryName } from '../../lib/format'
import type { GeoReport } from '../../api/types'

export function CountriesRanking({ geo }: { geo: GeoReport | null }) {
  const { t, language } = useI18n()
  const items = useMemo(() => aggregateByCountry(geo?.points).slice(0, TOPN), [geo])

  return (
    <div className="panel flex1">
      <div className="panel-head">
        <h2>
          <span className="panel-head-title">{t('countriesTitle')}</span>
        </h2>
      </div>
      <RankList items={items} labelFn={(it) => countryName(it.country, language)} color="#35e0ff" />
    </div>
  )
}
