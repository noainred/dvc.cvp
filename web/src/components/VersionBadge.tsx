import { useEffect, useState } from 'react'
import type { UpgradeStatus } from '../types'
import { api } from '../api'

// VersionBadge shows the portal's current build version and, when a newer
// release is detected, offers a one-click self-upgrade (which restarts the
// server into the new binary).
export function VersionBadge({ admin = true }: { admin?: boolean }) {
  const [st, setSt] = useState<UpgradeStatus | null>(null)
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState<string | null>(null)

  useEffect(() => {
    api.version().then(setSt).catch(() => {})
  }, [])

  const check = async () => {
    setBusy(true)
    setMsg(null)
    try {
      setSt(await api.checkUpgrade())
    } catch (e) {
      setMsg(String(e))
    } finally {
      setBusy(false)
    }
  }

  const apply = async () => {
    if (!confirm(`버전 ${st?.latest} 로 업그레이드하고 서버를 재시작합니다. 계속할까요?`)) return
    setBusy(true)
    setMsg(null)
    try {
      await api.applyUpgrade()
      setMsg('업그레이드 중… 곧 재시작됩니다')
      // The server re-execs into the new binary; reload once it is back up.
      setTimeout(() => location.reload(), 6000)
    } catch (e) {
      setMsg(String(e))
      setBusy(false)
    }
  }

  if (!st) return <span className="version">·</span>

  return (
    <div className="version-badge">
      <span className="version" title={`commit ${st.commit} · built ${st.buildTime}`}>
        {st.current}
      </span>
      {!admin ? null : st.upgradeAvailable ? (
        <button className="upgrade-pill" disabled={busy || !st.enabled} onClick={apply}>
          ▲ {st.latest} 업그레이드
        </button>
      ) : (
        <button className="check-btn" disabled={busy} onClick={check} title="최신 릴리스 확인">
          {busy ? '확인중…' : '업데이트 확인'}
        </button>
      )}
      {st.autoApply && <span className="auto-tag" title="새 버전 자동 적용">AUTO</span>}
      {msg && <span className="upgrade-msg">{msg}</span>}
    </div>
  )
}
