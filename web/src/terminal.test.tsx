import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  consumeWheelHistorySequence,
  getWheelHistorySequence,
  shouldSuppressWheelHistory,
  TerminalView,
} from "./terminal";

const {
  attachTerminalSession,
  createTerminalSession,
  terminalInstances,
  MockTerminal,
  MockFitAddon,
  MockWebglAddon,
  MockResizeObserver,
  MockWebSocket,
  HttpError,
} = vi.hoisted(() => {
  class HttpError extends Error {
    constructor(public status: number, message: string) {
      super(message);
    }
  }

  const attachTerminalSession = vi.fn(async (sessionId: string) => ({
    id: sessionId,
    machine_name: "m-1",
    host_id: "fascinate-01",
    attach_url: `/v1/terminal/sessions/${sessionId}/stream?token=token-${sessionId}`,
    expires_at: "2026-03-22T00:00:00Z",
  }));

  const createTerminalSession = vi.fn(async () => ({
    id: "term-1",
    machine_name: "m-1",
    host_id: "fascinate-01",
    attach_url: "/v1/terminal/sessions/term-1/stream?token=token-1",
    expires_at: "2026-03-22T00:00:00Z",
  }));

  class MockTerminal {
    cols = 120;
    rows = 40;
    textarea = document.createElement("textarea");
    modes = { mouseTrackingMode: "none" as const };
    buffer = {
      active: {
        type: "normal" as const,
        viewportY: 0,
        baseY: 0,
        length: 40,
        getLine: vi.fn(),
        getNullCell: vi.fn(),
      },
    };
    focus = vi.fn();
    writeln = vi.fn();
    write = vi.fn();
    refresh = vi.fn();
    open = vi.fn((host?: Element) => {
      this.textarea.setAttribute("aria-label", "Terminal input");
      this.textarea.className = "xterm-helper-textarea";
      host?.appendChild(this.textarea);
    });
    loadAddon = vi.fn();
    dispose = vi.fn(() => {
      this.textarea.remove();
    });
    onData = vi.fn(() => ({ dispose: vi.fn() }));
    onResize = vi.fn(() => ({ dispose: vi.fn() }));
    hasSelection = vi.fn(() => false);
    getSelection = vi.fn(() => "");
    private selectionChangeListener: (() => void) | null = null;
    onSelectionChange = vi.fn((listener: () => void) => {
      this.selectionChangeListener = listener;
      return {
        dispose: vi.fn(() => {
          if (this.selectionChangeListener === listener) {
            this.selectionChangeListener = null;
          }
        }),
      };
    });

    emitSelectionChange() {
      this.selectionChangeListener?.();
    }
  }

  const terminalInstances: MockTerminal[] = [];

  class MockFitAddon {
    fit = vi.fn();
  }

  type ContextLossHandler = () => void;

  class MockWebglAddon {
    static instances: MockWebglAddon[] = [];

    dispose = vi.fn();
    private handlers = new Set<ContextLossHandler>();

    constructor() {
      MockWebglAddon.instances.push(this);
    }

    onContextLoss(handler: ContextLossHandler) {
      this.handlers.add(handler);
      return {
        dispose: () => {
          this.handlers.delete(handler);
        },
      };
    }

    emitContextLoss() {
      for (const handler of this.handlers) {
        handler();
      }
    }
  }

  class MockResizeObserver {
    observe() {}
    disconnect() {}
  }

  class MockWebSocket {
    static instances: MockWebSocket[] = [];
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSING = 2;
    static CLOSED = 3;

    binaryType = "blob";
    readyState = MockWebSocket.CONNECTING;
    private listeners = new Map<string, Set<(event?: any) => void>>();

    constructor(public url: string) {
      MockWebSocket.instances.push(this);
    }

    addEventListener(type: string, listener: (event?: any) => void) {
      const current = this.listeners.get(type) ?? new Set();
      current.add(listener);
      this.listeners.set(type, current);
    }

    send = vi.fn();
    close = vi.fn();

    emit(type: string, event?: any) {
      if (type === "open") {
        this.readyState = MockWebSocket.OPEN;
      }
      if (type === "close") {
        this.readyState = MockWebSocket.CLOSED;
      }
      for (const listener of this.listeners.get(type) ?? []) {
        listener(event);
      }
    }
  }

  return {
    attachTerminalSession,
    createTerminalSession,
    terminalInstances,
    MockTerminal,
    MockFitAddon,
    MockWebglAddon,
    MockResizeObserver,
    MockWebSocket,
    HttpError,
  };
});

