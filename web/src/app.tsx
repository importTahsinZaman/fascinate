import { Suspense, lazy, startTransition, useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  cloneMachine,
  createMachine,
  createSnapshot,
  deleteEnvVar,
  deleteMachine,
  deleteSnapshot,
  deleteTerminalSession,
  getDefaultWorkspace,
  getMachineEnv,
  getSession,
  listEnvVars,
  listMachines,
  listSnapshots,
  logout,
  requestLoginCode,
  saveDefaultWorkspace,
  setEnvVar,
  verifyLogin,
  type WorkspaceViewport,
  type WorkspaceWindow,
} from "./api";
import { useWorkspaceStore } from "./store";

const TerminalView = lazy(async () => import("./terminal").then((module) => ({ default: module.TerminalView })));

type SidebarTab = "machines" | "snapshots" | "env";

type WorkspaceBounds = {
  width: number;
  height: number;
};

const workspaceCanvasSize = { width: 9600, height: 6400 };
const workspaceViewportPadding = 160;
const defaultWorkspaceViewport: WorkspaceViewport = { x: 120, y: 96, scale: 1 };
const minWorkspaceScale = 0.45;
const maxWorkspaceScale = 2.2;

export function App() {
  const queryClient = useQueryClient();
  const sessionQuery = useQuery({
    queryKey: ["session"],
    queryFn: getSession,
  });

  if (sessionQuery.isLoading) {
    return <div className="app-loading">Loading Fascinate…</div>;
  }

  if (!sessionQuery.data) {
    return (
      <LoginView
        onVerified={() => {
          void queryClient.invalidateQueries({ queryKey: ["session"] });
        }}
      />
    );
  }

  return <CommandCenter email={sessionQuery.data.email} />;
}

function LoginView({ onVerified }: { onVerified: () => void }) {
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [requested, setRequested] = useState(false);

  const requestCodeMutation = useMutation({
    mutationFn: requestLoginCode,
    onSuccess: () => setRequested(true),
  });
  const verifyMutation = useMutation({
    mutationFn: ({ email, code }: { email: string; code: string }) => verifyLogin(email, code),
    onSuccess: () => onVerified(),
  });

  return (
    <main className="login-page">
      <section className="login-card">
        <div>
          <div className="eyebrow">Fascinate</div>
          <h1>Browser command center for your coding-agent VMs</h1>
          <p>Sign in with a verification code. SSH keys are no longer required for the web app.</p>
        </div>
        <label className="field">
          <span>Email</span>
          <input value={email} onChange={(event) => setEmail(event.target.value)} placeholder="you@example.com" />
        </label>
        {requested ? (
          <label className="field">
            <span>Verification code</span>
            <input value={code} onChange={(event) => setCode(event.target.value)} placeholder="123456" />
          </label>
        ) : null}
        <div className="login-actions">
          {!requested ? (
            <button
              onClick={() => requestCodeMutation.mutate(email)}
              disabled={!email || requestCodeMutation.isPending}
            >
              {requestCodeMutation.isPending ? "Sending…" : "Send code"}
            </button>
          ) : (
            <button
              onClick={() => verifyMutation.mutate({ email, code })}
              disabled={!email || !code || verifyMutation.isPending}
            >
              {verifyMutation.isPending ? "Signing in…" : "Sign in"}
            </button>
          )}
        </div>
        <StatusError mutationError={requestCodeMutation.error ?? verifyMutation.error} />
      </section>
    </main>
  );
}

