import { FileKey, KeyRound, ShieldAlert } from 'lucide-react'
import { useState } from 'react'
import { Page } from '@/components/shell/page'
import { Button } from '@/components/ui/button'
import { Notice } from '@/components/ui/feedback'
import { Field, Input } from '@/components/ui/input'
import { Panel, PanelBody } from '@/components/ui/panel'

export type CredentialRequestView = {
  request_id: string
  kind: number
  prompt: string
  operation: string
}

// kind 数值与 schema/service/v1/service.proto 的 CredentialRequestKind 对齐：
// 1 登录密码 / 2 私钥口令 / 3 私钥材料 / 4 危险动作确认。
const KIND_SPECS: Record<number, { title: string; icon: typeof KeyRound; hint: string }> = {
  1: {
    title: '提供登录密码',
    icon: KeyRound,
    hint: '密码只交给本机 dec-server 进程内存，用于这一次密码登录，随后以库加密形式存入设备密码条目。',
  },
  2: {
    title: '提供私钥口令',
    icon: KeyRound,
    hint: '口令仅在内存中用于解密校验；私钥去口令入库后以库加密为唯一保护。',
  },
  3: {
    title: '粘贴私钥',
    icon: FileKey,
    hint: '私钥材料只经进程内存流转入库，不会出现在对话转录或日志里。',
  },
  4: {
    title: '确认操作',
    icon: ShieldAlert,
    hint: '该操作会触碰远端设备状态，请核对来源操作与描述后再批准。',
  },
}

export function CredentialPage(props: {
  request: CredentialRequestView
  onSubmit: (input: { secret: string; publicKey: string; approved: boolean }) => void
  onCancel: () => void
  busy: boolean
  error: string
}) {
  const spec = KIND_SPECS[props.request.kind]
  const [secret, setSecret] = useState('')
  const [publicKey, setPublicKey] = useState('')
  const isConfirm = props.request.kind === 4
  const isKeyMaterial = props.request.kind === 3
  const canSubmit = isConfirm || secret.trim() !== ''
  const Icon = spec?.icon || KeyRound
  return (
    <Page>
      <div className="flex min-h-0 flex-1 items-center justify-center overflow-y-auto px-8 py-10">
        <div className="w-full max-w-xl">
          <Panel>
            <PanelBody className="space-y-5 p-5">
              <div className="space-y-2">
                <span className="grid size-9 place-items-center rounded-lg bg-accent/15 text-accent-hi">
                  <Icon className="size-4" />
                </span>
                <h1 className="text-lg font-semibold text-ink">{spec?.title || '凭据请求'}</h1>
                <p className="text-xs leading-relaxed text-faint">{props.request.prompt}</p>
                {props.request.operation && (
                  <p className="font-mono text-[11px] text-faint">来源操作 {props.request.operation}</p>
                )}
              </div>
              <p className="text-[11px] leading-relaxed text-faint">{spec?.hint}</p>
              {props.error && <Notice tone="bad" text={props.error} />}
              <form
                className="space-y-3"
                onSubmit={(event) => {
                  event.preventDefault()
                  if (!props.busy && canSubmit) {
                    props.onSubmit({ secret, publicKey, approved: true })
                  }
                }}
              >
                {!isConfirm && !isKeyMaterial && (
                  <Field label={props.request.kind === 1 ? '登录密码' : '私钥口令'}>
                    <Input
                      type="password"
                      autoComplete="off"
                      value={secret}
                      onChange={(e) => setSecret(e.target.value)}
                    />
                  </Field>
                )}
                {isKeyMaterial && (
                  <>
                    <Field label="私钥正文" hint="OpenSSH 私钥全文（含 BEGIN/END 行）。">
                      <textarea
                        className="h-40 w-full rounded-lg border border-line-hi bg-canvas px-3 py-2 font-mono text-xs text-ink transition-colors placeholder:text-faint focus:border-accent focus-visible:outline-none"
                        value={secret}
                        onChange={(e) => setSecret(e.target.value)}
                        spellCheck={false}
                      />
                    </Field>
                    <Field label="对应公钥（可选）" hint="服务端会自行推导校验，通常留空即可。">
                      <Input value={publicKey} onChange={(e) => setPublicKey(e.target.value)} />
                    </Field>
                  </>
                )}
                <div className="flex gap-2 pt-1">
                  <Button className="flex-1" type="submit" disabled={props.busy || !canSubmit}>
                    {props.busy ? '提交中…' : isConfirm ? '批准' : '提交'}
                  </Button>
                  <Button type="button" variant="ghost" onClick={props.onCancel} disabled={props.busy}>
                    {isConfirm ? '拒绝' : '取消'}
                  </Button>
                </div>
              </form>
            </PanelBody>
          </Panel>
          <p className="mt-3 text-center text-[11px] leading-relaxed text-faint">
            凭据只在 dec-server 进程内存中等待使用，不落盘、不进对话转录。
          </p>
        </div>
      </div>
    </Page>
  )
}
