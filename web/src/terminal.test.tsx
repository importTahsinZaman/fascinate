import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TerminalView } from "./terminal";

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

  const attachTerminalSession = vi.fn(async () => ({
    id: "term-existing",
    machine_name: "m-1",
    host_id: "fascinate-01",
    attach_url: "/v1/terminal/sessions/term-existing/stream?token=token-existing",
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
    focus = vi.fn();
    writeln = vi.fn();
    write = vi.fn();
    refresh = vi.fn();
    open = vi.fn();
    loadAddon = vi.fn();
    dispose = vi.fn();
    onData = vi.fn(() => ({ dispose: vi.fn() }));
    onResize = vi.fn(() => ({ dispose: vi.fn() }));
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

    binaryType = "blob";
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
    terminalInstances.length = 0;
    MockWebSocket.instances.length = 0;
    MockWebglAddon.instances.length = 0;
    attachTerminalSession.mockClear();
    createTerminalSession.mockClear();
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

  it("creates a fresh session when the saved session can no longer be reattached", async () => {
    attachTerminalSession.mockRejectedValueOnce(new HttpError(404, "not found"));
    const onSessionId = vi.fn();

    render(
      <TerminalView machineName="m-1" title="m-1 shell" sessionId="term-stale" onSessionId={onSessionId} />,
    );

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

  it("shows a retry overlay when the terminal websocket fails", async () => {
    const onSessionId = vi.fn();

    render(<TerminalView machineName="m-1" title="m-1 shell" onSessionId={onSessionId} />);

    await waitFor(() => {
      expect(createTerminalSession).toHaveBeenCalledTimes(1);
    });

    await act(async () => {
      MockWebSocket.instances[0].emit("error");
    });

    expect(screen.getByText("Connection failed")).toBeTruthy();

    await act(async () => {
      screen.getByRole("button", { name: "Retry" }).click();
    });

    await waitFor(() => {
      expect(attachTerminalSession).toHaveBeenCalledWith("term-1", 120, 40);
    });
  });

  it("shows a reconnect overlay when the shell closes", async () => {
    const onSessionId = vi.fn();

    render(<TerminalView machineName="m-1" title="m-1 shell" onSessionId={onSessionId} />);

    await waitFor(() => {
      expect(createTerminalSession).toHaveBeenCalledTimes(1);
    });

    await act(async () => {
      MockWebSocket.instances[0].emit("close");
    });

    expect(screen.getByText("Shell disconnected")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Reconnect" })).toBeTruthy();
  });
});
