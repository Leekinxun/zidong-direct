import type { Terminal } from '@xterm/xterm';

export interface TerminalSize {
  rows: number;
  cols: number;
}

const endpoint = '/api/terminal/sessions';
const maxInputBytes = 128 * 1024;

async function requireOK(response: Response) {
  if (response.ok) return response;
  const payload = await response.json().catch(() => null) as { message?: string } | null;
  throw new Error(payload?.message || `终端请求失败（HTTP ${response.status}）`);
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

  async connect(connectionId: string, terminal: Terminal, onReady: () => void) {
    const { signal } = this.abort;
    try {
      const response = await requireOK(await fetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ connectionId, rows: terminal.rows, cols: terminal.cols }),
        signal,
      }));
      const { sessionId } = await response.json() as { sessionId: string };
      this.sessionId = sessionId;
      if (signal.aborted) {
        this.deleteSession();
        return;
      }

      const stream = await requireOK(await fetch(this.sessionURL('output'), { signal }));
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
    const url = `${endpoint}/${encodeURIComponent(this.sessionId)}`;
    this.sessionId = '';
    void fetch(url, { method: 'DELETE' }).catch(() => {});
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
