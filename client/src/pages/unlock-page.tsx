import { KeyRound } from 'lucide-react'
import { Page } from '@/components/shell/page'
import { Button } from '@/components/ui/button'
import { CheckOption } from '@/components/ui/checkbox'
import { Field, Input } from '@/components/ui/input'
import { Panel, PanelBody } from '@/components/ui/panel'
import { shortInstanceId } from '@/lib/console'
import type { PingInfo } from '@/lib/utils'

export function UnlockPage(props: {
  deviceLabel: string
  ping: PingInfo | null
  email: string
  password: string
  totp: string
  need2fa: boolean
  rememberPassword: boolean
  setEmail: (value: string) => void
  setPassword: (value: string) => void
  setTotp: (value: string) => void
  setRememberPassword: (value: boolean) => void
  onUnlock: () => void
  onBack: () => void
  busy: boolean
}) {
  return (
    <Page>
      <div className="flex min-h-0 flex-1 items-start justify-center overflow-y-auto px-8 py-10">
        <div className="my-auto w-full max-w-sm">
          <Panel>
            <PanelBody className="space-y-5 p-5">
              <div className="space-y-2">
                <span className="grid size-9 place-items-center rounded-lg bg-accent/15 text-accent-hi">
                  <KeyRound className="size-4" />
                </span>
                <h1 className="text-lg font-semibold text-ink">解锁 {props.deviceLabel}</h1>
                <p className="text-xs leading-relaxed text-faint">
                  实例 <span className="font-mono">{shortInstanceId(props.ping?.instance_id || '')}</span> 已锁定。
                  凭据只发送给这台 dec-server，保存在它的进程内存里。
                </p>
              </div>
              <form
                className="space-y-3"
                onSubmit={(event) => {
                  event.preventDefault()
                  props.onUnlock()
                }}
              >
                <Field label="Bitwarden 邮箱">
                  <Input autoComplete="username" value={props.email} onChange={(e) => props.setEmail(e.target.value)} />
                </Field>
                <Field label="主密码">
                  <Input
                    type="password"
                    autoComplete="current-password"
                    value={props.password}
                    onChange={(e) => props.setPassword(e.target.value)}
                  />
                </Field>
                {props.need2fa && (
                  <Field label="两步验证码" hint="来自验证器或邮件的一次性验证码。">
                    <Input className="tnum" inputMode="numeric" value={props.totp} onChange={(e) => props.setTotp(e.target.value)} />
                  </Field>
                )}
                <CheckOption
                  label="用系统凭据库保存这个连接的主密码"
                  checked={props.rememberPassword}
                  onChange={() => props.setRememberPassword(!props.rememberPassword)}
                />
                <div className="flex gap-2 pt-1">
                  <Button className="flex-1" type="submit" disabled={props.busy}>
                    {props.busy ? '解锁中…' : '解锁'}
                  </Button>
                  <Button type="button" variant="ghost" onClick={props.onBack} disabled={props.busy}>
                    返回
                  </Button>
                </div>
              </form>
            </PanelBody>
          </Panel>
          <p className="mt-3 text-center text-[11px] leading-relaxed text-faint">
            session 只存在于 dec-server 进程内存，默认 1 小时后失效，不落盘。
          </p>
        </div>
      </div>
    </Page>
  )
}
