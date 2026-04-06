import { useEffect, useEffectEvent, useLayoutEffect, useMemo, useRef, useState } from "react";
import { Terminal } from "xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebglAddon } from "@xterm/addon-webgl";
import { attachTerminalSession, createTerminalSession, HttpError, toWebSocketURL } from "./api";
import { getMockTerminalPresentation, isMockUIEnabled } from "./mock-control-plane";

type Props = {
  machineName: string;
  title: string;
  sessionId?: string;
  onSessionId: (sessionId: string) => void;
  onCwdChange?: (cwd: string) => void;
  onConnectionStateChange?: (state: TerminalConnectionState) => void;
};

type ConnectionPhase = "connecting" | "reconnecting";

export type TerminalConnectionState = "connecting" | "ready" | "reconnecting" | "error";

type TerminalStats = {
  status: "connecting" | "ready" | "error";
  phase: ConnectionPhase;
  error: string | null;
  retryAction: "reuse" | "new" | null;
};

const cwdSequencePrefix = "\u001b]1337;FascinateCwd=";
const osc52SequencePrefix = "\u001b]52;";
const pageUpSequence = "\u001b[5~";
const pageDownSequence = "\u001b[6~";
const ansiSequencePattern = /\u001b(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~]|[ -/]*[@-~])/g;
const promptPathPattern = /^[^\s@]+@[^:\s]+:(.+?)[#$]\s?$/;
const clipboardNoticeDurationMs = 2_400;
const wheelHistoryPixelThreshold = 96;
const wheelHistoryLineThreshold = 3;
const wheelHistoryPageThreshold = 1;
const maxWheelHistoryStepsPerEvent = 3;
const automaticReconnectDelaysMs = [0, 900];

type ClipboardNotice = {
  tone: "success" | "error";
  message: string;
};

type MockPresentation = {
  cwd: string;
  lines: string[];
};

export function TerminalView({
  machineName,
  title,
  sessionId,
  onSessionId,
  onCwdChange,
  onConnectionStateChange,
}: Props) {
  const mockUIEnabled = isMockUIEnabled();
  const shellRef = useRef<HTMLDivElement | null>(null);
  const hostRef = useRef<HTMLDivElement | null>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const dataListenerRef = useRef<{ dispose(): void } | null>(null);
  const resizeListenerRef = useRef<{ dispose(): void } | null>(null);
  const selectionListenerRef = useRef<{ dispose(): void } | null>(null);
  const webglAddonRef = useRef<WebglAddon | null>(null);
  const webglContextLossRef = useRef<{ dispose(): void } | null>(null);
  const sessionIdRef = useRef(sessionId ?? "");
  const selectionTextRef = useRef("");
  const decoderRef = useRef<TextDecoder | null>(null);
  const pendingMetadataRef = useRef("");
  const promptLineRef = useRef("");
  const clipboardNoticeTimerRef = useRef<number | null>(null);
  const wheelHistoryRemainderRef = useRef(0);
  const reconnectTimerRef = useRef<number | null>(null);
  const ignoreSocketCloseRef = useRef(false);
  const hasConnectedRef = useRef(false);
  const automaticReconnectAttemptsRef = useRef(0);
  const [mockPresentation, setMockPresentation] = useState<MockPresentation | null>(null);
  const [connectionAttempt, setConnectionAttempt] = useState(0);
  const [stats, setStats] = useState<TerminalStats>({
    status: "connecting",
    phase: sessionId ? "reconnecting" : "connecting",
    error: null,
    retryAction: null,
  });
  const [clipboardNotice, setClipboardNotice] = useState<ClipboardNotice | null>(null);

  const label = useMemo(() => `${title} (${machineName})`, [machineName, title]);
  const persistSessionId = useEffectEvent((value: string) => {
    onSessionId(value);
  });
  const persistCwd = useEffectEvent((value: string) => {
    onCwdChange?.(value);
  });
  const reportConnectionState = useEffectEvent((value: TerminalConnectionState) => {
    onConnectionStateChange?.(value);
  });
  const showClipboardNotice = useEffectEvent((notice: ClipboardNotice) => {
    setClipboardNotice(notice);
    if (clipboardNoticeTimerRef.current) {
      window.clearTimeout(clipboardNoticeTimerRef.current);
    }
    clipboardNoticeTimerRef.current = window.setTimeout(() => {
      setClipboardNotice(null);
      clipboardNoticeTimerRef.current = null;
    }, clipboardNoticeDurationMs);
  });
  const copyToLocalClipboard = useEffectEvent(async (value: string) => {
    if (typeof navigator === "undefined" || typeof navigator.clipboard?.writeText !== "function") {
      showClipboardNotice({
        tone: "error",
        message: "Clipboard copy is unavailable in this browser terminal.",
      });
      return;
    }

    try {
      await navigator.clipboard.writeText(value);
      showClipboardNotice({
        tone: "success",
        message: "Copied to your local clipboard.",
      });
    } catch {
      showClipboardNotice({
        tone: "error",
        message: globalThis.isSecureContext
          ? "Clipboard access was blocked by the browser."
          : "Clipboard copy requires a secure browser context.",
      });
    }
  });
  const retryConnection = useEffectEvent((mode: "reuse" | "new") => {
    if (reconnectTimerRef.current !== null) {
      window.clearTimeout(reconnectTimerRef.current);
      reconnectTimerRef.current = null;
    }
    if (mode === "new") {
      sessionIdRef.current = "";
      persistCwd("");
      automaticReconnectAttemptsRef.current = 0;
    }
    setStats({
      status: "connecting",
      phase: mode === "reuse" && sessionIdRef.current !== "" ? "reconnecting" : "connecting",
      error: null,
      retryAction: null,
    });
    setConnectionAttempt((current) => current + 1);
  });
  const scheduleAutomaticReconnect = useEffectEvent(() => {
    if (sessionIdRef.current === "") {
      return;
    }
    if (automaticReconnectAttemptsRef.current >= automaticReconnectDelaysMs.length) {
      setStats({
        status: "error",
        phase: "reconnecting",
        error: "The shell could not be restored. Click Retry to reconnect.",
        retryAction: "reuse",
      });
      return;
    }

    const delay = automaticReconnectDelaysMs[automaticReconnectAttemptsRef.current] ?? 0;
    automaticReconnectAttemptsRef.current += 1;
    setStats({
      status: "connecting",
      phase: "reconnecting",
      error: null,
      retryAction: null,
    });
    reconnectTimerRef.current = window.setTimeout(() => {
      reconnectTimerRef.current = null;
      retryConnection("reuse");
    }, delay);
  });
  const retryReconnectOnInteraction = useEffectEvent(() => {
    if (stats.status !== "error" || stats.retryAction !== "reuse" || sessionIdRef.current === "") {
      return;
    }
    retryConnection("reuse");
  });

  useEffect(() => {
    if (sessionId) {
      sessionIdRef.current = sessionId;
    }
  }, [sessionId]);

  useEffect(() => {
    reportConnectionState(deriveConnectionState(stats));
  }, [reportConnectionState, stats]);

  useLayoutEffect(() => {
    if (mockUIEnabled) {
      return;
    }
    if (!hostRef.current || terminalRef.current) {
      return;
    }

    const terminal = new Terminal({
      cursorBlink: true,
      scrollback: 3000,
      fontSize: 15,
      fontFamily: '"SF Mono", "SFMono-Regular", ui-monospace, Menlo, Consolas, monospace',
      theme: {
        background: "#161616",
        foreground: "#f3f1eb",
        cursor: "#f3f1eb",
        cursorAccent: "#161616",
        selectionBackground: "rgba(255, 255, 255, 0.14)",
        selectionInactiveBackground: "rgba(255, 255, 255, 0.14)",
        green: "#18E37D",
        brightGreen: "#18E37D",
      },
    });
    const fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    try {
      const webglAddon = new WebglAddon();
      terminal.loadAddon(webglAddon);
      webglAddonRef.current = webglAddon;
      webglContextLossRef.current = webglAddon.onContextLoss(() => {
        webglContextLossRef.current?.dispose();
        webglContextLossRef.current = null;
        webglAddon.dispose();
        if (webglAddonRef.current === webglAddon) {
          webglAddonRef.current = null;
        }
        terminal.write("\r\n\x1b[90mrenderer fallback enabled after graphics context reset\x1b[0m\r\n");
        terminal.refresh(0, Math.max(0, terminal.rows - 1));
      });
    } catch {
      // fall back to default renderer
    }
    terminal.open(hostRef.current);
    const hostElement = hostRef.current;
    const handleWheelCapture = (event: WheelEvent) => {
      const historyScrollResult = consumeWheelHistorySequence(
        terminal,
        event,
        wheelHistoryRemainderRef.current,
      );
      wheelHistoryRemainderRef.current = historyScrollResult.remainder;
      if (!historyScrollResult.consume) {
        return;
      }
      event.preventDefault();
      event.stopImmediatePropagation();
      const historyScrollSequence = historyScrollResult.sequence;
      if (!historyScrollSequence) {
        return;
      }
      const socket = socketRef.current;
      if (socket?.readyState === WebSocket.OPEN) {
        socket.send(new TextEncoder().encode(historyScrollSequence));
      }
    };
    hostElement.addEventListener("wheel", handleWheelCapture, { capture: true, passive: false });
    selectionListenerRef.current = terminal.onSelectionChange(() => {
      selectionTextRef.current = terminal.hasSelection() ? terminal.getSelection() : "";
    });
    fitAddon.fit();
    terminalRef.current = terminal;
    fitRef.current = fitAddon;

    return () => {
      dataListenerRef.current?.dispose();
      resizeListenerRef.current?.dispose();
      selectionListenerRef.current?.dispose();
      webglContextLossRef.current?.dispose();
      webglAddonRef.current?.dispose();
      ignoreSocketCloseRef.current = true;
      socketRef.current?.close();
      if (clipboardNoticeTimerRef.current) {
        window.clearTimeout(clipboardNoticeTimerRef.current);
      }
      hostElement.removeEventListener("wheel", handleWheelCapture, true);
      terminal.dispose();
      terminalRef.current = null;
      fitRef.current = null;
      dataListenerRef.current = null;
      resizeListenerRef.current = null;
      selectionListenerRef.current = null;
      webglContextLossRef.current = null;
      webglAddonRef.current = null;
      decoderRef.current = null;
      pendingMetadataRef.current = "";
      promptLineRef.current = "";
      selectionTextRef.current = "";
      wheelHistoryRemainderRef.current = 0;
      clipboardNoticeTimerRef.current = null;
      if (reconnectTimerRef.current !== null) {
        window.clearTimeout(reconnectTimerRef.current);
        reconnectTimerRef.current = null;
      }
    };
  }, [mockUIEnabled]);

  useEffect(() => {
    if (mockUIEnabled) {
      return;
    }
    const shellElement = shellRef.current;
    if (!shellElement) {
      return;
    }

    const handlePointerDown = () => {
      retryReconnectOnInteraction();
    };
    const handleFocus = () => {
      retryReconnectOnInteraction();
    };
    const handleVisibilityChange = () => {
      if (document.visibilityState === "visible") {
        retryReconnectOnInteraction();
      }
    };

    shellElement.addEventListener("pointerdown", handlePointerDown, { capture: true });
    shellElement.addEventListener("focusin", handleFocus);
    window.addEventListener("focus", handleFocus);
    window.addEventListener("online", handleFocus);
    document.addEventListener("visibilitychange", handleVisibilityChange);

    return () => {
      shellElement.removeEventListener("pointerdown", handlePointerDown, true);
      shellElement.removeEventListener("focusin", handleFocus);
      window.removeEventListener("focus", handleFocus);
      window.removeEventListener("online", handleFocus);
      document.removeEventListener("visibilitychange", handleVisibilityChange);
    };
  }, [mockUIEnabled, retryReconnectOnInteraction]);

  useEffect(() => {
    if (!mockUIEnabled) {
      return;
    }

    let cancelled = false;
    const existingSessionId = sessionIdRef.current;

    const start = async () => {
      try {
        setStats({
          status: "connecting",
          phase: existingSessionId !== "" ? "reconnecting" : "connecting",
          error: null,
          retryAction: null,
        });
        const init =
          existingSessionId !== ""
            ? await attachTerminalSession(existingSessionId, 120, 40)
            : await createTerminalSession(machineName, 120, 40);
        if (cancelled) {
          return;
        }
        sessionIdRef.current = init.id;
        persistSessionId(init.id);
        const presentation = getMockTerminalPresentation(init.id, machineName);
        persistCwd(presentation.cwd);
        setMockPresentation({
          cwd: presentation.cwd,
          lines: presentation.lines,
        });
        hasConnectedRef.current = true;
        automaticReconnectAttemptsRef.current = 0;
        setStats({
          status: "ready",
          phase: existingSessionId !== "" ? "reconnecting" : "connecting",
          error: null,
          retryAction: null,
        });
      } catch (error) {
        if (cancelled) {
          return;
        }
        const failure = describeTerminalConnectionError(error, existingSessionId !== "");
        setStats({
          status: "error",
          phase: sessionIdRef.current !== "" ? "reconnecting" : "connecting",
          error: failure.message,
          retryAction: failure.retryAction,
        });
      }
    };

    start();

    return () => {
      cancelled = true;
    };
  }, [connectionAttempt, machineName, mockUIEnabled, persistCwd, persistSessionId]);

  useEffect(() => {
    if (mockUIEnabled) {
      return;
    }
    const handleCopy = (event: ClipboardEvent) => {
      const terminal = terminalRef.current;
      if (!terminal || event.target !== terminal.textarea) {
        return;
      }

      const selection = selectedTerminalText(terminal, selectionTextRef.current);
      if (!selection) {
        return;
      }

      if (event.clipboardData) {
        event.preventDefault();
        event.stopPropagation();
        event.clipboardData.setData("text/plain", selection);
      }
    };

    const handleKeyDown = (event: KeyboardEvent) => {
      const terminal = terminalRef.current;
      if (!terminal || event.target !== terminal.textarea || !shouldCopyShortcut(event)) {
        return;
      }

      const selection = selectedTerminalText(terminal, selectionTextRef.current);
      if (!selection) {
        return;
      }

      event.preventDefault();
      event.stopPropagation();
      void copyToLocalClipboard(selection);
    };

    document.addEventListener("copy", handleCopy);
    document.addEventListener("keydown", handleKeyDown, true);
    return () => {
      document.removeEventListener("copy", handleCopy);
      document.removeEventListener("keydown", handleKeyDown, true);
    };
  }, [copyToLocalClipboard, mockUIEnabled]);

  useEffect(() => {
    if (mockUIEnabled) {
      return;
    }
    if (!terminalRef.current || !fitRef.current) {
      return;
    }

    let resizeObserver: ResizeObserver | undefined;
    let disposed = false;

    const start = async () => {
      const existingSessionId = sessionIdRef.current;
      try {
        const cols = terminalRef.current?.cols ?? 120;
        const rows = terminalRef.current?.rows ?? 40;
        let socketOpened = false;
        let sawSocketError = false;
        setStats({
          status: "connecting",
          phase: existingSessionId !== "" ? "reconnecting" : "connecting",
          error: null,
          retryAction: null,
        });
        const init =
          existingSessionId !== ""
            ? await attachTerminalSession(existingSessionId, cols, rows)
            : await createTerminalSession(machineName, cols, rows);
        if (disposed) {
          return;
        }

        sessionIdRef.current = init.id;
        persistSessionId(init.id);

        const socket = new WebSocket(toWebSocketURL(init.attach_url));
        ignoreSocketCloseRef.current = false;
        socket.binaryType = "arraybuffer";
        socketRef.current = socket;

        terminalRef.current?.writeln(`\x1b[90mconnecting to ${label}\x1b[0m`);

        socket.addEventListener("open", () => {
          if (disposed || !terminalRef.current) {
            return;
          }
          socketOpened = true;
          hasConnectedRef.current = true;
          automaticReconnectAttemptsRef.current = 0;
          terminalRef.current.focus();
          dataListenerRef.current?.dispose();
          resizeListenerRef.current?.dispose();
          dataListenerRef.current = terminalRef.current.onData((value) => {
            socket.send(new TextEncoder().encode(value));
          });
          resizeListenerRef.current = terminalRef.current.onResize(({ cols, rows }) => {
            socket.send(JSON.stringify({ type: "resize", cols, rows }));
          });
          setStats((current) => ({ ...current, status: "ready", error: null, retryAction: null }));
        });

        socket.addEventListener("message", (event) => {
          if (disposed) {
            return;
          }
          if (typeof event.data === "string") {
            try {
              const body = JSON.parse(event.data) as {
                type: string;
                error?: string;
              };
              if (body.type === "error") {
                setStats((current) => ({
                  ...current,
                  status: "error",
                  error: body.error ?? "terminal session failed",
                  retryAction: "reuse",
                }));
              }
            } catch {
              // ignore invalid control payloads
            }
            return;
          }

          const decoder = decoderRef.current ?? new TextDecoder();
          decoderRef.current = decoder;
          const decodedChunk = decoder.decode(new Uint8Array(event.data as ArrayBuffer), { stream: true });
          const parsedChunk = parseTerminalMetadata(pendingMetadataRef.current + decodedChunk);
          pendingMetadataRef.current = parsedChunk.pending;
          if (parsedChunk.cwd) {
            persistCwd(parsedChunk.cwd);
          }
          for (const clipboardWrite of parsedChunk.clipboardWrites) {
            void copyToLocalClipboard(clipboardWrite);
          }
          if (parsedChunk.output !== "") {
            terminalRef.current?.write(parsedChunk.output);
            const promptMetadata = detectPromptPath(promptLineRef.current, parsedChunk.output);
            promptLineRef.current = promptMetadata.line;
            if (promptMetadata.cwd) {
              persistCwd(promptMetadata.cwd);
            }
          }
        });

        socket.addEventListener("close", () => {
          if (disposed || ignoreSocketCloseRef.current) {
            return;
          }
          socketRef.current = null;
          dataListenerRef.current?.dispose();
          resizeListenerRef.current?.dispose();
          dataListenerRef.current = null;
          resizeListenerRef.current = null;
          if (!socketOpened) {
            setStats({
              status: "error",
              phase: existingSessionId !== "" ? "reconnecting" : "connecting",
              error: sawSocketError ? "websocket connection failed" : "The shell connection closed before it was ready.",
              retryAction: "reuse",
            });
            return;
          }
          scheduleAutomaticReconnect();
        });

        socket.addEventListener("error", () => {
          sawSocketError = true;
        });
      } catch (error) {
        const failure = describeTerminalConnectionError(error, sessionIdRef.current !== "");
        setStats({
          status: "error",
          phase: sessionIdRef.current !== "" ? "reconnecting" : "connecting",
          error: failure.message,
          retryAction: failure.retryAction,
        });
      }
    };

    start();

    if (hostRef.current) {
      resizeObserver = new ResizeObserver(() => {
        fitRef.current?.fit();
      });
      resizeObserver.observe(hostRef.current);
    }

    return () => {
      disposed = true;
      if (reconnectTimerRef.current !== null) {
        window.clearTimeout(reconnectTimerRef.current);
        reconnectTimerRef.current = null;
      }
      dataListenerRef.current?.dispose();
      resizeListenerRef.current?.dispose();
      dataListenerRef.current = null;
      resizeListenerRef.current = null;
      resizeObserver?.disconnect();
      ignoreSocketCloseRef.current = true;
      socketRef.current?.close();
      socketRef.current = null;
    };
  }, [connectionAttempt, label, machineName, mockUIEnabled]);

  const overlay = getTerminalOverlay(stats, hasConnectedRef.current);

  return (
    <div className="terminal-shell" ref={shellRef}>
      {mockUIEnabled ? (
        <div className="terminal-host terminal-host--mock" data-terminal-mode="mock">
          <div className="terminal-mock-meta">
            <span>{mockPresentation?.cwd?.replace("/home/ubuntu", "~") ?? `~/${machineName}`}</span>
            <span>mock PTY</span>
          </div>
          <pre className="terminal-mock-output">
            {(mockPresentation?.lines ?? ["Starting mock shell…"]).join("\n")}
          </pre>
        </div>
      ) : (
        <div className="terminal-host" ref={hostRef} />
      )}
      {clipboardNotice ? (
        <div className="terminal-notice" data-tone={clipboardNotice.tone} role="status" aria-live="polite">
          {clipboardNotice.message}
        </div>
      ) : null}
      {overlay ? (
        <div className="terminal-overlay" data-state={overlay.tone} role="status" aria-live="polite">
          <div className="terminal-overlay-card">
            <p className="terminal-overlay-title">{overlay.title}</p>
            {overlay.description ? <p className="terminal-overlay-description">{overlay.description}</p> : null}
            {overlay.action ? (
              <button
                type="button"
                className="terminal-overlay-action"
                onClick={() => retryConnection(overlay.action)}
              >
                {overlay.actionLabel}
              </button>
            ) : null}
          </div>
        </div>
      ) : null}
    </div>
  );
}

function getTerminalOverlay(stats: TerminalStats, hasConnected: boolean) {
  if (stats.status === "ready") {
    return null;
  }
  if (stats.status === "connecting") {
    if (stats.phase === "reconnecting" && hasConnected) {
      return null;
    }
    return {
      title: stats.phase === "reconnecting" ? "Reconnecting…" : "Connecting…",
      description: stats.phase === "reconnecting" ? "Restoring your shell session." : "Starting a fresh shell session.",
      tone: "connecting" as const,
    };
  }
  if (stats.retryAction === "new") {
    return {
      title: "Shell ended",
      description: stats.error ?? "The previous shell can no longer be restored.",
      action: "new" as const,
      actionLabel: "Start new shell",
      tone: "error" as const,
    };
  }
  return {
    title: "Connection failed",
    description: stats.error ?? "The shell could not be opened.",
    action: stats.retryAction ?? "reuse",
    actionLabel: "Retry",
    tone: "error" as const,
  };
}

function deriveConnectionState(stats: TerminalStats): TerminalConnectionState {
  if (stats.status === "ready") {
    return "ready";
  }
  if (stats.status === "error") {
    return "error";
  }
  return stats.phase === "reconnecting" ? "reconnecting" : "connecting";
}

function describeTerminalConnectionError(error: unknown, reattaching: boolean) {
  if (reattaching && error instanceof HttpError && (error.status === 400 || error.status === 404)) {
    return {
      message: "The previous shell can no longer be restored.",
      retryAction: "new" as const,
    };
  }
  return {
    message: error instanceof Error ? error.message : "failed to create terminal session",
    retryAction: "reuse" as const,
  };
}

function shouldCopyShortcut(event: KeyboardEvent) {
  if (event.type !== "keydown") {
    return false;
  }
  return !event.altKey && event.key.toLowerCase() === "c" && (event.ctrlKey || event.metaKey);
}

function selectedTerminalText(terminal: Terminal, cachedSelection: string) {
  const textarea = terminal.textarea;
  if (!textarea || !textarea.classList.contains("xterm-helper-textarea")) {
    return "";
  }
  if (cachedSelection) {
    return cachedSelection;
  }
  if (terminal.hasSelection()) {
    return terminal.getSelection();
  }
  return "";
}

export function shouldSuppressWheelHistory(terminal: Terminal, event: WheelEvent) {
  if (event.deltaY === 0 || event.shiftKey) {
    return false;
  }
  if (terminal.modes.mouseTrackingMode !== "none") {
    return false;
  }
  const activeBuffer = terminal.buffer.active;
  return activeBuffer.baseY === 0 && activeBuffer.viewportY === 0;
}

export function getWheelHistorySequence(terminal: Terminal, event: WheelEvent) {
  if (!shouldSuppressWheelHistory(terminal, event)) {
    return "";
  }
  return event.deltaY < 0 ? pageUpSequence : pageDownSequence;
}

export function consumeWheelHistorySequence(terminal: Terminal, event: WheelEvent, remainder: number) {
  if (!shouldSuppressWheelHistory(terminal, event)) {
    return { sequence: "", remainder: 0, consume: false };
  }

  const threshold = wheelHistoryDeltaThreshold(event);
  const nextRemainder = remainder + event.deltaY;
  const steps = Math.min(
    maxWheelHistoryStepsPerEvent,
    Math.trunc(Math.abs(nextRemainder) / threshold),
  );
  if (steps === 0) {
    return { sequence: "", remainder: nextRemainder, consume: true };
  }

  const sequence = nextRemainder < 0 ? pageUpSequence : pageDownSequence;
  const remainingMagnitude = Math.abs(nextRemainder) - steps * threshold;
  return {
    sequence: sequence.repeat(steps),
    remainder: Math.sign(nextRemainder) * remainingMagnitude,
    consume: true,
  };
}

function wheelHistoryDeltaThreshold(event: WheelEvent) {
  switch (event.deltaMode) {
    case WheelEvent.DOM_DELTA_LINE:
      return wheelHistoryLineThreshold;
    case WheelEvent.DOM_DELTA_PAGE:
      return wheelHistoryPageThreshold;
    default:
      return wheelHistoryPixelThreshold;
  }
}

function parseTerminalMetadata(chunk: string) {
  let output = "";
  let cwd: string | undefined;
  const clipboardWrites: string[] = [];
  let index = 0;

  while (index < chunk.length) {
    const nextSequence = findNextMetadataSequence(chunk, index);
    if (!nextSequence) {
      output += chunk.slice(index);
      return { output, pending: "", cwd, clipboardWrites };
    }

    output += chunk.slice(index, nextSequence.start);
    const contentStart = nextSequence.start + nextSequence.prefix.length;
    const bellIndex = chunk.indexOf("\u0007", contentStart);
    const stringTerminatorIndex = chunk.indexOf("\u001b\\", contentStart);
    let sequenceEnd = -1;
    let terminatorLength = 0;

    if (bellIndex !== -1 && (stringTerminatorIndex === -1 || bellIndex < stringTerminatorIndex)) {
      sequenceEnd = bellIndex;
      terminatorLength = 1;
    } else if (stringTerminatorIndex !== -1) {
      sequenceEnd = stringTerminatorIndex;
      terminatorLength = 2;
    }

    if (sequenceEnd === -1) {
      return { output, pending: chunk.slice(nextSequence.start), cwd, clipboardWrites };
    }

    const value = chunk.slice(contentStart, sequenceEnd);
    if (nextSequence.prefix === cwdSequencePrefix) {
      cwd = value.trim();
    }
    if (nextSequence.prefix === osc52SequencePrefix) {
      const clipboardValue = decodeOsc52Write(value);
      if (clipboardValue !== null) {
        clipboardWrites.push(clipboardValue);
      }
    }
    index = sequenceEnd + terminatorLength;
  }

  return { output, pending: "", cwd, clipboardWrites };
}

function findNextMetadataSequence(chunk: string, index: number) {
  const sequenceStarts = [cwdSequencePrefix, osc52SequencePrefix]
    .map((prefix) => ({ prefix, start: chunk.indexOf(prefix, index) }))
    .filter((entry) => entry.start !== -1)
    .sort((left, right) => left.start - right.start);

  if (sequenceStarts.length === 0) {
    return null;
  }
  return sequenceStarts[0];
}

function decodeOsc52Write(value: string) {
  const separatorIndex = value.indexOf(";");
  if (separatorIndex === -1) {
    return null;
  }
  const encoded = value.slice(separatorIndex + 1).trim();
  if (!encoded || encoded === "?") {
    return null;
  }
  try {
    const binary = globalThis.atob(encoded);
    const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0));
    return new TextDecoder().decode(bytes);
  } catch {
    return null;
  }
}

function detectPromptPath(previousLine: string, output: string) {
  const plainText = output.replace(ansiSequencePattern, "");
  if (plainText === "") {
    return { line: previousLine, cwd: null as string | null };
  }

  const segments = plainText.split(/\r\n|\n|\r/g);
  let currentLine = previousLine;
  for (let index = 0; index < segments.length; index += 1) {
    const segment = segments[index];
    if (index > 0) {
      currentLine = "";
    }
    currentLine += segment;
  }

  const normalizedLine = promptLineRefSafeguard(currentLine);
  const match = normalizedLine.match(promptPathPattern);
  return {
    line: normalizedLine,
    cwd: match?.[1]?.trim() || null,
  };
}

function promptLineRefSafeguard(value: string) {
  return value.replace(/\u0000/g, "").slice(-512);
}
