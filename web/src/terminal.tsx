import { useEffect, useEffectEvent, useLayoutEffect, useMemo, useRef, useState } from "react";
import { Terminal } from "xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebglAddon } from "@xterm/addon-webgl";
import { attachTerminalSession, createTerminalSession, HttpError, toWebSocketURL } from "./api";

type Props = {
  machineName: string;
  title: string;
  sessionId?: string;
  onSessionId: (sessionId: string) => void;
  onCwdChange?: (cwd: string) => void;
};

type ConnectionPhase = "connecting" | "reconnecting";

type TerminalStats = {
  status: "connecting" | "ready" | "closed" | "error";
  phase: ConnectionPhase;
  error: string | null;
};

const cwdSequencePrefix = "\u001b]1337;FascinateCwd=";
const osc52SequencePrefix = "\u001b]52;";
const ansiSequencePattern = /\u001b(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~]|[ -/]*[@-~])/g;
const promptPathPattern = /^[^\s@]+@[^:\s]+:(.+?)[#$]\s?$/;
const clipboardNoticeDurationMs = 2_400;

type ClipboardNotice = {
  tone: "success" | "error";
  message: string;
};

export function TerminalView({ machineName, title, sessionId, onSessionId, onCwdChange }: Props) {
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
  const [connectionAttempt, setConnectionAttempt] = useState(0);
  const [stats, setStats] = useState<TerminalStats>({
    status: "connecting",
    phase: sessionId ? "reconnecting" : "connecting",
    error: null,
  });
  const [clipboardNotice, setClipboardNotice] = useState<ClipboardNotice | null>(null);

  const label = useMemo(() => `${title} (${machineName})`, [machineName, title]);
  const persistSessionId = useEffectEvent((value: string) => {
    onSessionId(value);
  });
  const persistCwd = useEffectEvent((value: string) => {
    onCwdChange?.(value);
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
    if (mode === "new") {
      sessionIdRef.current = "";
      persistCwd("");
    }
    setStats({
      status: "connecting",
      phase: mode === "reuse" && sessionIdRef.current !== "" ? "reconnecting" : "connecting",
      error: null,
    });
    setConnectionAttempt((current) => current + 1);
  });

  useEffect(() => {
    if (sessionId) {
      sessionIdRef.current = sessionId;
    }
  }, [sessionId]);

  useLayoutEffect(() => {
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
      socketRef.current?.close();
      if (clipboardNoticeTimerRef.current) {
        window.clearTimeout(clipboardNoticeTimerRef.current);
      }
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
      clipboardNoticeTimerRef.current = null;
    };
  }, []);

  useEffect(() => {
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
  }, [copyToLocalClipboard]);

  useEffect(() => {
    if (!terminalRef.current || !fitRef.current) {
      return;
    }

    let resizeObserver: ResizeObserver | undefined;
    let disposed = false;

    const start = async () => {
      try {
        const cols = terminalRef.current?.cols ?? 120;
        const rows = terminalRef.current?.rows ?? 40;
        const existingSessionId = sessionIdRef.current;
        setStats({
          status: "connecting",
          phase: existingSessionId !== "" ? "reconnecting" : "connecting",
          error: null,
        });
        const init =
          existingSessionId !== ""
            ? await attachTerminalSession(existingSessionId, cols, rows).catch(async (error) => {
                if (!(error instanceof HttpError) || (error.status !== 400 && error.status !== 404)) {
                  throw error;
                }
                return createTerminalSession(machineName, cols, rows);
              })
            : await createTerminalSession(machineName, cols, rows);
        if (disposed) {
          return;
        }

        sessionIdRef.current = init.id;
        persistSessionId(init.id);

        const socket = new WebSocket(toWebSocketURL(init.attach_url));
        socket.binaryType = "arraybuffer";
        socketRef.current = socket;

        terminalRef.current?.writeln(`\x1b[90mconnecting to ${label}\x1b[0m`);

        socket.addEventListener("open", () => {
          if (!terminalRef.current) {
            return;
          }
          terminalRef.current.focus();
          dataListenerRef.current?.dispose();
          resizeListenerRef.current?.dispose();
          dataListenerRef.current = terminalRef.current.onData((value) => {
            socket.send(new TextEncoder().encode(value));
          });
          resizeListenerRef.current = terminalRef.current.onResize(({ cols, rows }) => {
            socket.send(JSON.stringify({ type: "resize", cols, rows }));
          });
          setStats((current) => ({ ...current, status: "ready", error: null }));
        });

        socket.addEventListener("message", (event) => {
          if (typeof event.data === "string") {
            try {
              const body = JSON.parse(event.data) as {
                type: string;
                error?: string;
              };
              if (body.type === "exit") {
                setStats((current) => ({
                  ...current,
                  status: current.error ? "error" : "closed",
                }));
              }
              if (body.type === "error") {
                setStats((current) => ({
                  ...current,
                  status: "error",
                  error: body.error ?? "terminal session failed",
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
          setStats((current) => ({
            ...current,
            status: current.error ? "error" : "closed",
          }));
        });

        socket.addEventListener("error", () => {
          setStats((current) => ({
            ...current,
            status: "error",
            error: current.error ?? "websocket connection failed",
          }));
        });
      } catch (error) {
        setStats({
          status: "error",
          phase: sessionIdRef.current !== "" ? "reconnecting" : "connecting",
          error: error instanceof Error ? error.message : "failed to create terminal session",
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
      dataListenerRef.current?.dispose();
      resizeListenerRef.current?.dispose();
      dataListenerRef.current = null;
      resizeListenerRef.current = null;
      resizeObserver?.disconnect();
      socketRef.current?.close();
      socketRef.current = null;
    };
  }, [connectionAttempt, label, machineName]);

  const overlay = getTerminalOverlay(stats);

  return (
    <div className="terminal-shell">
      <div className="terminal-host" ref={hostRef} />
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

function getTerminalOverlay(stats: TerminalStats) {
  if (stats.status === "ready") {
    return null;
  }
  if (stats.status === "connecting") {
    return {
      title: stats.phase === "reconnecting" ? "Reconnecting…" : "Connecting…",
      description: stats.phase === "reconnecting" ? "Restoring your shell session." : "Starting a fresh shell session.",
      tone: "connecting" as const,
    };
  }
  if (stats.status === "closed") {
    return {
      title: "Shell disconnected",
      description: "Reconnect to continue working in this shell.",
      action: "reuse" as const,
      actionLabel: "Reconnect",
      tone: "closed" as const,
    };
  }
  return {
    title: "Connection failed",
    description: stats.error ?? "The shell could not be opened.",
    action: "reuse" as const,
    actionLabel: "Retry",
    tone: "error" as const,
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
