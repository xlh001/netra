import { useT } from '../i18n/context'

export function Pagination({
  page,
  pageSize,
  total,
  onPageChange,
}: {
  page: number
  pageSize: number
  total: number
  onPageChange: (page: number) => void
}) {
  const t = useT()
  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: '10px', justifyContent: 'flex-end', padding: '8px 0' }}>
      <span className="control-label" style={{ margin: 0 }}>
        {t('pageInfo', { page: page + 1, total })}
      </span>
      <button type="button" className="icon-btn" style={{ width: 'auto', padding: '0 10px' }} disabled={page <= 0} onClick={() => onPageChange(page - 1)}>
        {t('pagePrev')}
      </button>
      <button type="button" className="icon-btn" style={{ width: 'auto', padding: '0 10px' }} disabled={page >= totalPages - 1} onClick={() => onPageChange(page + 1)}>
        {t('pageNext')}
      </button>
    </div>
  )
}
