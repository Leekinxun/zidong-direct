import { useId, useState, type FormEvent, type MouseEvent } from 'react';
import type { SSHHostSaveInput, SSHHostView } from '../types/db';
import { PasswordField } from './PasswordField';
import type { TerminalSSHSettings } from './TerminalSession';

interface Props {
  visible: boolean;
  initialSettings?: TerminalSSHSettings;
  onConnect: (settings: TerminalSSHSettings) => void;
  onSaveHost?: (input: SSHHostSaveInput) => Promise<SSHHostView>;
}

const field = 'mt-1 w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500';

export function DirectSSHForm({ visible, initialSettings, onConnect, onSaveHost }: Props) {
  const [authType, setAuthType] = useState(initialSettings?.authType ?? 'password');
  const [password, setPassword] = useState(initialSettings?.password ?? '');
  const [passphrase, setPassphrase] = useState(initialSettings?.passphrase ?? '');
  const [skipVerification, setSkipVerification] = useState(initialSettings?.insecureIgnoreHostKey ?? false);
  const [missingFingerprint, setMissingFingerprint] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveResult, setSaveResult] = useState<{ ok: boolean; message: string } | null>(null);
  const fingerprintErrorId = useId();

  function readSettings(fields: FormData): TerminalSSHSettings {
    const text = (name: string) => String(fields.get(name) ?? '');
    return {
      host: text('host').trim(),
      port: Number(text('port')),
      username: text('username').trim(),
      authType,
      password: authType === 'password' ? password : undefined,
      privateKey: authType === 'privateKey' ? text('privateKey') : undefined,
      passphrase: authType === 'privateKey' ? passphrase : undefined,
      hostKeyFingerprint: text('hostKeyFingerprint').trim(),
      insecureIgnoreHostKey: skipVerification,
    };
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!saving) onConnect(readSettings(new FormData(event.currentTarget)));
  }

  async function saveHost(event: MouseEvent<HTMLButtonElement>) {
    const form = event.currentTarget.form;
    if (!onSaveHost || saving || !form?.reportValidity()) return;
    const fields = new FormData(form);
    setSaving(true);
    setSaveResult(null);
    try {
      const name = String(fields.get('hostName') ?? '').trim();
      const host = await onSaveHost({ name: name || undefined, ssh: readSettings(fields) });
      setSaveResult({ ok: true, message: `已保存 SSH 主机「${host.name}」` });
    } catch (error) {
      setSaveResult({ ok: false, message: error instanceof Error ? error.message : '保存 SSH 主机失败' });
    } finally {
      setSaving(false);
    }
  }

  return (
    <form
      onSubmit={submit}
      onChange={() => setSaveResult(null)}
      onInvalid={(event) => {
        if (event.target instanceof HTMLInputElement && event.target.name === 'hostKeyFingerprint') {
          setMissingFingerprint(true);
        }
      }}
      className={visible ? 'min-h-0 flex-1 overflow-y-auto px-4 py-4' : 'hidden'}
      autoComplete="off"
    >
      <div className="mx-auto max-w-2xl space-y-3 text-xs text-slate-300">
        {onSaveHost && (
          <label className="block">
            主机名称（可选）
            <input name="hostName" className={field} placeholder="默认使用用户名和地址" />
          </label>
        )}
        <div className="grid grid-cols-[minmax(0,1fr)_6rem] gap-3">
          <label>
            SSH 主机
            <input name="host" defaultValue={initialSettings?.host} required className={field} placeholder="主机名或 IP" />
          </label>
          <label>
            SSH 端口
            <input name="port" type="number" required min={1} max={65535} defaultValue={initialSettings?.port || 22} className={field} />
          </label>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <label>
            SSH 用户名
            <input name="username" defaultValue={initialSettings?.username} required className={field} />
          </label>
          <label>
            认证方式
            <select value={authType} onChange={(event) => setAuthType(event.target.value === 'privateKey' ? 'privateKey' : 'password')} className={field}>
              <option value="password">密码认证</option>
              <option value="privateKey">私钥认证</option>
            </select>
          </label>
        </div>
        {authType === 'password' ? (
          <PasswordField label="SSH 密码" required value={password} onChange={setPassword} />
        ) : (
          <>
            <label className="block">
              SSH 私钥
              <textarea name="privateKey" defaultValue={initialSettings?.privateKey} rows={3} required spellCheck={false} className={`${field} font-mono`} placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" />
            </label>
            <PasswordField label="私钥口令" hint="（可选）" value={passphrase} onChange={setPassphrase} />
          </>
        )}
        <label className="block">
          SSH 主机公钥指纹
          <input
            name="hostKeyFingerprint"
            defaultValue={initialSettings?.hostKeyFingerprint}
            required={!skipVerification}
            onChange={() => setMissingFingerprint(false)}
            aria-invalid={missingFingerprint || undefined}
            aria-describedby={missingFingerprint ? fingerprintErrorId : undefined}
            spellCheck={false}
            className={field}
            placeholder="SHA256:…"
          />
        </label>
        {missingFingerprint && (
          <p id={fingerprintErrorId} role="alert" className="text-amber-300">
            请填写服务器的 SHA256 指纹。确认主机身份后，也可选择临时跳过验证。
          </p>
        )}
        <label className="flex items-center gap-2">
          <input type="checkbox" checked={skipVerification} onChange={(event) => {
            setSkipVerification(event.target.checked);
            setMissingFingerprint(false);
          }} />
          未填写指纹时临时跳过主机验证
        </label>
        <div className="flex flex-wrap items-center gap-2">
          <button type="submit" disabled={saving} className="rounded bg-indigo-600 px-3 py-1.5 font-medium text-white hover:bg-indigo-500 disabled:opacity-50">
            连接
          </button>
          {onSaveHost && (
            <button type="button" disabled={saving} onClick={(event) => void saveHost(event)} className="rounded bg-slate-700 px-3 py-1.5 font-medium text-white hover:bg-slate-600 disabled:opacity-50">
              {saving ? '正在保存…' : '保存 SSH 主机'}
            </button>
          )}
        </div>
        {saveResult && (
          <p role={saveResult.ok ? 'status' : 'alert'} className={saveResult.ok ? 'text-emerald-300' : 'text-red-300'}>
            {saveResult.message}
          </p>
        )}
      </div>
    </form>
  );
}
