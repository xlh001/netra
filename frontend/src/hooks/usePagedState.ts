import { useRef, useState } from 'react'

const DEFAULT_PAGE_SIZE = 10

export function usePagedState(_ceiling?: number, _extraChromePx?: number) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [page, setPage] = useState(0)
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE)

  function onPageChange(p: number, ps: number) {
    setPage(p)
    setPageSize(ps)
  }

  return { containerRef, page, pageSize, setPage, onPageChange }
}
