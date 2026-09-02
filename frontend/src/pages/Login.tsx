import { useState, type CSSProperties, type FormEvent } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '../auth/context'
import { useI18n } from '../i18n/context'
import { Logo } from '../components/Logo'
import { RadarField } from '../components/RadarField'

function LoginLanguageSwitch() {
  const { language, setLanguage } = useI18n()
  return (
    <div
      style={{ position: 'absolute', top: '18px', right: '20px', zIndex: 2, display: 'flex', gap: '2px' }}
      title="Display language preview only -- the actual system language is set by an administrator under Settings."
    >
      {(['zh', 'en'] as const).map((lang) => (
        <button
          key={lang}
          type="button"
          onClick={() => setLanguage(lang)}
          style={{
            background: language === lang ? 'var(--panel-2, rgba(53,224,255,0.14))' : 'transparent',
            border: '1px solid var(--line)',
            borderRadius: '5px',
            padding: '3px 8px',
            fontSize: '11px',
            fontFamily: 'var(--font-mono)',
            color: language === lang ? 'var(--scan)' : 'var(--ink-dim)',
            cursor: 'pointer',
          }}
        >
          {lang === 'zh' ? '中文' : 'EN'}
        </button>
      ))}
    </div>
  )
}

const inputStyle: CSSProperties = {
  background: 'var(--panel)',
  border: '1px solid var(--line)',
  borderRadius: '6px',
  padding: '7px 10px',
  color: 'var(--ink)',
  fontFamily: 'var(--font-mono)',
  fontSize: '12px',
}

export function Login() {
  const { t } = useI18n()
  const { login } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setSubmitting(true)
    setError('')
    try {
      await login(username, password)
      const from = (location.state as { from?: string } | null)?.from || '/'
      navigate(from, { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div style={{ height: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center', position: 'relative' }}>
      <RadarField />
      <LoginLanguageSwitch />
      <form onSubmit={handleSubmit} className="panel" style={{ width: '320px', padding: '28px 24px', position: 'relative', zIndex: 1 }}>
        <div style={{ textAlign: 'center', marginBottom: '22px' }}>
          <Logo />
          <h1 style={{ margin: '10px 0 2px', fontSize: '18px', fontWeight: 700, letterSpacing: '4px', textTransform: 'uppercase', color: 'var(--scan)' }}>NETRA</h1>
          <div style={{ fontSize: '9.5px', color: 'var(--ink-dim)', fontFamily: 'var(--font-mono)', textTransform: 'uppercase', letterSpacing: '.07em' }}>
            {t('loginSubtitle')}
          </div>
        </div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: '14px' }}>
          <label style={{ display: 'flex', flexDirection: 'column', gap: '4px' }}>
            <span className="control-label" style={{ margin: 0 }}>
              {t('loginUsername')}
            </span>
            <input type="text" value={username} onChange={(e) => setUsername(e.target.value)} autoFocus style={inputStyle} required />
          </label>
          <label style={{ display: 'flex', flexDirection: 'column', gap: '4px' }}>
            <span className="control-label" style={{ margin: 0 }}>
              {t('loginPassword')}
            </span>
            <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} style={inputStyle} required />
          </label>
          {error && (
            <div style={{ color: 'var(--rose)', fontSize: '11px' }}>
              {t('loginFailed')}
              {error}
            </div>
          )}
          <button type="submit" className="icon-btn" disabled={submitting} style={{ width: '100%', padding: '9px 0', marginTop: '4px', fontSize: '12px' }}>
            {t('loginButton')}
          </button>
        </div>
      </form>
    </div>
  )
}
