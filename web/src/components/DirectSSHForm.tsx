import { useState, type FormEvent } from 'react';
import { PasswordField } from './PasswordField';
import type { TerminalSSHSettings } from './TerminalSession';

interface Props {
  visible: boolean;
  onConnect: (settings: TerminalSSHSettings) => void;
}

const field = 'mt-1 w-full rounded border border-slate-700 bg-slate-800 px-3 py-1.5 text-sm text-slate-100 outline-none focus:border-indigo-500';

export function DirectSSHForm({ visible, onConnect }: Props) {
  const [authType, setAuthType] = useState<'password' | 'privateKey'>('password');
  const [password, setPassword] = useState('');
  const [passphrase, setPassphrase] = useState('');
  const [skipVerification, setSkipVerification] = useState(false);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const fields = new FormData(event.currentTarget);
    const text = (name: string) => String(fields.get(name) ?? '');
    onConnect({
      host: text('host').trim(),
      port: Number(text('port')),
      username: text('username').trim(),
      authType,
      password: authType === 'password' ? password : undefined,
      privateKey: authType === 'privateKey' ? text('privateKey') : undefined,
      passphrase: authType === 'privateKey' ? passphrase : undefined,
      hostKeyFingerprint: text('hostKeyFingerprint').trim(),
      insecureIgnoreHostKey: skipVerification,
    });
  }

  return (
    <form
      onSubmit={submit}
      className={visible ? 'min-h-0 flex-1 overflow-y-auto px-4 py-4' : 'hidden'}
      autoComplete="off"
    >
      <div className="mx-auto max-w-2xl space-y-3 text-xs text-slate-300">
        <div className="grid grid-cols-[minmax(0,1fr)_6rem] gap-3">
          <label>
            SSH 主机
            <input name="host" required className={field} placeholder="主机名或 IP" />
          </label>
          <label>
            SSH 端口
            <input name="port" type="number" required min={1} max={65535} defaultValue={22} className={field} />
          </label>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <label>
            SSH 用户名
            <input name="username" required className={field} />
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
              <textarea name="privateKey" rows={3} required spellCheck={false} className={`${field} font-mono`} placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" />
            </label>
            <PasswordField label="私钥口令" hint="（可选）" value={passphrase} onChange={setPassphrase} />
          </>
        )}
        <label className="block">
          SSH 主机公钥指纹
          <input name="hostKeyFingerprint" required={!skipVerification} spellCheck={false} className={field} placeholder="SHA256:…" />
        </label>
        <label className="flex items-center gap-2">
          <input type="checkbox" checked={skipVerification} onChange={(event) => setSkipVerification(event.target.checked)} />
          未填写指纹时临时跳过主机验证
        </label>
        <button type="submit" className="rounded bg-indigo-600 px-3 py-1.5 font-medium text-white hover:bg-indigo-500">
          连接
        </button>
      </div>
    </form>
  );
}