function CommandCenter({ email }: { email: string }) {
  const queryClient = useQueryClient();
  const [tab, setTab] = useState<SidebarTab>("machines");
  const [machineName, setMachineName] = useState("");
  const [snapshotSource, setSnapshotSource] = useState("");
  const [envForm, setEnvForm] = useState({ key: "", value: "" });
  const [envPreview, setEnvPreview] = useState<Record<string, string> | null>(null);

  const hydrate = useWorkspaceStore((state) => state.hydrate);
  const openTerminal = useWorkspaceStore((state) => state.openTerminal);

  const machinesQuery = useQuery({
    queryKey: ["machines"],
    queryFn: listMachines,
    refetchInterval: 5_000,
  });
  const snapshotsQuery = useQuery({
    queryKey: ["snapshots"],
    queryFn: listSnapshots,
    refetchInterval: 5_000,
  });
  const envVarsQuery = useQuery({ queryKey: ["env-vars"], queryFn: listEnvVars });
  const workspaceQuery = useQuery({ queryKey: ["workspace"], queryFn: getDefaultWorkspace });

  useEffect(() => {
    if (workspaceQuery.data) {
      startTransition(() => hydrate(workspaceQuery.data));
    }
  }, [hydrate, workspaceQuery.data]);

  const createMachineMutation = useMutation({
    mutationFn: ({ name, snapshotName }: { name: string; snapshotName?: string }) =>
      createMachine(name, snapshotName),
    onSuccess: () => {
      setMachineName("");
      setSnapshotSource("");
      void queryClient.invalidateQueries({ queryKey: ["machines"] });
      void queryClient.invalidateQueries({ queryKey: ["snapshots"] });
    },
  });
  const deleteMachineMutation = useMutation({
    mutationFn: deleteMachine,
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["machines"] }),
  });
  const cloneMachineMutation = useMutation({
    mutationFn: ({ source, target }: { source: string; target: string }) => cloneMachine(source, target),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["machines"] }),
  });
  const createSnapshotMutation = useMutation({
    mutationFn: ({ machine, name }: { machine: string; name: string }) => createSnapshot(machine, name),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["snapshots"] }),
  });
  const deleteSnapshotMutation = useMutation({
    mutationFn: deleteSnapshot,
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["snapshots"] }),
  });
  const setEnvMutation = useMutation({
    mutationFn: ({ key, value }: { key: string; value: string }) => setEnvVar(key, value),
    onSuccess: () => {
      setEnvForm({ key: "", value: "" });
      void queryClient.invalidateQueries({ queryKey: ["env-vars"] });
    },
  });
  const deleteEnvMutation = useMutation({
    mutationFn: deleteEnvVar,
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["env-vars"] }),
  });

  const envPreviewMutation = useMutation({
    mutationFn: getMachineEnv,
    onSuccess: (value) => setEnvPreview(value.entries),
  });

  const machineList = machinesQuery.data ?? [];
  const snapshotList = snapshotsQuery.data ?? [];
  const envList = envVarsQuery.data ?? [];

  const sidebar = useMemo(() => {
    switch (tab) {
      case "machines":
        return (
          <>
            <section className="panel">
              <h2>Create machine</h2>
              <label className="field">
                <span>Name</span>
                <input value={machineName} onChange={(event) => setMachineName(event.target.value)} placeholder="m-1" />
              </label>
              <label className="field">
                <span>From snapshot (optional)</span>
                <select value={snapshotSource} onChange={(event) => setSnapshotSource(event.target.value)}>
                  <option value="">Base image</option>
                  {snapshotList.map((snapshot) => (
                    <option key={snapshot.id} value={snapshot.name}>
                      {snapshot.name}
                    </option>
                  ))}
                </select>
              </label>
              <button
                onClick={() => createMachineMutation.mutate({ name: machineName, snapshotName: snapshotSource || undefined })}
                disabled={!machineName || createMachineMutation.isPending}
              >
                {createMachineMutation.isPending ? "Creating…" : "Create machine"}
              </button>
              <StatusError mutationError={createMachineMutation.error} />
            </section>
            <section className="panel">
              <h2>Machines</h2>
              <div className="list">
                {machineList.map((machine) => (
                  <article key={machine.id} className="list-item">
                    <div>
                      <strong>{machine.name}</strong>
                      <div className="muted">{machine.state}</div>
                    </div>
                    <div className="actions">
                      <button onClick={() => openTerminal(machine.name, `${machine.name} shell`)}>Open shell</button>
                      <button
                        onClick={() => {
                          const target = window.prompt(`Clone ${machine.name} into:`, `${machine.name}-v2`);
                          if (target) {
                            cloneMachineMutation.mutate({ source: machine.name, target });
                          }
                        }}
                      >
                        Clone
                      </button>
                      <button
                        onClick={() => {
                          const snapshot = window.prompt(`Snapshot name for ${machine.name}:`, `${machine.name}-snapshot`);
                          if (snapshot) {
                            createSnapshotMutation.mutate({ machine: machine.name, name: snapshot });
                          }
                        }}
                      >
                        Snapshot
                      </button>
                      <button onClick={() => envPreviewMutation.mutate(machine.name)}>Env</button>
                      <button className="danger" onClick={() => deleteMachineMutation.mutate(machine.name)}>
                        Delete
                      </button>
                    </div>
                  </article>
                ))}
              </div>
              <StatusError mutationError={deleteMachineMutation.error ?? cloneMachineMutation.error ?? createSnapshotMutation.error} />
            </section>
          </>
        );
      case "snapshots":
        return (
          <section className="panel">
            <h2>Snapshots</h2>
            <div className="list">
              {snapshotList.map((snapshot) => (
                <article key={snapshot.id} className="list-item">
                  <div>
                    <strong>{snapshot.name}</strong>
                    <div className="muted">{snapshot.source_machine_name ?? "snapshot"} · {snapshot.state}</div>
                  </div>
                  <div className="actions">
                    <button
                      onClick={() => {
                        const target = window.prompt(`Create a machine from ${snapshot.name}:`, `${snapshot.name}-vm`);
                        if (target) {
                          createMachineMutation.mutate({ name: target, snapshotName: snapshot.name });
                        }
                      }}
                    >
                      Restore
                    </button>
                    <button className="danger" onClick={() => deleteSnapshotMutation.mutate(snapshot.name)}>
                      Delete
                    </button>
                  </div>
                </article>
              ))}
            </div>
            <StatusError mutationError={deleteSnapshotMutation.error} />
          </section>
        );
      case "env":
        return (
          <>
            <section className="panel">
              <h2>Env vars</h2>
              <label className="field">
                <span>Key</span>
                <input value={envForm.key} onChange={(event) => setEnvForm((current) => ({ ...current, key: event.target.value }))} placeholder="FRONTEND_URL" />
              </label>
              <label className="field">
                <span>Value</span>
                <input value={envForm.value} onChange={(event) => setEnvForm((current) => ({ ...current, value: event.target.value }))} placeholder="${FASCINATE_PUBLIC_URL}" />
              </label>
              <button
                onClick={() => setEnvMutation.mutate(envForm)}
                disabled={!envForm.key || !envForm.value || setEnvMutation.isPending}
              >
                {setEnvMutation.isPending ? "Saving…" : "Save env var"}
              </button>
              <StatusError mutationError={setEnvMutation.error} />
            </section>
            <section className="panel">
              <h2>Saved env vars</h2>
              <div className="list">
                {envList.map((entry) => (
                  <article key={entry.key} className="list-item">
                    <div>
                      <strong>{entry.key}</strong>
                      <div className="muted">{entry.value}</div>
                    </div>
                    <div className="actions">
                      <button onClick={() => setEnvForm({ key: entry.key, value: entry.value })}>Edit</button>
                      <button className="danger" onClick={() => deleteEnvMutation.mutate(entry.key)}>
                        Delete
                      </button>
                    </div>
                  </article>
                ))}
              </div>
              <StatusError mutationError={deleteEnvMutation.error} />
            </section>
          </>
        );
    }
  }, [
    cloneMachineMutation,
    createMachineMutation,
    createSnapshotMutation,
    deleteEnvMutation,
    deleteMachineMutation,
    deleteSnapshotMutation,
    envForm,
    envList,
    envPreviewMutation,
    machineList,
    machineName,
    openTerminal,
    setEnvMutation,
    snapshotList,
    snapshotSource,
    tab,
  ]);

  return (
    <main className="command-center">
      <WorkspaceAutosave enabled={workspaceQuery.isSuccess} />
      <aside className="sidebar">
        <header className="sidebar-header">
          <div>
            <div className="eyebrow">Fascinate</div>
            <h1>Command center</h1>
            <div className="muted">{email}</div>
          </div>
          <button
            onClick={async () => {
              await logout();
              window.location.reload();
            }}
          >
            Sign out
          </button>
        </header>

        <nav className="sidebar-tabs">
          <button data-active={tab === "machines"} onClick={() => setTab("machines")}>
            Machines
          </button>
          <button data-active={tab === "snapshots"} onClick={() => setTab("snapshots")}>
            Snapshots
          </button>
          <button data-active={tab === "env"} onClick={() => setTab("env")}>
            Env Vars
          </button>
        </nav>

        <div className="sidebar-content">{sidebar}</div>

        <section className="panel">
          <h2>Effective machine env</h2>
          {envPreview ? <pre className="env-preview">{JSON.stringify(envPreview, null, 2)}</pre> : <p className="muted">Select a machine and click Env to inspect its rendered values.</p>}
          <StatusError mutationError={envPreviewMutation.error} />
        </section>
      </aside>

      <WorkspaceCanvas />
    </main>
  );
}

