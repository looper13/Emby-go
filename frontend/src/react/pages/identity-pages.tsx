import { Plus, RotateCcw, Save, Shield, Trash2, UserRound } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { accountApi, type LibraryPolicy } from '../../api/account'
import { usersApi, type ManagedUser } from '../../api/users'
import { useAdminRead } from '../hooks/use-admin-read'
import { useAppRuntimeStore } from '../app-store'
import { useReactAuthStore } from '../auth-store'
import { useReactMediaWallStore } from '../media-wall-store'
import { useConfirmLeave } from '../hooks/use-confirm-leave'
import { PageHeading, PageState } from './PageState'
import './basic-pages.css'
import './identity-pages.css'

const equalFolders = (a: string[], b: string[]) => JSON.stringify([...a].sort()) === JSON.stringify([...b].sort())

function passwordError(password: string) {
  return Array.from(password).length < 9 || new TextEncoder().encode(password).length > 72
}

export function AccountPage() {
  const { data, setData, loading, error, load, active } = useAdminRead(accountApi.get)
  const auth = useReactAuthStore()
  const toast = useAppRuntimeStore((state) => state.showToast)
  const navigate = useNavigate()
  const clearWall = useReactMediaWallStore((state) => state.clear)
  const [username, setUsername] = useState('')
  const [folders, setFolders] = useState<string[]>([])
  const [allFolders, setAllFolders] = useState(true)
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [savingPolicy, setSavingPolicy] = useState(false)
  const [savingCredentials, setSavingCredentials] = useState(false)
  const [formError, setFormError] = useState('')
  useEffect(() => {
    if (!data) return
    setUsername(data.user.Name)
    setAllFolders(data.policy.EnableAllFolders)
    setFolders([...data.policy.EnabledFolders])
  }, [data?.user.Name, data?.policy.EnableAllFolders, data?.policy.EnabledFolders])
  const policyDirty = Boolean(data && (allFolders !== data.policy.EnableAllFolders || !equalFolders(folders, data.policy.EnabledFolders)))
  const credentialsDirty = Boolean(data && (username !== data.user.Name || currentPassword || newPassword || confirmation))
  const allowLeave = useConfirmLeave(policyDirty || credentialsDirty, '放弃未保存的账号设置？')

  async function savePolicy(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!data || savingPolicy || !auth.user?.Policy.IsAdministrator) return
    setSavingPolicy(true)
    setFormError('')
    const policy: LibraryPolicy = { EnableAllFolders: allFolders, EnabledFolders: [...folders] }
    try {
      await accountApi.savePolicy(data.user.Id, policy)
      if (!active()) return
      setData(current => current ? { ...current, policy } : current)
      toast('媒体库权限已保存')
      clearWall()
    } catch (cause) { setFormError(cause instanceof Error ? cause.message : '保存权限失败') }
    finally { setSavingPolicy(false) }
  }

  async function saveCredentials(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!data || savingCredentials) return
    setFormError('')
    if (policyDirty) { setFormError('请先保存或恢复媒体库权限'); return }
    if (newPassword !== confirmation) { setFormError('两次输入的新密码不一致'); return }
    if (newPassword && passwordError(newPassword)) { setFormError('新密码至少 9 个字符且不超过 72 字节'); return }
    if (!window.confirm('保存账号后，所有已登录设备都需要重新登录。继续？')) return
    const token = auth.token
    setSavingCredentials(true)
    try {
      const nextUsername = username.trim()
      await accountApi.update({ Username: nextUsername, CurrentPassword: currentPassword, NewPassword: newPassword })
      if (!active() || auth.token !== token) return
      setUsername(nextUsername)
      setData(current => current ? { ...current, user: { ...current.user, Name: nextUsername } } : current)
      setCurrentPassword(''); setNewPassword(''); setConfirmation('')
      auth.clearSession(token)
      toast('账号已更新，请重新登录')
      allowLeave(true)
      try { await navigate('/login', { replace: true }) }
      finally { allowLeave(false) }
    } catch (cause) { setFormError(cause instanceof Error ? cause.message : '保存账号失败') }
    finally { setSavingCredentials(false) }
  }

  const busy = savingPolicy || savingCredentials
  return <div className="react-page-auth-sections">
    <PageState loading={loading && !data} error={error} empty={false} retry={() => void load()} label="账号" />
    {data && <>
      {formError && <p className="react-page-banner danger" role="alert">{formError}</p>}
      <section className="react-page-panel"><PageHeading title="媒体库访问权限" description={auth.user?.Policy.IsAdministrator ? '变更会清空本地浏览快照，下次浏览时重新读取授权内容。' : '管理员可以调整媒体库授权。'} />
        {auth.user?.Policy.IsAdministrator ? <form onSubmit={(event) => void savePolicy(event)}>
          <label className="react-page-check"><input type="checkbox" checked={allFolders} disabled={busy} onChange={(event) => setAllFolders(event.currentTarget.checked)} />允许访问所有媒体库</label>
          <fieldset className="react-page-access-list" disabled={allFolders || busy}><legend>指定媒体库</legend>
            {data.libraries.map((library) => <label className="react-page-check" key={library.Id}><input type="checkbox" checked={folders.includes(library.Id)} onChange={(event) => { const checked = event.currentTarget.checked; setFolders(current => checked ? [...current, library.Id] : current.filter(id => id !== library.Id)) }} />{library.Name}</label>)}
            {!data.libraries.length && <p className="react-page-muted">暂无媒体库</p>}
          </fieldset>
          {!allFolders && !folders.length && <p className="react-page-banner warning" role="status">未授权任何媒体库。</p>}
          <div className="react-page-footer"><button className="react-primary-button" disabled={busy || !policyDirty}><Save size={14} /><span>保存权限</span></button>
            <button className="react-detail-action" type="button" disabled={busy || !policyDirty} onClick={() => { setAllFolders(data.policy.EnableAllFolders); setFolders([...data.policy.EnabledFolders]) }}><RotateCcw size={14} />恢复</button>
          </div>
        </form> : data.libraries.length ? <ul>{data.libraries.map((library) => <li key={library.Id}>{library.Name}</li>)}</ul> : <p className="react-page-muted">未授权任何媒体库。</p>}
      </section>
      <section className="react-page-panel"><PageHeading title="登录账号" description="修改用户名或密码会让其他设备上的登录状态失效。" />
        <form className="react-page-form" onSubmit={(event) => void saveCredentials(event)}>
          <label className="react-page-field">用户名<input autoComplete="username" minLength={3} maxLength={64} required value={username} disabled={busy} onChange={(event) => setUsername(event.currentTarget.value)} /></label>
          <label className="react-page-field">当前密码<input autoComplete="current-password" type="password" required value={currentPassword} disabled={busy} onChange={(event) => setCurrentPassword(event.currentTarget.value)} /></label>
          <label className="react-page-field">新密码（可选）<input autoComplete="new-password" type="password" minLength={9} value={newPassword} disabled={busy} onChange={(event) => setNewPassword(event.currentTarget.value)} /></label>
          <label className="react-page-field">确认新密码<input autoComplete="new-password" type="password" required={Boolean(newPassword)} value={confirmation} disabled={busy} onChange={(event) => setConfirmation(event.currentTarget.value)} /></label>
          <div className="react-page-footer"><button className="react-primary-button" disabled={busy || !currentPassword}><Save size={14} /><span>{savingCredentials ? '正在保存…' : '保存账号'}</span></button></div>
        </form>
      </section>
    </>}
  </div>
}

