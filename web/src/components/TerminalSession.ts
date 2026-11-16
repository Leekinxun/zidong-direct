import type { Terminal } from '@xterm/xterm';
import type { SSHInput } from '../types/db';

export type { SSHHostSaveInput } from '../types/db';

export interface TerminalSize {
  rows: number;
  cols: number;
}

export type TerminalSSHSettings = Omit<SSHInput, 'enabled'>;
export type TerminalTarget = { connectionId: string } | { sshHostId: string } | { ssh: TerminalSSHSettings };
export type TerminalEncoding = 'utf-8' | 'gbk' | 'gb18030';

const endpoint = '/api/terminal/sessions';
const maxInputBytes = 128 * 1024;

class TerminalRequestError extends Error {
  constructor(message: string, readonly code?: string) {
    super(message);
  }
}

async function requireOK(response: Response) {
  if (response.ok) return response;
  const payload = await response.json().catch(() => null) as { message?: string; error?: string } | null;
  throw new TerminalRequestError(payload?.message || `终端请求失败（HTTP ${response.status}）`, payload?.error);
}

async function openTerminalSession(target: TerminalTarget, size: TerminalSize, encoding: TerminalEncoding, signal?: AbortSignal) {
  const response = await requireOK(await fetch(endpoint, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ...target, rows: size.rows, cols: size.cols, encoding }),
    signal,
  }));
  const { sessionId } = await response.json() as { sessionId: string };
  return sessionId;
}

function closeTerminalSession(sessionId: string) {
  return fetch(`${endpoint}/${encodeURIComponent(sessionId)}`, { method: 'DELETE', credentials: 'same-origin' });
}

export async function testTerminalConnection(target: TerminalTarget, encoding: TerminalEncoding = 'utf-8') {
  const startedAt = performance.now();
  const sessionId = await openTerminalSession(target, { rows: 24, cols: 80 }, encoding);
  const latencyMs = Math.round(performance.now() - startedAt);
  await requireOK(await closeTerminalSession(sessionId));
  return latencyMs;
}

export class TerminalSession {
  private readonly abort = new AbortController();
  private sessionId = '';
  private stream: WebSocket | null = null;
  private ready = false;
  private readonly input: { data: Uint8Array; binary: boolean }[] = [];
  private inputBytes = 0;
  private inputTimer: number | undefined;
  private sendingInput = false;
  private desiredSize: TerminalSize | null = null;
  private sendingSize = false;
  private opening: Promise<string> | null = null;
  private closeCompletion: Promise<void> | null = null;

  constructor(
    private readonly onEnd: (error?: string) => void,
    private readonly onInputError?: (error: string) => void,
  ) {}

  async connect(target: TerminalTarget, terminal: Terminal, onReady: () => void, encoding: TerminalEncoding = 'utf-8') {
    const { signal } = this.abort;
    try {
      if (signal.aborted) return;
      this.opening = openTerminalSession(target, terminal, encoding, signal);
      this.sessionId = await this.opening;
      if (signal.aborted) return;

      const url = new URL(this.sessionURL('stream'), window.location.href);
      url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
      const stream = new WebSocket(url);
      this.stream = stream;
      stream.binaryType = 'arraybuffer';
      stream.onopen = () => {
        if (signal.aborted) return;
        this.ready = true;
        onReady();
        void this.flushSize();
      };
      stream.onmessage = ({ data }) => {
        if (signal.aborted) return;
        if (!(data instanceof ArrayBuffer)) {
          this.fail(new Error('终端输出流格式无效'));
          return;
        }
        terminal.write(new Uint8Array(data), () => {
          if (stream.readyState === WebSocket.OPEN) stream.send(new Uint8Array([1]));
        });
      };
      stream.onerror = () => this.fail(new Error('无法连接终端输出流'));
      stream.onclose = ({ wasClean }) => {
        if (signal.aborted) return;
        if (!this.ready || !wasClean) {
          this.fail(new Error('终端输出连接已中断'));
          return;
        }
        void this.close();
        this.onEnd();
      };
    } catch (error) {
      this.fail(error);
    }
  }

  send(data: Uint8Array) {
    this.queueInput(data, false);
  }

  sendBinary(data: Uint8Array) {
    this.queueInput(data, true);
  }

  private queueInput(data: Uint8Array, binary: boolean) {
    if (!this.ready || data.length === 0) return;
    if (this.inputBytes + data.length > maxInputBytes) {
      this.fail(new Error('终端输入缓冲已满，连接已断开'));
      return;
    }
    this.input.push({ data, binary });
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

  close(): Promise<void> {
    if (this.closeCompletion) return this.closeCompletion;
    this.ready = false;
    this.abort.abort();
    this.stream?.close();
    this.stream = null;
    window.clearTimeout(this.inputTimer);
    this.input.length = 0;
    this.inputBytes = 0;
    this.closeCompletion = this.deleteSession();
    return this.closeCompletion;
  }

  private sessionURL(action: string) {
    return `${endpoint}/${encodeURIComponent(this.sessionId)}/${action}`;
  }

  private async deleteSession() {
    const sessionId = this.sessionId || await this.opening?.catch(() => '');
    this.sessionId = '';
    if (sessionId) await closeTerminalSession(sessionId).catch(() => {});
  }

  private fail(error: unknown) {
    if (this.abort.signal.aborted) return;
    void this.close();
    this.onEnd(error instanceof Error ? error.message : 'SSH 终端连接异常');
  }

  private async flushInput() {
    this.sendingInput = true;
    try {
      while (this.ready && this.inputBytes > 0) {
        const binary = this.input[0].binary;
        let length = 0;
        let count = 0;
        for (const part of this.input) {
          if (part.binary !== binary) break;
          length += part.data.length;
          count++;
        }
        const payload = new Uint8Array(length);
        let offset = 0;
        for (const { data } of this.input.splice(0, count)) {
          payload.set(data, offset);
          offset += data.length;
        }
        this.inputBytes -= length;
        try {
          await requireOK(await fetch(this.sessionURL('input'), {
            method: 'POST',
            credentials: 'same-origin',
            headers: { 'Content-Type': 'application/octet-stream', 'X-Zidong-Terminal-Input': binary ? 'binary' : 'text' },
            body: payload.buffer,
            signal: this.abort.signal,
          }));
        } catch (error) {
          if (!(error instanceof TerminalRequestError) || error.code !== 'terminal_input_encoding' || !this.onInputError) {
            throw error;
          }
          if (!this.abort.signal.aborted) this.onInputError(error.message);
        }
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
