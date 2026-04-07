import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  consumeWheelHistorySequence,
  getWheelHistorySequence,
  shouldSuppressWheelHistory,
  TerminalView,
} from "./terminal";

const {
  attachShell,
  attachTerminalSession,
  createTerminalSession,
  shellAttachmentIDs,
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

  const shellAttachmentIDs = new Map<string, string>();

  const attachTerminalSession = vi.fn(async (sessionId: string, _cols?: number, _rows?: number) => ({
    id: sessionId,
    machine_name: "m-1",
    host_id: "fascinate-01",
    attach_url: `/v1/terminal/sessions/${sessionId}/stream?token=token-${sessionId}`,
    expires_at: "2026-03-22T00:00:00Z",
  }));

  const createTerminalSession = vi.fn(async (shellId: string, _cols?: number, _rows?: number) => {
    const attachmentID = shellId.startsWith("term-") ? shellId : "term-1";
    shellAttachmentIDs.set(shellId, attachmentID);
    return {
      id: attachmentID,
      machine_name: "m-1",
      host_id: "fascinate-01",
      attach_url: `/v1/terminal/sessions/${attachmentID}/stream?token=token-1`,
      expires_at: "2026-03-22T00:00:00Z",
    };
  });

  const attachShell = vi.fn(async (shellId: string, cols: number, rows: number) => {
    if (shellId.startsWith("term-")) {
      return attachTerminalSession(shellId, cols, rows);
    }
    const existingAttachmentID = shellAttachmentIDs.get(shellId);
    if (!existingAttachmentID) {
      return createTerminalSession(shellId, cols, rows);
    }
    return attachTerminalSession(existingAttachmentID, cols, rows);
  });

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
    attachShell,
    attachTerminalSession,
    createTerminalSession,
    shellAttachmentIDs,
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
  attachShell,
  HttpError,
  toWebSocketURL: (path: string) => `ws://example.test${path}`,
}));

function renderTerminal(
  props: Partial<{
    shellId: string;
    machineName: string;
    title: string;
    onCwdChange: (cwd: string) => void;
    onConnectionStateChange: (state: "connecting" | "ready" | "reconnecting" | "error") => void;
  }> = {},
) {
  return render(
    <TerminalView
      shellId="m-1"
      machineName="m-1"
      title="m-1 shell"
      onCwdChange={props.onCwdChange}
      onConnectionStateChange={props.onConnectionStateChange}
      {...props}
    />,
  );
}

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
    shellAttachmentIDs.clear();
    MockWebSocket.instances.length = 0;
    MockWebglAddon.instances.length = 0;
    attachShell.mockReset();
    attachTerminalSession.mockReset();
    attachTerminalSession.mockImplementation(async (sessionId: string, _cols?: number, _rows?: number) => ({
      id: sessionId,
      machine_name: "m-1",
      host_id: "fascinate-01",
      attach_url: `/v1/terminal/sessions/${sessionId}/stream?token=token-${sessionId}`,
      expires_at: "2026-03-22T00:00:00Z",
    }));
    createTerminalSession.mockReset();
    createTerminalSession.mockImplementation(async (shellId: string, _cols?: number, _rows?: number) => {
      const attachmentID = shellId.startsWith("term-") ? shellId : "term-1";
      shellAttachmentIDs.set(shellId, attachmentID);
      return {
        id: attachmentID,
        machine_name: "m-1",
        host_id: "fascinate-01",
        attach_url: `/v1/terminal/sessions/${attachmentID}/stream?token=token-1`,
        expires_at: "2026-03-22T00:00:00Z",
      };
    });
    attachShell.mockImplementation(async (shellId: string, cols: number, rows: number) => {
      if (shellId.startsWith("term-")) {
        return attachTerminalSession(shellId, cols, rows);
      }
      const existingAttachmentID = shellAttachmentIDs.get(shellId);
      if (!existingAttachmentID) {
        return createTerminalSession(shellId, cols, rows);
      }
      return attachTerminalSession(existingAttachmentID, cols, rows);
    });
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("falls back when the WebGL renderer loses context", async () => {
    renderTerminal();

    await waitFor(() => {
      expect(createTerminalSession).toHaveBeenCalledWith("m-1", 120, 40);
    });

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
    renderTerminal({ shellId: "term-existing" });

    await waitFor(() => {
      expect(attachTerminalSession).toHaveBeenCalledWith("term-existing", 120, 40);
    });
    expect(createTerminalSession).not.toHaveBeenCalled();
    expect(screen.getByText("Reconnecting…")).toBeTruthy();
  });

  it("surfaces a missing shared shell without offering to create a new one", async () => {
    attachTerminalSession.mockRejectedValueOnce(new HttpError(404, "not found"));
    renderTerminal({ shellId: "term-stale" });

    expect(await screen.findByText("Connection failed")).toBeTruthy();
    expect(screen.getByText("This shared shell is no longer available.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
    expect(createTerminalSession).not.toHaveBeenCalled();
  });

  it("updates cwd metadata from the terminal stream", async () => {
    const onCwdChange = vi.fn();

    renderTerminal({ onCwdChange });

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
    renderTerminal();

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
    renderTerminal();

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
    renderTerminal();

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
    renderTerminal();

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
    renderTerminal();

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
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(globalThis, "navigator", {
      configurable: true,
      value: {
        clipboard: {
          writeText,
        },
      },
    });

    renderTerminal();

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
    renderTerminal();

    await waitFor(() => {
      expect(createTerminalSession).toHaveBeenCalledWith("m-1", 120, 40);
    });

    expect((terminalInstances[0] as unknown as { attachCustomKeyEventHandler?: unknown }).attachCustomKeyEventHandler).toBeUndefined();
  });

  it("does not mirror xterm selections into the hidden helper textarea", async () => {
    renderTerminal();

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

    renderTerminal();

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

    renderTerminal();

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
    const onConnectionStateChange = vi.fn();

    renderTerminal({ onConnectionStateChange });

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

    renderTerminal();

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
