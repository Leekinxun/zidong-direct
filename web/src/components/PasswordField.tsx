import { useId, useState } from 'react';
import { Eye, EyeOff } from 'lucide-react';

interface Props {
  label: string;
  hint?: string;
  saved?: boolean;
  required?: boolean;
  value: string;
  onChange: (value: string) => void;
}

export function PasswordField({ label, hint, saved, required, value, onChange }: Props) {
  const id = useId();
  const [visible, setVisible] = useState(false);
  const action = `${visible ? '隐藏' : '显示'}${label}`;
  const Icon = visible ? EyeOff : Eye;

  return (
    <div>
      <label htmlFor={id} className="mb-1 block text-xs text-slate-300">
        {label}{hint}
      </label>
      <div className="relative">
        <input
          id={id}
          type={visible ? 'text' : 'password'}
          placeholder={saved ? '••••••••' : ''}
          required={required}
          value={value}
          spellCheck={false}
          onChange={(event) => {
            const value = event.target.value;
            if (!value) setVisible(false);
            onChange(value);
          }}
          className="w-full rounded border border-slate-700 bg-slate-800 py-1.5 pl-3 pr-10 text-sm text-slate-100 outline-none focus:border-indigo-500"
        />
        <button
          type="button"
          aria-label={action}
          aria-controls={id}
          title={action}
          disabled={!value}
          onClick={() => setVisible((previous) => !previous)}
          className="absolute inset-y-0 right-0 flex w-9 items-center justify-center rounded-r text-slate-400 hover:text-slate-100 focus-visible:outline focus-visible:outline-2 focus-visible:outline-indigo-500 disabled:opacity-40"
        >
          <Icon aria-hidden="true" className="h-4 w-4" />
        </button>
      </div>
    </div>
  );
}
