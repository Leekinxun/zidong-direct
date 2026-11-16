import React, { useState } from 'react';
import { Lock } from 'lucide-react';
import { api, ApiError } from '../api/client';
import { BrandLogo } from './BrandLogo';
import { ThemeSwitcher } from './ThemeSwitcher';

interface Props {
  onSuccess: (username: string) => void;
}

export const LoginModal: React.FC<Props> = ({ onSuccess }) => {
  const [username, setUsername] = useState('admin');
  const [password, setPassword] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError('');
    try {
      const res = await api.login(username, password);
      onSuccess(res.username);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '登录失败');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-surface-base">
      <form
        onSubmit={handleSubmit}
        className="w-full max-w-sm rounded-2xl border border-interaction/[0.12] bg-surface-login p-6 shadow-2xl"
      >
        <div className="mb-5 flex items-center gap-3">
          <BrandLogo className="h-10 w-10 drop-shadow" />
          <div className="flex-1">
            <h1 className="text-base font-semibold tracking-tight text-strong">紫东直连</h1>
            <p className="text-xs text-content-muted">多源数据库直连管理工作台</p>
          </div>
          <ThemeSwitcher />
        </div>

        <div className="space-y-3">
          <div>
            <label className="mb-1 block text-xs text-content-label">用户名</label>
            <input
              type="text"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              className="apple-input w-full py-2 text-sm"
              required
              autoFocus
            />
          </div>
          <div>
            <label className="mb-1 block text-xs text-content-label">密码</label>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="apple-input w-full py-2 text-sm"
              required
            />
          </div>
        </div>

        {error && (
          <div className="mt-3 rounded-[7px] border border-danger/30 bg-danger/15 px-3 py-2 text-xs text-icon-red">
            {error}
          </div>
        )}

        <button
          type="submit"
          disabled={loading}
          className="apple-btn-primary mt-5 w-full py-2 text-sm"
        >
          <Lock className="h-4 w-4" />
          {loading ? '正在验证...' : '登录紫东直连'}
        </button>
      </form>
    </div>
  );
};
