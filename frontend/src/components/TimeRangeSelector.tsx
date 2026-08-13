import { DatePicker } from 'antd'
import dayjs from 'dayjs'
import { useI18n } from '../i18n/context'
import type { FlowExplorerWindow, TimeRange } from '../api/types'

const { RangePicker } = DatePicker

const WINDOWS: { value: FlowExplorerWindow; key: 'w15' | 'w30' | 'w1h' | 'w1d' | 'w15d' | 'w1mo' }[] = [
  { value: '15m', key: 'w15' },
  { value: '30m', key: 'w30' },
  { value: '1h', key: 'w1h' },
  { value: '1d', key: 'w1d' },
  { value: '15d', key: 'w15d' },
  { value: '1mo', key: 'w1mo' },
]

export function TimeRangeSelector({ value, onChange }: { value: TimeRange; onChange: (r: TimeRange) => void }) {
  const { t } = useI18n()
  const isCustom = value.kind === 'custom'

  return (
    <div className="control-group">
      <span className="control-label">{t('rangeLabel')}</span>
      <div className="segmented">
        {WINDOWS.map((w) => (
          <button
            key={w.value}
            type="button"
            className={!isCustom && value.window === w.value ? 'active' : ''}
            onClick={() => onChange({ kind: 'window', window: w.value })}
          >
            {t(w.key)}
          </button>
        ))}
        <button
          type="button"
          className={isCustom ? 'active' : ''}
          onClick={() => {
            if (isCustom) return
            const to = dayjs()
            const from = to.subtract(15, 'minute')
            onChange({ kind: 'custom', from: from.unix(), to: to.unix() })
          }}
        >
          {t('rangeCustom')}
        </button>
      </div>
      {isCustom && (
        <RangePicker
          showTime
          size="small"
          allowClear={false}
          value={[dayjs.unix(value.from), dayjs.unix(value.to)]}
          placeholder={[t('rangeCustomPlaceholderStart'), t('rangeCustomPlaceholderEnd')]}
          onChange={(dates) => {
            if (dates && dates[0] && dates[1]) {
              onChange({ kind: 'custom', from: dates[0].unix(), to: dates[1].unix() })
            }
          }}
        />
      )}
    </div>
  )
}