export function UsersPage() {
  const { data, setData, loading, error, load, loadData, active } = useAdminRead(usersApi.list)
  const auth = useReactAuthStore()
  const toast = useAppRuntimeStore((state) => state.showToast)
  const clearWall = useReactMediaWallStore((state) => state.clear)
  const navigate = useNavigate()
  const [selectedId, setSelectedId] = useState('')
  const [creating, setCreating] = useState(false)
  const [busy, setBusy] = useState(false)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirmation, setConfirmation] = useState('')
  const [disabled, setDisabled] = useState(false)
  const [allFolders, setAllFolders] = useState(false)
  const [folders, setFolders] = useState<string[]>([])
  const [formError, setFormError] = useState('')
  const selected = useMemo(() => data?.users.find((user) => user.Id === selectedId), [data, selectedId])
  useEffect(() => {
    if (!data) return
    if (creating || data.users.some((user) => user.Id === selectedId)) return
    const first = data.users[0]
    setSelectedId(first?.Id || '')
    if (first) resetUser(first)
  }, [data, creating, selectedId])

  function resetUser(user = selected, isNew = creating) {
    setUsername(isNew ? '' : user?.Name || '')
    setDisabled(isNew ? false : user?.Policy.IsDisabled || false)
    setPassword(''); setConfirmation('')
    setAllFolders(user?.Policy.EnableAllFolders || false)
    setFolders([...(user?.Policy.EnabledFolders || [])])
    setFormError('')
  }
  function confirmSwitch() {
    const dirty = creating ? Boolean(username || password || confirmation)
      : Boolean(selected && (username !== selected.Name || disabled !== selected.Policy.IsDisabled || password || confirmation
        || allFolders !== selected.Policy.EnableAllFolders || !equalFolders(folders, selected.Policy.EnabledFolders)))
    return !dirty || window.confirm('放弃未保存的用户设置？')
  }
  function selectUser(id: string, isNew = false) {
    if (busy || !confirmSwitch()) return
    const user = data?.users.find((entry) => entry.Id === id)
    setSelectedId(id); setCreating(isNew); resetUser(user, isNew)
  }
  function replaceUser(user: ManagedUser) {
    setData(current => current ? { ...current, users: current.users.map(item => item.Id === user.Id ? user : item) } : current)
  }
  async function refreshUsers() {
    if (busy || !confirmSwitch()) return
    const next = await loadData()
    if (!next || !active()) return
    const user = next.users.find(item => item.Id === selectedId) ?? next.users[0]
    setSelectedId(user?.Id || '')
    setCreating(false)
    if (user) resetUser(user, false)
  }
  async function saveAccount(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!data || busy || !auth.user?.Policy.IsAdministrator) return
    const name = username.trim()
    setFormError('')
    if (Array.from(name).length < 3 || Array.from(name).length > 64) { setFormError('用户名须为 3-64 个字符'); return }
    if (password !== confirmation) { setFormError('两次输入的密码不一致'); return }
    if ((creating || password) && passwordError(password)) { setFormError('密码至少 9 个字符且不超过 72 字节'); return }
    const user = selected
    if (!creating && !user) return
    if (!creating && !window.confirm(`保存后，${user!.Name} 的已登录会话将失效。继续？`)) return
    const token = auth.token
    setBusy(true)
    try {
      if (creating) {
        const created = await usersApi.create(name, password)
        if (!active()) return
        await load(); setSelectedId(created.Id); setCreating(false); resetUser(created, false); toast('用户已创建')
      } else if (user) {
        await usersApi.update(user.Id, { Username: name, NewPassword: password, IsDisabled: disabled })
        if (!active()) return
        replaceUser({ ...user, Name: name, Policy: { ...user.Policy, IsDisabled: disabled } })
        if (user.Id === auth.user?.Id) {
          setUsername(name)
          auth.clearSession(token); toast('账号已更新，请重新登录')
          allowLeave(true)
          try { await navigate('/login', { replace: true }) }
          finally { allowLeave(false) }
        } else { setPassword(''); setConfirmation(''); toast('用户已保存') }
      }
    } catch (cause) { if (active()) setFormError(cause instanceof Error ? cause.message : '保存账号失败') }
    finally { setBusy(false) }
  }
  async function savePolicy(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!selected || busy || !auth.user?.Policy.IsAdministrator) return
    setBusy(true); setFormError('')
    const policy = { EnableAllFolders: allFolders, EnabledFolders: [...folders] }
    try {
      await usersApi.savePolicy(selected.Id, policy)
      if (!active()) return
      setData(current => current ? { ...current, users: current.users.map(user => user.Id === selected.Id
        ? { ...user, Policy: { ...user.Policy, ...policy } } : user) } : current)
      if (selected.Id === auth.user?.Id) clearWall()
      toast('媒体库权限已保存')
    } catch (cause) { if (active()) setFormError(cause instanceof Error ? cause.message : '保存权限失败') }
    finally { setBusy(false) }
  }
  async function removeUser() {
    if (!selected || selected.Id === '1' || busy || !auth.user?.Policy.IsAdministrator
      || !window.confirm(`删除用户 ${selected.Name} 及其收藏和播放记录？媒体文件不会删除。`)) return
    setBusy(true); setFormError('')
    try { await usersApi.remove(selected.Id); if (active()) { await load(); setSelectedId(''); setCreating(false); toast('用户已删除') } }
    catch (cause) { setFormError(cause instanceof Error ? cause.message : '删除用户失败') }
    finally { setBusy(false) }
  }
  const credentialDirty = creating ? Boolean(username || password || confirmation)
    : Boolean(selected && (username !== selected.Name || disabled !== selected.Policy.IsDisabled || password || confirmation))
  const policyDirty = Boolean(selected && (allFolders !== selected.Policy.EnableAllFolders || !equalFolders(folders, selected.Policy.EnabledFolders)))
  const allowLeave = useConfirmLeave(credentialDirty || policyDirty, '放弃未保存的用户设置？', busy)

  return <div>
    <div className="react-page-toolbar"><span className="react-page-muted">{data?.users.length ?? 0} 个用户</span><div className="react-page-actions">
      <button className="react-icon-button" type="button" title="刷新用户" aria-label="刷新用户" disabled={busy || loading} onClick={() => void refreshUsers()}><RotateCcw size={15} /></button>
      <button className="react-primary-button" type="button" disabled={busy || !data} onClick={() => selectUser('', true)}><Plus size={14} /><span>新增用户</span></button>
    </div></div>
    <PageState loading={loading && !data} error={error} empty={false} retry={() => void load()} label="用户" />
    {formError && <p className="react-page-banner danger" role="alert">{formError}</p>}
    {data && <div className="react-users-workspace">
      <nav className="react-users-list" aria-label="用户列表">{data.users.map((user) => {
        const Icon = user.Policy.IsAdministrator ? Shield : UserRound
        return <button key={user.Id} type="button" className="react-user-row" aria-pressed={!creating && selectedId === user.Id} disabled={busy}
          onClick={() => selectUser(user.Id)}><Icon size={17} /><span><strong>{user.Name}</strong><small>{user.Policy.IsAdministrator ? '管理员' : '普通用户'} · {user.Policy.IsDisabled ? '已禁用' : '启用'}</small></span></button>
      })}</nav>
      {(creating || selected) && <div className="react-user-editor">
        <PageHeading title={creating ? '新增用户' : selected?.Name || ''} description={creating ? '创建一个 Emby-go 本地账号。' : `用户 ID ${selectedId}`} />
        <section className="react-page-panel"><form className="react-page-form" data-form="credentials" onSubmit={(event) => void saveAccount(event)}>
          <label className="react-page-field">用户名<input name="username" required minLength={3} maxLength={64} value={username} disabled={busy} onChange={(event) => setUsername(event.currentTarget.value)} /></label>
          {!creating && selectedId !== '1' && <label className="react-page-check"><input name="disabled" type="checkbox" checked={disabled} disabled={busy} onChange={(event) => setDisabled(event.currentTarget.checked)} />禁用账号</label>}
          <label className="react-page-field">{creating ? '密码' : '重置密码（可选）'}<input name="password" type="password" autoComplete="new-password" required={creating} minLength={9} value={password} disabled={busy} onChange={(event) => setPassword(event.currentTarget.value)} /></label>
          <label className="react-page-field">确认密码<input name="confirmation" type="password" autoComplete="new-password" required={creating || Boolean(password)} value={confirmation} disabled={busy} onChange={(event) => setConfirmation(event.currentTarget.value)} /></label>
          <div className="react-page-footer"><button className="react-primary-button" disabled={busy || (!creating && !credentialDirty)}><Save size={14} /><span>{creating ? '创建用户' : '保存账号'}</span></button>
            <button className="react-icon-button" type="button" title="恢复账号设置" aria-label="恢复账号设置" disabled={busy || !credentialDirty} onClick={() => resetUser()}><RotateCcw size={15} /></button>
          </div>
        </form></section>
        {!creating && selected && <>
          <section className="react-page-panel"><h3>媒体库访问权限</h3><form onSubmit={(event) => void savePolicy(event)}>
            <label className="react-page-check"><input type="checkbox" checked={allFolders} disabled={busy} onChange={(event) => setAllFolders(event.currentTarget.checked)} />允许访问所有媒体库</label>
            <fieldset className="react-page-access-list" disabled={allFolders || busy}><legend>指定媒体库</legend>
              {data.libraries.map((library) => <label className="react-page-check" key={library.Id}><input type="checkbox" checked={folders.includes(library.Id)} onChange={(event) => { const checked = event.currentTarget.checked; setFolders(current => checked ? [...current, library.Id] : current.filter(id => id !== library.Id)) }} />{library.Name}</label>)}
              {!data.libraries.length && <p className="react-page-muted">暂无媒体库</p>}
            </fieldset>
            {!allFolders && !folders.length && <p className="react-page-banner warning">未授权任何媒体库。</p>}
            <div className="react-page-footer"><button className="react-primary-button" disabled={busy || !policyDirty}><Save size={14} /><span>保存权限</span></button>
              <button className="react-icon-button" type="button" title="恢复媒体库权限" aria-label="恢复媒体库权限" disabled={busy || !policyDirty} onClick={() => { setAllFolders(selected.Policy.EnableAllFolders); setFolders([...selected.Policy.EnabledFolders]) }}><RotateCcw size={15} /></button>
            </div>
          </form></section>
          {selected.Id !== '1' && <button className="react-detail-action" type="button" disabled={busy} onClick={() => void removeUser()}><Trash2 size={15} /><span>删除用户</span></button>}
        </>}
      </div>}
    </div>}
  </div>
}
