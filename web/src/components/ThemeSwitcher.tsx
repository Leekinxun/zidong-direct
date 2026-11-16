import { useState } from 'react';
import { Moon, Sun } from 'lucide-react';
import { setTheme, useTheme } from '../theme';

export function ThemeSwitcher() {
  const theme = useTheme();
  const [error, setError] = useState('');
  const label = theme === 'dark' ? '切换到亮色主题' : '切换到暗色主题';
  const Icon = theme === 'dark' ? Sun : Moon;
  return (
    <div className="relative shrink-0">
      <button
        type="button"
        className="apple-btn-secondary p-2"
        title={label}
        aria-label={label}
        onClick={() => {
          setError('');
          void setTheme(theme === 'dark' ? 'light' : 'dark').catch((failure) => setError(
            failure instanceof Error ? failure.message : '外观设置暂时无法保存',
          ));
        }}
      >
        <Icon aria-hidden="true" className="h-4 w-4" />
      </button>
      {error && <span role="status" className="absolute right-0 top-full z-50 mt-1 w-44 rounded border border-slate-600 bg-slate-900 p-2 text-xs text-rose-300">{error}</span>}
    </div>
  );
}
