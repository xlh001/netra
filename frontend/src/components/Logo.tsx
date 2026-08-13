
export function Logo() {
  return (
    <svg className="mark" viewBox="0 0 42 42">
      <circle className="ring" cx="21" cy="21" r="19" />
      <circle className="ring" cx="21" cy="21" r="12" />
      <circle className="pupil" cx="21" cy="21" r="4" />
      <path className="sweep" d="M 21 2 A 19 19 0 0 1 38 14" />
    </svg>
  )
}