function WorkspaceAutosave({ enabled }: { enabled: boolean }) {
  const hydrated = useWorkspaceStore((state) => state.hydrated);
  const windows = useWorkspaceStore((state) => state.windows);
  const viewport = useWorkspaceStore((state) => state.viewport);
  const serialize = useWorkspaceStore((state) => state.serialize);
  const saveTimer = useRef<number | null>(null);
  useEffect(() => {
    if (!enabled || !hydrated) {
      return;
    }
    if (saveTimer.current) {
      window.clearTimeout(saveTimer.current);
    }
    saveTimer.current = window.setTimeout(() => {
      void saveDefaultWorkspace(serialize());
    }, 400);
    return () => {
      if (saveTimer.current) {
        window.clearTimeout(saveTimer.current);
      }
    };
  }, [enabled, hydrated, serialize, viewport, windows]);

  return null;
}

function WorkspaceCanvas() {
  const windows = useWorkspaceStore((state) => state.windows);
  const viewport = useWorkspaceStore((state) => state.viewport);
  const setViewport = useWorkspaceStore((state) => state.setViewport);
  const closeWindow = useWorkspaceStore((state) => state.closeWindow);
  const focusWindow = useWorkspaceStore((state) => state.focusWindow);
  const moveWindow = useWorkspaceStore((state) => state.moveWindow);
  const setWindowSession = useWorkspaceStore((state) => state.setWindowSession);

  const workspaceViewportRef = useRef<HTMLDivElement | null>(null);
  const panStateRef = useRef<{
    pointerId: number;
    startClientX: number;
    startClientY: number;
    viewport: WorkspaceViewport;
  } | null>(null);
  const [workspaceBounds, setWorkspaceBounds] = useState<WorkspaceBounds>({ width: 0, height: 0 });
  const [isPanning, setIsPanning] = useState(false);

  useEffect(() => {
    const node = workspaceViewportRef.current;
    if (!node) {
      return;
    }

    const updateBounds = () => {
      const rect = node.getBoundingClientRect();
      setWorkspaceBounds({ width: rect.width, height: rect.height });
    };

    updateBounds();

    if (typeof ResizeObserver === "undefined") {
      return;
    }

    const observer = new ResizeObserver(() => updateBounds());
    observer.observe(node);
    return () => observer.disconnect();
  }, []);

  const applyViewport = (nextViewport: WorkspaceViewport) => {
    setViewport(clampViewport(nextViewport, workspaceBounds));
  };

  const getCanvasPointFromClient = (clientX: number, clientY: number) => {
    const node = workspaceViewportRef.current;
    if (!node) {
      return { x: 0, y: 0 };
    }
    return getCanvasPoint(clientX, clientY, node.getBoundingClientRect(), viewport);
  };

  const adjustZoom = (scaleFactor: number) => {
    const node = workspaceViewportRef.current;
    if (!node) {
      return;
    }
    const rect = node.getBoundingClientRect();
    const nextScale = clamp(viewport.scale * scaleFactor, minWorkspaceScale, maxWorkspaceScale);
    const origin = {
      x: rect.width / 2,
      y: rect.height / 2,
    };
    applyViewport(zoomViewportFromScreenPoint(viewport, nextScale, origin, workspaceBounds));
  };

  return (
    <section className="workspace">
      <div className="workspace-toolbar">
        <div className="workspace-toolbar-label">Canvas</div>
        <div className="workspace-toolbar-actions">
          <button onClick={() => adjustZoom(1 / 1.15)} aria-label="Zoom out">−</button>
          <button onClick={() => applyViewport(defaultWorkspaceViewport)}>Reset view</button>
          <button onClick={() => adjustZoom(1.15)} aria-label="Zoom in">+</button>
          <div className="workspace-zoom-readout">{Math.round(viewport.scale * 100)}%</div>
        </div>
      </div>
      <div
        ref={workspaceViewportRef}
        className="workspace-viewport"
        data-panning={isPanning ? "true" : "false"}
        onWheel={(event) => {
          const target = event.target as HTMLElement;
          if (target.closest(".window-frame")) {
            return;
          }
          event.preventDefault();
          const node = workspaceViewportRef.current;
          if (!node) {
            return;
          }
          const rect = node.getBoundingClientRect();
          if (event.ctrlKey || event.metaKey) {
            const nextScale = clamp(viewport.scale * Math.exp(-event.deltaY * 0.0015), minWorkspaceScale, maxWorkspaceScale);
            applyViewport(
              zoomViewportFromScreenPoint(
                viewport,
                nextScale,
                { x: event.clientX - rect.left, y: event.clientY - rect.top },
                workspaceBounds,
              ),
            );
            return;
          }
          applyViewport({
            ...viewport,
            x: viewport.x - event.deltaX,
            y: viewport.y - event.deltaY,
          });
        }}
        onPointerDown={(event) => {
          if (event.button !== 0) {
            return;
          }
          const target = event.target as HTMLElement;
          if (target.closest(".window-frame")) {
            return;
          }
          panStateRef.current = {
            pointerId: event.pointerId,
            startClientX: event.clientX,
            startClientY: event.clientY,
            viewport,
          };
          setIsPanning(true);
          event.currentTarget.setPointerCapture(event.pointerId);
        }}
        onPointerMove={(event) => {
          const panState = panStateRef.current;
          if (!panState || panState.pointerId !== event.pointerId) {
            return;
          }
          applyViewport({
            ...panState.viewport,
            x: panState.viewport.x + (event.clientX - panState.startClientX),
            y: panState.viewport.y + (event.clientY - panState.startClientY),
          });
        }}
        onPointerUp={(event) => {
          if (panStateRef.current?.pointerId === event.pointerId) {
            panStateRef.current = null;
            setIsPanning(false);
            event.currentTarget.releasePointerCapture(event.pointerId);
          }
        }}
        onPointerCancel={(event) => {
          if (panStateRef.current?.pointerId === event.pointerId) {
            panStateRef.current = null;
            setIsPanning(false);
            event.currentTarget.releasePointerCapture(event.pointerId);
          }
        }}
      >
        <div
          className="workspace-stage"
          style={{
            width: workspaceCanvasSize.width,
            height: workspaceCanvasSize.height,
            transform: `translate3d(${viewport.x}px, ${viewport.y}px, 0) scale(${viewport.scale})`,
          }}
        >
          <div className="workspace-grid" />
          {windows.map((window) => (
            <WindowFrame
              key={window.id}
              window={window}
              onClose={() => {
                closeWindow(window.id);
                if (window.sessionId) {
                  void deleteTerminalSession(window.sessionId);
                }
              }}
              onFocus={() => focusWindow(window.id)}
              onMove={(x, y) => moveWindow(window.id, x, y)}
              toCanvasPoint={getCanvasPointFromClient}
            >
              <Suspense fallback={<div className="terminal-loading">Opening terminal…</div>}>
                <TerminalView
                  machineName={window.machineName}
                  title={window.title}
                  sessionId={window.sessionId}
                  onSessionId={(sessionId) => setWindowSession(window.id, sessionId)}
                />
              </Suspense>
            </WindowFrame>
          ))}
        </div>
      </div>
    </section>
  );
}

