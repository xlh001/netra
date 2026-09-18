import { useId } from 'react'

export function AiMark() {
  const sweep = 'aisweep-' + useId().replace(/:/g, '')
  return (
    <svg className="ai-mark" viewBox="0 0 48 48" fill="none">
      <defs>
        <linearGradient id={sweep} gradientUnits="userSpaceOnUse" x1="-14" y1="0" x2="14" y2="0">
          <stop offset="0" stopColor="#5B8DEF" />
          <stop offset=".5" stopColor="#FFFFFF" />
          <stop offset="1" stopColor="#5B8DEF" />
          <animateTransform attributeName="gradientTransform" type="translate" from="-30 0" to="70 0" dur="1.8s" repeatCount="indefinite" />
        </linearGradient>
      </defs>
      <path className="ai-mark-spark" fill={`url(#${sweep})`} d="M20 8 L23 20 L35 23 L23 26 L20 40 L17 26 L5 23 L17 20 Z" />
      <path className="ai-mark-spark-sm" fill={`url(#${sweep})`} d="M37 10 L38.6 15.4 L44 17 L38.6 18.6 L37 24 L35.4 18.6 L30 17 L35.4 15.4 Z" />
    </svg>
  )
}
