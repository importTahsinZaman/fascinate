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

type TerminalStats = {
  status: "connecting" | "ready" | "closed" | "error";
  attachMs: number | null;
  rttMs: number | null;
  error: string | null;
  note: string | null;
};

const cwdSequencePrefix = "\u001b]1337;FascinateCwd=";
const ansiSequencePattern = /\u001b(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~]|[ -/]*[@-~])/g;
const promptPathPattern = /^[^\s@]+@[^:\s]+:(.+?)[#$]\s?$/;

export function TerminalView({ machineName, title, sessionId, onSessionId, onCwdChange }: Props) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const terminalRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const dataListenerRef = useRef<{ dispose(): void } | null>(null);
  const resizeListenerRef = useRef<{ dispose(): void } | null>(null);
  const webglAddonRef = useRef<WebglAddon | null>(null);
  const webglContextLossRef = useRef<{ dispose(): void } | null>(null);
  const sessionIdRef = useRef(sessionId ?? "");
  const decoderRef = useRef<TextDecoder | null>(null);
  const pendingMetadataRef = useRef("");
  const promptLineRef = useRef("");
  const [stats, setStats] = useState<TerminalStats>({
    status: "connecting",
    attachMs: null,
    rttMs: null,
    error: null,
    note: null,
  });

  const label = useMemo(() => `${title} (${machineName})`, [machineName, title]);
  const persistSessionId = useEffectEvent((value: string) => {
    onSessionId(value);
  });
  const persistCwd = useEffectEvent((value: string) => {
    onCwdChange?.(value);
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
        background: "#121212",
        foreground: "#f3f1eb",
        cursor: "#f3f1eb",
        cursorAccent: "#121212",
        selectionBackground: "rgba(255, 255, 255, 0.14)",
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
        setStats((current) => ({
          ...current,
          note: "renderer fallback enabled",
        }));
      });
    } catch {
      // fall back to default renderer
    }
    terminal.open(hostRef.current);
    fitAddon.fit();
    terminalRef.current = terminal;
    fitRef.current = fitAddon;

    return () => {
      dataListenerRef.current?.dispose();
      resizeListenerRef.current?.dispose();
      webglContextLossRef.current?.dispose();
      webglAddonRef.current?.dispose();
      socketRef.current?.close();
      terminal.dispose();
      terminalRef.current = null;
      fitRef.current = null;
      dataListenerRef.current = null;
      resizeListenerRef.current = null;
      webglContextLossRef.current = null;
      webglAddonRef.current = null;
      decoderRef.current = null;
      pendingMetadataRef.current = "";
      promptLineRef.current = "";
    };
  }, []);

  useEffect(() => {
    if (!terminalRef.current || !fitRef.current) {
      return;
    }

    const startedAt = performance.now();
    let firstOutput = false;
    let pingHandle: number | undefined;
    let resizeObserver: ResizeObserver | undefined;
    let disposed = false;

    const start = async () => {
      try {
        const cols = terminalRef.current?.cols ?? 120;
        const rows = terminalRef.current?.rows ?? 40;
        const existingSessionId = sessionIdRef.current;
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
          setStats((current) => ({
            ...current,
            status: "ready",
            error: null,
            note: existingSessionId !== "" ? "restored" : current.note,
          }));
          pingHandle = window.setInterval(() => {
            socket.send(JSON.stringify({ type: "ping", sent_at: Date.now() }));
          }, 10_000);
        });

        socket.addEventListener("message", (event) => {
          if (typeof event.data === "string") {
            try {
              const body = JSON.parse(event.data) as {
                type: string;
                sent_at?: number;
                error?: string;
              };
              const sentAt = body.sent_at;
              if (body.type === "pong" && typeof sentAt === "number") {
                setStats((current) => ({
                  ...current,
                  rttMs: Math.max(0, Date.now() - sentAt),
                }));
              }
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

          if (!firstOutput) {
            firstOutput = true;
            setStats((current) => ({
              ...current,
              attachMs: Math.round(performance.now() - startedAt),
            }));
          }
          const decoder = decoderRef.current ?? new TextDecoder();
          decoderRef.current = decoder;
          const decodedChunk = decoder.decode(new Uint8Array(event.data as ArrayBuffer), { stream: true });
          const parsedChunk = parseTerminalMetadata(pendingMetadataRef.current + decodedChunk);
          pendingMetadataRef.current = parsedChunk.pending;
          if (parsedChunk.cwd) {
            persistCwd(parsedChunk.cwd);
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
          attachMs: null,
          rttMs: null,
          error: error instanceof Error ? error.message : "failed to create terminal session",
          note: null,
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
      if (pingHandle) {
        window.clearInterval(pingHandle);
      }
      dataListenerRef.current?.dispose();
      resizeListenerRef.current?.dispose();
      dataListenerRef.current = null;
      resizeListenerRef.current = null;
      resizeObserver?.disconnect();
      socketRef.current?.close();
      socketRef.current = null;
    };
  }, [label, machineName]);

  return (
    <div className="terminal-shell">
      <div className="terminal-meta">
        <div className="terminal-meta-group">
          <span className={`terminal-pill terminal-pill-${stats.status}`}>{stats.status}</span>
          {stats.note ? <span className="terminal-pill terminal-pill-note">{stats.note}</span> : null}
          {stats.error ? <span className="terminal-pill terminal-pill-error">{stats.error}</span> : null}
        </div>
        <div className="terminal-meta-group terminal-meta-group-end">
          {stats.attachMs !== null ? <span className="terminal-pill terminal-pill-metric">{stats.attachMs}ms attach</span> : null}
          {stats.rttMs !== null ? <span className="terminal-pill terminal-pill-metric">{stats.rttMs}ms rtt</span> : null}
        </div>
      </div>
      <div className="terminal-host" ref={hostRef} />
    </div>
  );
}

function parseTerminalMetadata(chunk: string) {
  let output = "";
  let cwd: string | undefined;
  let index = 0;

  while (index < chunk.length) {
    const nextSequence = findNextMetadataSequence(chunk, index);
    if (!nextSequence) {
      output += chunk.slice(index);
      return { output, pending: "", cwd };
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
      return { output, pending: chunk.slice(nextSequence.start), cwd };
    }

    const value = chunk.slice(contentStart, sequenceEnd).trim();
    if (nextSequence.prefix === cwdSequencePrefix) {
      cwd = value;
    }
    index = sequenceEnd + terminatorLength;
  }

  return { output, pending: "", cwd };
}

function findNextMetadataSequence(chunk: string, index: number) {
  const cwdStart = chunk.indexOf(cwdSequencePrefix, index);
  if (cwdStart === -1) {
    return null;
  }
  return { start: cwdStart, prefix: cwdSequencePrefix };
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
