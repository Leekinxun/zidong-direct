import type { Terminal } from '@xterm/xterm';
import type { SSHInput } from '../types/db';

export type { SSHHostSaveInput } from '../types/db';

export interface TerminalSize {
  rows: number;
  cols: number;
}

export type TerminalSSHSettings = Omit<SSHInput, 'enabled'>;
export type TerminalTarget = { connectionId: string } | { sshHostId: string } | { ssh: TerminalSSHSettings };

const endpoint = '/api/terminal/sessions';
const maxInputBytes = 128 * 1024;

async function requireOK(response: Response) {
  if (response.ok) return response;
  const payload = await response.json().catch(() => null) as { message?: string } | null;
  throw new Error(payload?.message || `终端请求失败（HTTP ${response.status}）`);
}

async function openTerminalSession(target: TerminalTarget, size: TerminalSize, signal?: AbortSignal) {
  const response = await requireOK(await fetch(endpoint, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ...target, rows: size.rows, cols: size.cols }),
    signal,
  }));
  const { sessionId } = await response.json() as { sessionId: string };
  return sessionId;
}

function closeTerminalSession(sessionId: string) {
  return fetch(`${endpoint}/${encodeURIComponent(sessionId)}`, {
    method: 'DELETE',
    credentials: 'same-origin',
  });
}

export async function testTerminalConnection(target: TerminalTarget) {
  const startedAt = performance.now();
  const sessionId = await openTerminalSession(target, { rows: 24, cols: 80 });
  const latencyMs = Math.round(performance.now() - startedAt);
  await requireOK(await closeTerminalSession(sessionId));
  return latencyMs;
}

function writeChunk(terminal: Terminal, value: Uint8Array, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    if (signal.aborted) {
      resolve();
      return;
    }
    const done = () => {
      signal.removeEventListener('abort', done);
      resolve();
    };
    signal.addEventListener('abort', done, { once: true });
    terminal.write(value, done);
  });
}

export class TerminalSession {
  private readonly abort = new AbortController();
  private sessionId = '';
  private ready = false;
  private readonly input: Uint8Array[] = [];
  private inputBytes = 0;
  private inputTimer: number | undefined;
  private sendingInput = false;
  private desiredSize: TerminalSize | null = null;
  private sendingSize = false;

  constructor(private readonly onEnd: (error?: string) => void) {}

  async connect(target: TerminalTarget, terminal: Terminal, onReady: () => void) {
    const { signal } = this.abort;
    try {
      this.sessionId = await openTerminalSession(target, terminal, signal);
      if (signal.aborted) {
        this.deleteSession();
        return;
      }

      const stream = await requireOK(await fetch(this.sessionURL('output'), {
        credentials: 'same-origin',
        signal,
      }));
      if (!stream.body) throw new Error('终端输出流不可用');
      const reader = stream.body.getReader();
      try {
        if (signal.aborted) return;
        this.ready = true;
        onReady();
        void this.flushSize();
        while (!signal.aborted) {
          const { value, done } = await reader.read();
          if (done) break;
          await writeChunk(terminal, value, signal);
        }
      } finally {
        reader.releaseLock();
      }
      if (!signal.aborted) {
        this.close();
        this.onEnd();
      }
    } catch (error) {
      this.fail(error);
    }
  }

  send(data: Uint8Array) {
    if (!this.ready || data.length === 0) return;
    if (this.inputBytes + data.length > maxInputBytes) {
      this.fail(new Error('终端输入缓冲已满，连接已断开'));
      return;
    }
    this.input.push(data);
    this.inputBytes += data.length;
    if (this.inputTimer === undefined && !this.sendingInput) {
      this.inputTimer = window.setTimeout(() => {
        this.inputTimer = undefined;
        void this.flushInput();
      }, 12);
    }
  }

  resize(size: TerminalSize) {
    this.desiredSize = size;
    void this.flushSize();
  }

  close() {
    this.ready = false;
    this.abort.abort();
    window.clearTimeout(this.inputTimer);
    this.input.length = 0;
    this.inputBytes = 0;
    this.deleteSession();
  }

  private sessionURL(action: string) {
    return `${endpoint}/${encodeURIComponent(this.sessionId)}/${action}`;
  }

  private deleteSession() {
    if (!this.sessionId) return;
    const sessionId = this.sessionId;
    this.sessionId = '';
    void closeTerminalSession(sessionId).catch(() => {});
  }

  private fail(error: unknown) {
    if (this.abort.signal.aborted) return;
    this.close();
    this.onEnd(error instanceof Error ? error.message : 'SSH 终端连接异常');
  }

  private async flushInput() {
    this.sendingInput = true;
    try {
      while (this.ready && this.inputBytes > 0) {
        const payload = new Uint8Array(this.inputBytes);
        let offset = 0;
        for (const part of this.input) {
          payload.set(part, offset);
          offset += part.length;
        }
        this.input.length = 0;
        this.inputBytes = 0;
        await requireOK(await fetch(this.sessionURL('input'), {
          method: 'POST',
          credentials: 'same-origin',
          headers: { 'Content-Type': 'application/octet-stream' },
          body: payload.buffer,
          signal: this.abort.signal,
        }));
      }
    } catch (error) {
      this.fail(error);
    } finally {
      this.sendingInput = false;
    }
  }

  private async flushSize() {
    if (!this.ready || this.sendingSize) return;
    this.sendingSize = true;
    try {
      while (this.ready && this.desiredSize) {
        const size = this.desiredSize;
        this.desiredSize = null;
        await requireOK(await fetch(this.sessionURL('resize'), {
          method: 'POST',
          credentials: 'same-origin',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(size),
          signal: this.abort.signal,
        }));
      }
    } catch (error) {
      this.fail(error);
    } finally {
      this.sendingSize = false;
    }
  }
}