function WindowFrame({
  window: layoutWindow,
  children,
  onClose,
  onFocus,
  onMove,
  toCanvasPoint,
}: {
  window: WorkspaceWindow;
  children: React.ReactNode;
  onClose: () => void;
  onFocus: () => void;
  onMove: (x: number, y: number) => void;
  toCanvasPoint: (clientX: number, clientY: number) => { x: number; y: number };
}) {
  const dragOffsetRef = useRef<{ x: number; y: number } | null>(null);

  useEffect(() => {
    const handleMove = (event: PointerEvent) => {
      if (dragOffsetRef.current) {
        const point = toCanvasPoint(event.clientX, event.clientY);
        onMove(point.x - dragOffsetRef.current.x, point.y - dragOffsetRef.current.y);
      }
    };
    const handleUp = () => {
      dragOffsetRef.current = null;
    };
    globalThis.window.addEventListener("pointermove", handleMove);
    globalThis.window.addEventListener("pointerup", handleUp);
    return () => {
      globalThis.window.removeEventListener("pointermove", handleMove);
      globalThis.window.removeEventListener("pointerup", handleUp);
    };
  }, [onMove, toCanvasPoint]);

  return (
    <div
      className="window-frame"
      style={{
        transform: `translate3d(${layoutWindow.x}px, ${layoutWindow.y}px, 0)`,
        width: layoutWindow.width,
        height: layoutWindow.height,
        zIndex: layoutWindow.z,
      }}
      onPointerDown={onFocus}
    >
      <header
        className="window-header"
        onPointerDown={(event) => {
          onFocus();
          const point = toCanvasPoint(event.clientX, event.clientY);
          dragOffsetRef.current = {
            x: point.x - layoutWindow.x,
            y: point.y - layoutWindow.y,
          };
        }}
      >
        <div>
          <strong>{layoutWindow.title}</strong>
          <div className="muted">{layoutWindow.machineName}</div>
        </div>
        <div className="actions">
          <button
            className="danger"
            onPointerDown={(event) => event.stopPropagation()}
            onClick={onClose}
          >
            Close
          </button>
        </div>
      </header>
      <div className="window-body">{children}</div>
    </div>
  );
}