vi.mock("./api", () => ({
  attachTerminalSession,
  createTerminalSession,
  HttpError,
  toWebSocketURL: (path: string) => `ws://example.test${path}`,
}));

vi.mock("xterm", () => ({
  Terminal: vi.fn(() => {
    const terminal = new MockTerminal();
    terminalInstances.push(terminal);
    return terminal;
  }),
}));

vi.mock("@xterm/addon-fit", () => ({
  FitAddon: MockFitAddon,
}));

vi.mock("@xterm/addon-webgl", () => ({
  WebglAddon: MockWebglAddon,
}));

describe("TerminalView", () => {
  beforeEach(() => {
    vi.stubGlobal("ResizeObserver", MockResizeObserver);
    vi.stubGlobal("WebSocket", MockWebSocket);
    Object.defineProperty(globalThis, "navigator", {
      configurable: true,
      value: {
        clipboard: {
          writeText: vi.fn().mockResolvedValue(undefined),
        },
      },
    });
    Object.defineProperty(globalThis, "isSecureContext", {
      configurable: true,
      value: true,
    });
    terminalInstances.length = 0;
    MockWebSocket.instances.length = 0;
    MockWebglAddon.instances.length = 0;
    attachTerminalSession.mockReset();
    attachTerminalSession.mockImplementation(async (sessionId: string) => ({
      id: sessionId,
      machine_name: "m-1",
      host_id: "fascinate-01",
      attach_url: `/v1/terminal/sessions/${sessionId}/stream?token=token-${sessionId}`,
      expires_at: "2026-03-22T00:00:00Z",
    }));
    createTerminalSession.mockReset();
    createTerminalSession.mockImplementation(async () => ({
      id: "term-1",
      machine_name: "m-1",
      host_id: "fascinate-01",
      attach_url: "/v1/terminal/sessions/term-1/stream?token=token-1",
      expires_at: "2026-03-22T00:00:00Z",
    }));
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("falls back when the WebGL renderer loses context", async () => {
    const onSessionId = vi.fn();
    render(<TerminalView machineName="m-1" title="m-1 shell" onSessionId={onSessionId} />);

    await waitFor(() => {
      expect(createTerminalSession).toHaveBeenCalledWith("m-1", 120, 40);
    });
    expect(onSessionId).toHaveBeenCalledWith("term-1");

    await act(async () => {
      MockWebSocket.instances[0].emit("open");
    });

    await act(async () => {
      MockWebglAddon.instances[0].emitContextLoss();
    });

    expect(MockWebglAddon.instances[0].dispose).toHaveBeenCalled();
    expect(terminalInstances[0].refresh).toHaveBeenCalled();
    expect(terminalInstances[0].write).toHaveBeenCalledWith(
      "\r\n\x1b[90mrenderer fallback enabled after graphics context reset\x1b[0m\r\n",
    );
  });

  it("reattaches to an existing terminal session", async () => {
    const onSessionId = vi.fn();

    render(
      <TerminalView machineName="m-1" title="m-1 shell" sessionId="term-existing" onSessionId={onSessionId} />,
    );

    await waitFor(() => {
      expect(attachTerminalSession).toHaveBeenCalledWith("term-existing", 120, 40);
    });
    expect(createTerminalSession).not.toHaveBeenCalled();
    expect(onSessionId).toHaveBeenCalledWith("term-existing");
    expect(screen.getByText("Reconnecting…")).toBeTruthy();
  });

  it("asks the user to start a new shell when the saved session can no longer be reattached", async () => {
    attachTerminalSession.mockRejectedValueOnce(new HttpError(404, "not found"));
    const onSessionId = vi.fn();

    render(
      <TerminalView machineName="m-1" title="m-1 shell" sessionId="term-stale" onSessionId={onSessionId} />,
    );

    expect(await screen.findByText("Shell ended")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Start new shell" })).toBeTruthy();
    expect(createTerminalSession).not.toHaveBeenCalled();

    await act(async () => {
      screen.getByRole("button", { name: "Start new shell" }).click();
    });

    await waitFor(() => {
      expect(createTerminalSession).toHaveBeenCalledWith("m-1", 120, 40);
    });
    expect(onSessionId).toHaveBeenCalledWith("term-1");
  });

  it("updates cwd metadata from the terminal stream", async () => {
    const onSessionId = vi.fn();
    const onCwdChange = vi.fn();

    render(
      <TerminalView
        machineName="m-1"
        title="m-1 shell"
        onSessionId={onSessionId}
        onCwdChange={onCwdChange}
      />,
    );

    await waitFor(() => {
      expect(createTerminalSession).toHaveBeenCalledWith("m-1", 120, 40);
    });

    await act(async () => {
      MockWebSocket.instances[0].emit("open");
    });

    await act(async () => {
      MockWebSocket.instances[0].emit("message", {
        data: new TextEncoder().encode("\u001b]1337;FascinateCwd=/home/ubuntu/space-shooter\u0007").buffer,
      });
    });

    await act(async () => {
      MockWebSocket.instances[0].emit("message", {
        data: new TextEncoder().encode("\u001b(Bubuntu@m-1:~/space-shooter$ ").buffer,
      });
    });

    expect(onCwdChange).toHaveBeenNthCalledWith(1, "/home/ubuntu/space-shooter");
    expect(onCwdChange).toHaveBeenNthCalledWith(2, "~/space-shooter");
  });

  it("suppresses wheel input when the terminal has no scrollback", async () => {
    render(<TerminalView machineName="m-1" title="m-1 shell" onSessionId={vi.fn()} />);

    await waitFor(() => {
      expect(createTerminalSession).toHaveBeenCalledWith("m-1", 120, 40);
    });

    const terminal = terminalInstances[0];
    const wheelEvent = new WheelEvent("wheel", {
      deltaY: 120,
      bubbles: true,
      cancelable: true,
    });

    expect(shouldSuppressWheelHistory(terminal as unknown as any, wheelEvent)).toBe(true);
    expect(getWheelHistorySequence(terminal as unknown as any, wheelEvent)).toBe("\u001b[6~");
  });

  it("accumulates small wheel deltas before scrolling tmux history", async () => {
    render(<TerminalView machineName="m-1" title="m-1 shell" onSessionId={vi.fn()} />);

    await waitFor(() => {
      expect(createTerminalSession).toHaveBeenCalledWith("m-1", 120, 40);
    });

    const terminal = terminalInstances[0];
    const subtleWheelEvent = new WheelEvent("wheel", {
      deltaY: 24,
      bubbles: true,
      cancelable: true,
    });
    const assertiveWheelEvent = new WheelEvent("wheel", {
      deltaY: 120,
      bubbles: true,
      cancelable: true,
    });

    expect(consumeWheelHistorySequence(terminal as unknown as any, subtleWheelEvent, 0)).toEqual({
      consume: true,
      sequence: "",
      remainder: 24,
    });
    expect(consumeWheelHistorySequence(terminal as unknown as any, assertiveWheelEvent, 24)).toEqual({
      consume: true,
      sequence: "\u001b[6~",
      remainder: 48,
    });
  });

  it("normalizes line-mode wheel deltas before scrolling tmux history", async () => {
    render(<TerminalView machineName="m-1" title="m-1 shell" onSessionId={vi.fn()} />);

    await waitFor(() => {
      expect(createTerminalSession).toHaveBeenCalledWith("m-1", 120, 40);
    });

    const terminal = terminalInstances[0];
    const lineWheelEvent = new WheelEvent("wheel", {
      deltaY: 3,
      deltaMode: WheelEvent.DOM_DELTA_LINE,
      bubbles: true,
      cancelable: true,
    });

    expect(consumeWheelHistorySequence(terminal as unknown as any, lineWheelEvent, 0)).toEqual({
      consume: true,
      sequence: "\u001b[6~",
      remainder: 0,
    });
  });

  it("consumes subtle wheel events before xterm can turn them into prompt history", async () => {
    render(<TerminalView machineName="m-1" title="m-1 shell" onSessionId={vi.fn()} />);

    await waitFor(() => {
      expect(createTerminalSession).toHaveBeenCalledWith("m-1", 120, 40);
    });

    await act(async () => {
      MockWebSocket.instances[0].emit("open");
    });

    const host = document.querySelector(".terminal-host");
    expect(host).not.toBeNull();

    const subtleWheelEvent = new WheelEvent("wheel", {
      deltaY: 24,
      bubbles: true,
      cancelable: true,
    });

    const dispatchResult = host!.dispatchEvent(subtleWheelEvent);

    expect(dispatchResult).toBe(false);
    expect(subtleWheelEvent.defaultPrevented).toBe(true);
    expect(MockWebSocket.instances[0].send).not.toHaveBeenCalled();
  });

  it("allows wheel scroll when the terminal has scrollback", async () => {
    render(<TerminalView machineName="m-1" title="m-1 shell" onSessionId={vi.fn()} />);

    await waitFor(() => {
      expect(createTerminalSession).toHaveBeenCalledWith("m-1", 120, 40);
    });

    const terminal = terminalInstances[0];
    terminal.buffer.active.baseY = 24;

    const wheelEvent = new WheelEvent("wheel", {
      deltaY: 120,
      bubbles: true,
      cancelable: true,
    });

    expect(shouldSuppressWheelHistory(terminal as unknown as any, wheelEvent)).toBe(false);
    expect(getWheelHistorySequence(terminal as unknown as any, wheelEvent)).toBe("");
  });

  it("copies OSC 52 clipboard writes to the local browser clipboard", async () => {
    const onSessionId = vi.fn();
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(globalThis, "navigator", {
      configurable: true,
      value: {
        clipboard: {
          writeText,
        },
      },
    });

    render(<TerminalView machineName="m-1" title="m-1 shell" onSessionId={onSessionId} />);

    await waitFor(() => {
      expect(createTerminalSession).toHaveBeenCalledWith("m-1", 120, 40);
    });

    await act(async () => {
      MockWebSocket.instances[0].emit("open");
    });

    const url = "https://claude.ai/oauth/authorize?code=true";
    const encoded = btoa(url);

    await act(async () => {
      MockWebSocket.instances[0].emit("message", {
        data: new TextEncoder().encode(`\u001b]52;c;${encoded}\u0007`).buffer,
      });
    });

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith(url);
    });
    expect(await screen.findByText("Copied to your local clipboard.")).toBeTruthy();
  });

  it("does not override xterm's native selection copy handling", async () => {
    const onSessionId = vi.fn();

    render(<TerminalView machineName="m-1" title="m-1 shell" onSessionId={onSessionId} />);

    await waitFor(() => {
      expect(createTerminalSession).toHaveBeenCalledWith("m-1", 120, 40);
    });

    expect((terminalInstances[0] as unknown as { attachCustomKeyEventHandler?: unknown }).attachCustomKeyEventHandler).toBeUndefined();
  });

  it("does not mirror xterm selections into the hidden helper textarea", async () => {
    render(<TerminalView machineName="m-1" title="m-1 shell" onSessionId={vi.fn()} />);

    await waitFor(() => {
      expect(createTerminalSession).toHaveBeenCalledWith("m-1", 120, 40);
    });

    const terminal = terminalInstances[0];
    terminal.hasSelection.mockReturnValue(true);
    terminal.getSelection.mockReturnValue("echo hello");

    await act(async () => {
      terminal.emitSelectionChange();
    });

    expect(terminal.textarea.value).toBe("");
  });

  it("copies from the focused xterm selection on cmd+c", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(globalThis, "navigator", {
      configurable: true,
      value: {
        clipboard: {
          writeText,
        },
      },
    });

    render(<TerminalView machineName="m-1" title="m-1 shell" onSessionId={vi.fn()} />);

    await waitFor(() => {
      expect(createTerminalSession).toHaveBeenCalledWith("m-1", 120, 40);
    });

    const terminal = terminalInstances[0];
    terminal.hasSelection.mockReturnValue(true);
    terminal.getSelection.mockReturnValue("copy-test");
    terminal.textarea.focus();

    await act(async () => {
      terminal.emitSelectionChange();
    });

    await act(async () => {
      terminal.textarea.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: "c",
          metaKey: true,
          bubbles: true,
          cancelable: true,
        }),
      );
    });

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith("copy-test");
    });
  });

  it("shows a visible notice when the browser blocks clipboard access", async () => {
    const onSessionId = vi.fn();
    const writeText = vi.fn().mockRejectedValue(new Error("blocked"));
    Object.defineProperty(globalThis, "navigator", {
      configurable: true,
      value: {
        clipboard: {
          writeText,
        },
      },
    });
    Object.defineProperty(globalThis, "isSecureContext", {
      configurable: true,
      value: true,
    });

    render(<TerminalView machineName="m-1" title="m-1 shell" onSessionId={onSessionId} />);

    await waitFor(() => {
      expect(createTerminalSession).toHaveBeenCalledWith("m-1", 120, 40);
    });

    await act(async () => {
      MockWebSocket.instances[0].emit("open");
    });

    await act(async () => {
      MockWebSocket.instances[0].emit("message", {
        data: new TextEncoder().encode(`\u001b]52;c;${btoa("copy me")}\u0007`).buffer,
      });
    });

    expect(await screen.findByText("Clipboard access was blocked by the browser.")).toBeTruthy();
  });

  it("silently reattaches when a live shell socket closes", async () => {
    const onSessionId = vi.fn();
    const onConnectionStateChange = vi.fn();

    render(
      <TerminalView
        machineName="m-1"
        title="m-1 shell"
        onSessionId={onSessionId}
        onConnectionStateChange={onConnectionStateChange}
      />,
    );

    await waitFor(() => {
      expect(createTerminalSession).toHaveBeenCalledTimes(1);
    });

    await act(async () => {
      MockWebSocket.instances[0].emit("open");
    });

    await act(async () => {
      MockWebSocket.instances[0].emit("close");
    });

    expect(screen.queryByText("Shell disconnected")).toBeNull();
    expect(onConnectionStateChange).toHaveBeenCalledWith("reconnecting");

    await waitFor(() => {
      expect(attachTerminalSession).toHaveBeenCalledWith("term-1", 120, 40);
    });

    await act(async () => {
      MockWebSocket.instances[1].emit("open");
    });

    expect(onConnectionStateChange).toHaveBeenLastCalledWith("ready");
  });

  it("retries reuse when reconnecting fails and the user chooses Retry", async () => {
    attachTerminalSession.mockRejectedValueOnce(new Error("gateway down"));

    render(<TerminalView machineName="m-1" title="m-1 shell" onSessionId={vi.fn()} />);

    await waitFor(() => {
      expect(createTerminalSession).toHaveBeenCalledTimes(1);
    });

    await act(async () => {
      MockWebSocket.instances[0].emit("open");
    });

    await act(async () => {
      MockWebSocket.instances[0].emit("close");
    });

    expect(await screen.findByText("Connection failed")).toBeTruthy();

    await act(async () => {
      screen.getByRole("button", { name: "Retry" }).click();
    });

    await waitFor(() => {
      expect(attachTerminalSession).toHaveBeenCalledTimes(2);
    });
  });
});
