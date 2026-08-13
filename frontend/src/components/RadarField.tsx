
export function RadarField() {
  return (
    <div className="radar-field">
      <svg viewBox="0 0 1000 1000">
        <circle className="radar-ring radar-pulse" cx="500" cy="500" r="180" strokeWidth="1" />
        <circle className="radar-ring radar-pulse" cx="500" cy="500" r="320" strokeWidth="1" style={{ animationDelay: '1s' }} />
        <circle className="radar-ring radar-pulse" cx="500" cy="500" r="460" strokeWidth="1" style={{ animationDelay: '2s' }} />
        <g className="radar-sweep">
          <path d="M500 500 L500 20 A480 480 0 0 1 830 150 Z" fill="url(#radarSweepGrad)" opacity="0.45" />
        </g>
        <defs>
          <linearGradient id="radarSweepGrad" x1="0" y1="0" x2="1" y2="0">
            <stop offset="0%" stopColor="#35e0ff" stopOpacity="0" />
            <stop offset="100%" stopColor="#35e0ff" stopOpacity="0.35" />
          </linearGradient>
        </defs>
      </svg>
    </div>
  )
}
