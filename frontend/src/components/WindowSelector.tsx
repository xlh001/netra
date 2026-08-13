import { useI18n } from '../i18n/context'
import type { Window } from '../api/types'

const WINDOWS: { value: Window; key: 'w15' | 'w30' | 'w1h' }[] = [
  { value: '15m', key: 'w15' },
  { value: '30m', key: 'w30' },
  { value: '1h', key: 'w1h' },
]

export function WindowSelector({ value, onChange }: { value: Window; onChange: (w: Window) => void }) {
  const { t } = useI18n()
  return (
    <div className="control-group">
      <span className="control-label">{t('rangeLabel')}</span>
      <div className="segmented">
        {WINDOWS.map((w) => (
          <button key={w.value} type="button" className={w.value === value ? 'active' : ''} onClick={() => onChange(w.value)}>
            {t(w.key)}
          </button>
        ))}
      </div>
    </div>
  )
}