function StatusError({ mutationError }: { mutationError: unknown }) {
  return mutationError ? <p className="error-text">{mutationError instanceof Error ? mutationError.message : "Request failed"}</p> : null;
}

function clampViewport(viewport: WorkspaceViewport, bounds: WorkspaceBounds): WorkspaceViewport {
  const scale = clamp(viewport.scale, minWorkspaceScale, maxWorkspaceScale);
  if (!bounds.width || !bounds.height) {
    return { ...viewport, scale };
  }

  const scaledWidth = workspaceCanvasSize.width * scale;
  const scaledHeight = workspaceCanvasSize.height * scale;
  const minX = Math.min(workspaceViewportPadding, bounds.width - scaledWidth - workspaceViewportPadding);
  const minY = Math.min(workspaceViewportPadding, bounds.height - scaledHeight - workspaceViewportPadding);

  return {
    x: clamp(viewport.x, minX, workspaceViewportPadding),
    y: clamp(viewport.y, minY, workspaceViewportPadding),
    scale,
  };
}

function zoomViewportFromScreenPoint(
  viewport: WorkspaceViewport,
  nextScale: number,
  point: { x: number; y: number },
  bounds: WorkspaceBounds,
) {
  const canvasX = (point.x - viewport.x) / viewport.scale;
  const canvasY = (point.y - viewport.y) / viewport.scale;
  return clampViewport(
    {
      x: point.x - canvasX * nextScale,
      y: point.y - canvasY * nextScale,
      scale: nextScale,
    },
    bounds,
  );
}

function getCanvasPoint(clientX: number, clientY: number, rect: DOMRect, viewport: WorkspaceViewport) {
  return {
    x: (clientX - rect.left - viewport.x) / viewport.scale,
    y: (clientY - rect.top - viewport.y) / viewport.scale,
  };
}

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value));
}
