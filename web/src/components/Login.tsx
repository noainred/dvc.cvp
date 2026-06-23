import { useState } from 'react'
import { auth } from '../api'

// Login is shown when access control is enabled and the user is not yet
// authenticated. On success it stores the session token and notifies the app.
export function Login({ onSuccess }: { onSuccess: () => void }) {
  const [u, setU] = useState('')
  const [p, setP] = useState('')
  const [err, setErr] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setErr(null)
    try {
      await auth.login(u, p)
      onSuccess()
    } catch {
      setErr('로그인 실패 — 아이디 또는 비밀번호를 확인하세요.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="login-wrap">
      <form className="login-card" onSubmit={submit}>
        <div className="login-brand">
          <span className="brand-mark">◢◤</span> dvc.cvp
        </div>
        <div className="login-sub">Arista CloudVision 통합 관제</div>
        <input placeholder="아이디" value={u} onChange={(e) => setU(e.target.value)} autoFocus />
        <input placeholder="비밀번호" type="password" value={p} onChange={(e) => setP(e.target.value)} />
        {err && <div className="login-err">{err}</div>}
        <button disabled={busy || !u || !p}>{busy ? '로그인 중…' : '로그인'}</button>
      </form>
    </div>
  )
}
