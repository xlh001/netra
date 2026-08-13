import { useEffect, useRef, useState } from 'react'
import { useFitPageSize } from './useFitPageSize'

export function usePagedState(ceiling: number, extraChromePx = 0) {
  const { containerRef, fit } = useFitPageSize(ceiling, extraChromePx)
  const [page, setPage] = useState(0)
  const [pageSize, setPageSize] = useState(fit)
  const sizeTouched = useRef(false)

  useEffect(() => {
    if (!sizeTouched.current) setPageSize(fit)
  }, [fit])

  function onPageChange(p: number, ps: number) {
    if (ps !== pageSize) sizeTouched.current = true
    setPage(p)
    setPageSize(ps)
  }

  return { containerRef, page, pageSize, setPage, onPageChange }
}
