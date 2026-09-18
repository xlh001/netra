import { useState, type FormEvent } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useAuth } from '../auth/context'

function Mark() {
  return (
    <svg viewBox="0 0 48 48" fill="none" aria-hidden="true">
      <path d="M7 21V8h13M28 8h13v13M7 27v13h13M28 40h13V27" stroke="currentColor" strokeWidth="3.2" strokeLinecap="round" strokeLinejoin="round" />
      <circle cx="24" cy="24" r="3.5" fill="#E8A84B" />
    </svg>
  )
}

export function Login() {
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
    <div className="login-page">
      <svg className="login-background" viewBox="0 0 1672 941" preserveAspectRatio="xMidYMid slice" aria-hidden="true">
        <g className="login-background-lines">
          <path d="M857 0A203 203 0 0 0 1092 253" />
          <path d="M0 683A213 213 0 0 1 289 877" />
          <path d="M790 72v45M768 94h44M1016 104v284M942 75h119M905 320h156" />
          <path d="M70 690v92M0 760h205M372 720v40M352 740h40M835 730h169M920 698v63M953 817v52M1029 628v160" />
        </g>
      </svg>
      <main className="login-layout">
        <section className="login-intro" aria-label="Netra platform overview">
          <div className="login-brand"><span className="login-brand-mark"><Mark /></span><span>NETRA</span></div>
          <div className="login-eyebrow">KERNEL-NATIVE TRAFFIC OBSERVABILITY</div>
          <h1>Keep traffic in kernel.<br /><em>See what matters.</em></h1>
          <div className="login-capability-stage" aria-label="Netra capabilities">
            <article className="login-capability-card">
              <span className="login-capability-ordinal">01</span>
              <div className="login-capability-copy"><span>LIGHTWEIGHT</span><strong>One binary.<br />Zero dependencies.</strong></div>
              <small>DEPLOY WITHOUT A STACK</small>
            </article>
            <article className="login-capability-card">
              <span className="login-capability-ordinal">02</span>
              <div className="login-capability-copy"><span>HIGH-PERFORMANCE</span><strong>XDP-native capture.<br />Zero packet loss.</strong></div>
              <small>BUILT FOR HIGH-BANDWIDTH MIRROR PORTS</small>
            </article>
            <article className="login-capability-card">
              <span className="login-capability-ordinal">03</span>
              <div className="login-capability-copy"><span>AI-GROUNDED</span><strong>Ask questions about<br />your actual traffic.</strong></div>
              <small>ANSWERS BACKED BY OBSERVED DATA</small>
            </article>
            <article className="login-capability-card">
              <span className="login-capability-ordinal">04</span>
              <div className="login-capability-copy"><span>MCP-EXTENSIBLE</span><strong>Extend AI with your<br />own tools and data.</strong></div>
              <small>GROUNDED IN YOUR TRAFFIC</small>
            </article>
            <div className="login-capability-pager" aria-hidden="true"><i /><i /><i /><i /></div>
          </div>
        </section>
        <section className="login-access">
          <form className="login-form" onSubmit={handleSubmit}>
            <div className="login-form-brand"><span className="login-form-mark"><Mark /></span><span>NETRA</span></div>
            <div className="login-form-head"><div><h2>Sign in</h2><p>Access kernel-native observability</p></div></div>
            <label className="login-field"><span>USERNAME</span><input type="text" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" required /></label>
            <label className="login-field"><span>PASSWORD</span><input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required /></label>
            {error && <div className="login-err">Sign-in failed: {error}</div>}
            <button type="submit" className="login-btn" disabled={submitting}>{submitting ? 'SIGNING IN…' : 'SIGN IN'}</button>
          </form>
        </section>
      </main>
    </div>
  )
}
