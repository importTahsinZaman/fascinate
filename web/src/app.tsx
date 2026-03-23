import { Suspense, lazy, startTransition, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Camera, GitFork, Trash, X } from "@phosphor-icons/react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  forkMachine,
  createMachine,
  createSnapshot,
  deleteEnvVar,
  deleteMachine,
  deleteSnapshot,
  deleteTerminalSession,
  getDefaultWorkspace,
  getSession,
  listEnvVars,
  listMachines,
  listSnapshots,
  logout,
  requestLoginCode,
  saveDefaultWorkspace,
  setEnvVar,
  verifyLogin,
  type Machine,
  type Snapshot,
  type WorkspaceViewport,
  type WorkspaceWindow,
} from "./api";
import { useWorkspaceStore } from "./store";

const TerminalView = lazy(async () => import("./terminal").then((module) => ({ default: module.TerminalView })));

type WorkspaceBounds = {
  width: number;
  height: number;
};

type ModalState =
  | { type: "new-machine" }
  | { type: "fork-machine"; machine: Machine }
  | { type: "snapshot-machine"; machine: Machine }
  | { type: "restore-snapshot"; snapshot: Snapshot }
  | { type: "snapshots" }
  | { type: "env-vars" }
  | null;

const workspaceCanvasSize = { width: 9600, height: 6400 };
const workspaceViewportPadding = 160;
const defaultWorkspaceViewport: WorkspaceViewport = { x: 120, y: 96, scale: 1 };
const minWorkspaceScale = 0.45;
const maxWorkspaceScale = 2.2;
const workspaceZoomWheelFactor = 0.006;

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

  return <CommandCenter />;
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
              className="button-primary"
              onClick={() => requestCodeMutation.mutate(email)}
              disabled={!email || requestCodeMutation.isPending}
            >
              {requestCodeMutation.isPending ? "Sending…" : "Send code"}
            </button>
          ) : (
            <button
              className="button-primary"
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

function CommandCenter() {
  const queryClient = useQueryClient();
  const [modal, setModal] = useState<ModalState>(null);
  const [machineName, setMachineName] = useState("");
  const [forkTarget, setForkTarget] = useState("");
  const [snapshotName, setSnapshotName] = useState("");
  const [restoreTarget, setRestoreTarget] = useState("");
  const [envForm, setEnvForm] = useState({ key: "", value: "" });

  const hydrate = useWorkspaceStore((state) => state.hydrate);
  const openTerminal = useWorkspaceStore((state) => state.openTerminal);
  const windows = useWorkspaceStore((state) => state.windows);
  const windowCwds = useWorkspaceStore((state) => state.windowCwds);
  const closeWindow = useWorkspaceStore((state) => state.closeWindow);
  const focusWindow = useWorkspaceStore((state) => state.focusWindow);

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

  useEffect(() => {
    if (!modal) {
      return;
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setModal(null);
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [modal]);

  const createMachineMutation = useMutation({
    mutationFn: ({ name, snapshotName }: { name: string; snapshotName?: string }) =>
      createMachine(name, snapshotName),
    onSuccess: () => {
      setMachineName("");
      setRestoreTarget("");
      setModal(null);
      void queryClient.invalidateQueries({ queryKey: ["machines"] });
      void queryClient.invalidateQueries({ queryKey: ["snapshots"] });
    },
  });
  const deleteMachineMutation = useMutation({
    mutationFn: deleteMachine,
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["machines"] }),
  });
  const forkMachineMutation = useMutation({
    mutationFn: ({ source, target }: { source: string; target: string }) => forkMachine(source, target),
    onSuccess: () => {
      setForkTarget("");
      setModal(null);
      void queryClient.invalidateQueries({ queryKey: ["machines"] });
    },
  });
  const createSnapshotMutation = useMutation({
    mutationFn: ({ machine, name }: { machine: string; name: string }) => createSnapshot(machine, name),
    onSuccess: () => {
      setSnapshotName("");
      setModal(null);
      void queryClient.invalidateQueries({ queryKey: ["snapshots"] });
    },
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
  const logoutMutation = useMutation({
    mutationFn: logout,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["session"] });
      window.location.reload();
    },
  });

  const machineList = machinesQuery.data ?? [];
  const snapshotList = snapshotsQuery.data ?? [];
  const envList = envVarsQuery.data ?? [];
  const windowsByMachine = useMemo(() => {
    const grouped = new Map<string, WorkspaceWindow[]>();
    for (const window of windows) {
      const items = grouped.get(window.machineName) ?? [];
      items.push(window);
      grouped.set(window.machineName, items);
    }
    for (const items of grouped.values()) {
      items.sort((left, right) => left.z - right.z);
    }
    return grouped;
  }, [windows]);

  const openMachineShell = (machineName: string) => {
    const shellCount = windowsByMachine.get(machineName)?.length ?? 0;
    const nextShellNumber = shellCount + 1;
    openTerminal(machineName, nextShellNumber === 1 ? `${machineName} shell` : `${machineName} shell ${nextShellNumber}`);
  };

  const closeShellWindow = (window: WorkspaceWindow) => {
    closeWindow(window.id);
    if (window.sessionId) {
      void deleteTerminalSession(window.sessionId);
    }
  };

  const renderModal = () => {
    if (!modal) {
      return null;
    }

    if (modal.type === "new-machine") {
      return (
        <AppModal
          title="Create machine"
          description="Provision a fresh VM from the base image."
          onClose={() => setModal(null)}
        >
          <form
            className="app-modal-body"
            onSubmit={(event) => {
              event.preventDefault();
              createMachineMutation.mutate({ name: machineName });
            }}
          >
            <label className="field">
              <span>Name</span>
              <input value={machineName} onChange={(event) => setMachineName(event.target.value)} placeholder="m-1" />
            </label>
            <StatusError mutationError={createMachineMutation.error} />
            <div className="app-modal-actions">
              <button type="button" onClick={() => setModal(null)}>
                Cancel
              </button>
              <button className="button-primary" type="submit" disabled={!machineName || createMachineMutation.isPending}>
                {createMachineMutation.isPending ? "Creating…" : "Create machine"}
              </button>
            </div>
          </form>
        </AppModal>
      );
    }

    if (modal.type === "fork-machine") {
      return (
        <AppModal
          title="Fork machine"
          description={`Create a copy of ${modal.machine.name}.`}
          onClose={() => setModal(null)}
        >
          <form
            className="app-modal-body"
            onSubmit={(event) => {
              event.preventDefault();
              forkMachineMutation.mutate({ source: modal.machine.name, target: forkTarget });
            }}
          >
            <label className="field">
              <span>Source</span>
              <input value={modal.machine.name} disabled />
            </label>
            <label className="field">
              <span>New machine name</span>
              <input value={forkTarget} onChange={(event) => setForkTarget(event.target.value)} placeholder={`${modal.machine.name}-copy`} />
            </label>
            <StatusError mutationError={forkMachineMutation.error} />
            <div className="app-modal-actions">
              <button type="button" onClick={() => setModal(null)}>
                Cancel
              </button>
              <button className="button-primary" type="submit" disabled={!forkTarget || forkMachineMutation.isPending}>
                {forkMachineMutation.isPending ? "Forking…" : "Fork machine"}
              </button>
            </div>
          </form>
        </AppModal>
      );
    }

    if (modal.type === "snapshot-machine") {
      return (
        <AppModal
          title="Create snapshot"
          description={`Save a restorable snapshot of ${modal.machine.name}.`}
          onClose={() => setModal(null)}
        >
          <form
            className="app-modal-body"
            onSubmit={(event) => {
              event.preventDefault();
              createSnapshotMutation.mutate({ machine: modal.machine.name, name: snapshotName });
            }}
          >
            <label className="field">
              <span>Machine</span>
              <input value={modal.machine.name} disabled />
            </label>
            <label className="field">
              <span>Snapshot name</span>
              <input value={snapshotName} onChange={(event) => setSnapshotName(event.target.value)} placeholder={`${modal.machine.name}-snapshot`} />
            </label>
            <StatusError mutationError={createSnapshotMutation.error} />
            <div className="app-modal-actions">
              <button type="button" onClick={() => setModal(null)}>
                Cancel
              </button>
              <button className="button-primary" type="submit" disabled={!snapshotName || createSnapshotMutation.isPending}>
                {createSnapshotMutation.isPending ? "Saving…" : "Create snapshot"}
              </button>
            </div>
          </form>
        </AppModal>
      );
    }

    if (modal.type === "restore-snapshot") {
      return (
        <AppModal
          title="Restore snapshot"
          description={`Create a new machine from ${modal.snapshot.name}.`}
          onClose={() => setModal(null)}
        >
          <form
            className="app-modal-body"
            onSubmit={(event) => {
              event.preventDefault();
              createMachineMutation.mutate({ name: restoreTarget, snapshotName: modal.snapshot.name });
            }}
          >
            <label className="field">
              <span>Snapshot</span>
              <input value={modal.snapshot.name} disabled />
            </label>
            <label className="field">
              <span>New machine name</span>
              <input value={restoreTarget} onChange={(event) => setRestoreTarget(event.target.value)} placeholder={`${modal.snapshot.name}-vm`} />
            </label>
            <StatusError mutationError={createMachineMutation.error} />
            <div className="app-modal-actions">
              <button type="button" onClick={() => setModal(null)}>
                Cancel
              </button>
              <button className="button-primary" type="submit" disabled={!restoreTarget || createMachineMutation.isPending}>
                {createMachineMutation.isPending ? "Restoring…" : "Restore machine"}
              </button>
            </div>
          </form>
        </AppModal>
      );
    }

    if (modal.type === "snapshots") {
      return (
        <AppModal title="Snapshots" description="Restore or delete saved machine snapshots." onClose={() => setModal(null)} wide>
          <div className="app-modal-body">
            {snapshotList.length === 0 ? (
              <p className="muted">No snapshots yet.</p>
            ) : (
              <div className="sidebar-record-list">
                {snapshotList.map((snapshot) => (
                  <article key={snapshot.id} className="sidebar-record">
                    <div>
                      <div className="sidebar-record-heading">
                        <strong>{snapshot.name}</strong>
                        <StateBadge state={snapshot.state} />
                      </div>
                      <div className="muted">{snapshot.source_machine_name ?? "snapshot"}</div>
                    </div>
                    <div className="actions">
                      <button
                        type="button"
                        onClick={() => {
                          setRestoreTarget(`${snapshot.name}-vm`);
                          setModal({ type: "restore-snapshot", snapshot });
                        }}
                      >
                        Restore
                      </button>
                      <button
                        className="icon-action-button danger"
                        type="button"
                        aria-label={`Delete snapshot ${snapshot.name}`}
                        title={`Delete snapshot ${snapshot.name}`}
                        onClick={() => deleteSnapshotMutation.mutate(snapshot.name)}
                      >
                        <X className="icon-svg" weight="regular" />
                      </button>
                    </div>
                  </article>
                ))}
              </div>
            )}
            <StatusError mutationError={deleteSnapshotMutation.error} />
          </div>
        </AppModal>
      );
    }

    return (
      <AppModal
        title="Environment variables"
        description="Manage the user-defined variables Fascinate injects into every VM."
        onClose={() => setModal(null)}
        wide
      >
        <div className="app-modal-body">
          <section className="app-modal-section">
            <label className="field">
              <span>Key</span>
              <input
                value={envForm.key}
                onChange={(event) => setEnvForm((current) => ({ ...current, key: event.target.value }))}
                placeholder="FRONTEND_URL"
              />
            </label>
            <label className="field">
              <span>Value</span>
              <input
                value={envForm.value}
                onChange={(event) => setEnvForm((current) => ({ ...current, value: event.target.value }))}
                placeholder="${FASCINATE_PUBLIC_URL}"
              />
            </label>
            <div className="app-modal-actions">
              <button
                type="button"
                onClick={() => setEnvForm({ key: "", value: "" })}
                disabled={!envForm.key && !envForm.value}
              >
                Clear
              </button>
              <button
                className="button-primary"
                type="button"
                onClick={() => setEnvMutation.mutate(envForm)}
                disabled={!envForm.key || !envForm.value || setEnvMutation.isPending}
              >
                {setEnvMutation.isPending ? "Saving…" : "Save env var"}
              </button>
            </div>
            <StatusError mutationError={setEnvMutation.error} />
          </section>

          <section className="app-modal-section">
            {envList.length === 0 ? (
              <p className="muted">No env vars set yet.</p>
            ) : (
              <div className="sidebar-record-list">
                {envList.map((entry) => (
                  <article key={entry.key} className="sidebar-record">
                    <div>
                      <div className="sidebar-record-heading">
                        <strong>{entry.key}</strong>
                        <span className="inline-chip">env</span>
                      </div>
                      <div className="muted">{entry.value}</div>
                    </div>
                    <div className="actions">
                      <button type="button" onClick={() => setEnvForm({ key: entry.key, value: entry.value })}>
                        Edit
                      </button>
                      <button className="danger" type="button" onClick={() => deleteEnvMutation.mutate(entry.key)}>
                        Delete
                      </button>
                    </div>
                  </article>
                ))}
              </div>
            )}
            <StatusError mutationError={deleteEnvMutation.error} />
          </section>
        </div>
      </AppModal>
    );
  };

  return (
    <main className="command-center">
      <WorkspaceAutosave enabled={workspaceQuery.isSuccess} />
      <div className="command-center-workspace">
        <WorkspaceCanvas />
      </div>
      <aside className="control-sidebar" aria-label="Workspace controls">
        <div className="control-sidebar-scroll">
          <section className="sidebar-section">
            <div className="sidebar-section-header">
              <h2>Machines</h2>
              <button
                className="button-primary"
                type="button"
                onClick={() => {
                  setMachineName("");
                  setModal({ type: "new-machine" });
                }}
              >
                New machine
              </button>
            </div>
            {machineList.length === 0 ? (
              <p className="muted">No machines yet. Create one to start running shells and agents.</p>
            ) : (
              <div className="sidebar-machine-list">
                {machineList.map((machine) => {
                  const machineWindows = windowsByMachine.get(machine.name) ?? [];
                  return (
                    <article key={machine.id} className="machine-card">
                      <div className="machine-card-header">
                        <div>
                          <strong>{machine.name}</strong>
                        </div>
                        <div className="actions">
                          <button
                            className="icon-action-button"
                            type="button"
                            aria-label={`Fork ${machine.name}`}
                            title={`Fork ${machine.name}`}
                            onClick={() => {
                              setForkTarget(`${machine.name}-copy`);
                              setModal({ type: "fork-machine", machine });
                            }}
                          >
                            <GitFork className="icon-svg" weight="regular" />
                          </button>
                          <button
                            className="icon-action-button"
                            type="button"
                            aria-label={`Snapshot ${machine.name}`}
                            title={`Snapshot ${machine.name}`}
                            onClick={() => {
                              setSnapshotName(`${machine.name}-snapshot`);
                              setModal({ type: "snapshot-machine", machine });
                            }}
                          >
                            <Camera className="icon-svg" weight="regular" />
                          </button>
                          <button
                            className="icon-action-button danger"
                            type="button"
                            aria-label={`Delete ${machine.name}`}
                            title={`Delete ${machine.name}`}
                            onClick={() => deleteMachineMutation.mutate(machine.name)}
                          >
                            <Trash className="icon-svg" weight="regular" />
                          </button>
                        </div>
                      </div>
                      <div className="machine-card-shells">
                        {machineWindows.length === 0 ? (
                          <p className="muted">No open shells.</p>
                        ) : (
                          <div className="machine-shell-list">
                            {machineWindows.map((window, index) => {
                              const shellLabel = formatShellListLabel(windowCwds[window.id], index + 1);
                              return (
                                <div key={window.id} className="machine-shell-row">
                                  <button
                                    type="button"
                                    className="machine-shell-focus"
                                    onClick={() => focusWindow(window.id)}
                                    title={windowCwds[window.id] ?? shellLabel}
                                  >
                                    <span className="machine-shell-label">{shellLabel}</span>
                                  </button>
                                  <button
                                    type="button"
                                    className="machine-shell-delete"
                                    aria-label={`Delete ${shellLabel}`}
                                    title={`Delete ${shellLabel}`}
                                    onClick={() => closeShellWindow(window)}
                                  >
                                    <X className="icon-svg" weight="regular" />
                                  </button>
                                </div>
                              );
                            })}
                          </div>
                        )}
                        <div className="machine-card-shells-actions">
                          <button type="button" onClick={() => openMachineShell(machine.name)}>
                            New shell
                          </button>
                        </div>
                      </div>
                    </article>
                  );
                })}
              </div>
            )}
            <StatusError mutationError={deleteMachineMutation.error} />
          </section>
        </div>

        <footer className="control-sidebar-footer">
          <div className="control-sidebar-manage">
            <span className="control-sidebar-manage-label">Manage</span>
            <div className="control-sidebar-manage-actions">
              <button type="button" onClick={() => setModal({ type: "env-vars" })}>
                Env vars
              </button>
              <button type="button" onClick={() => setModal({ type: "snapshots" })}>
                Snapshots
              </button>
            </div>
          </div>
          <div className="sidebar-signout-row">
            <button
              className="sidebar-signout-button"
              type="button"
              onClick={() => logoutMutation.mutate()}
              disabled={logoutMutation.isPending}
            >
              {logoutMutation.isPending ? "Signing out…" : "Sign out"}
            </button>
          </div>
        </footer>
      </aside>
      {renderModal()}
    </main>
  );
}

function AppModal({
  title,
  description,
  onClose,
  children,
  wide,
}: {
  title: string;
  description?: string;
  onClose: () => void;
  children: ReactNode;
  wide?: boolean;
}) {
  return (
    <div className="app-modal-backdrop" onPointerDown={onClose}>
      <section
        className={`app-modal${wide ? " app-modal-wide" : ""}`}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        onPointerDown={(event) => event.stopPropagation()}
      >
        <header className="app-modal-header">
          <div>
            <h2>{title}</h2>
            {description ? <p>{description}</p> : null}
          </div>
          <button
            className="icon-action-button"
            type="button"
            aria-label="Close modal"
            title="Close modal"
            onClick={onClose}
          >
            <X className="icon-svg" weight="regular" />
          </button>
        </header>
        {children}
      </section>
    </div>
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
  const setWindowCwd = useWorkspaceStore((state) => state.setWindowCwd);

  const workspaceViewportRef = useRef<HTMLDivElement | null>(null);
  const viewportRef = useRef(viewport);
  const boundsRef = useRef<WorkspaceBounds>({ width: 0, height: 0 });
  const panStateRef = useRef<{
    pointerId: number;
    startClientX: number;
    startClientY: number;
    viewport: WorkspaceViewport;
  } | null>(null);
  const gestureZoomRef = useRef<{
    viewport: WorkspaceViewport;
    scale: number;
  } | null>(null);
  const [workspaceBounds, setWorkspaceBounds] = useState<WorkspaceBounds>({ width: 0, height: 0 });
  const [isPanning, setIsPanning] = useState(false);

  useEffect(() => {
    viewportRef.current = viewport;
  }, [viewport]);

  useEffect(() => {
    boundsRef.current = workspaceBounds;
  }, [workspaceBounds]);

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
    setViewport(clampViewport(nextViewport, boundsRef.current));
  };

  useEffect(() => {
    const node = workspaceViewportRef.current;
    if (!node) {
      return;
    }

    const isWindowTarget = (target: EventTarget | null) => {
      return target instanceof HTMLElement ? Boolean(target.closest(".window-frame")) : false;
    };

    const isWindowHeaderTarget = (target: EventTarget | null) => {
      return target instanceof HTMLElement ? Boolean(target.closest(".window-header")) : false;
    };

    const applyZoomFromClientPoint = (
      baseViewport: WorkspaceViewport,
      nextScale: number,
      clientX: number,
      clientY: number,
    ) => {
      const currentBounds = boundsRef.current;
      const rect = node.getBoundingClientRect();
      setViewport(
        zoomViewportFromScreenPoint(
          baseViewport,
          nextScale,
          { x: clientX - rect.left, y: clientY - rect.top },
          currentBounds,
        ),
      );
    };

    const panViewportFromWheel = (event: WheelEvent, currentViewport: WorkspaceViewport) => {
      event.preventDefault();
      event.stopPropagation();
      setViewport(
        clampViewport(
          {
            ...currentViewport,
            x: currentViewport.x - event.deltaX,
            y: currentViewport.y - event.deltaY,
          },
          boundsRef.current,
        ),
      );
    };

    const handleWheel = (event: WheelEvent) => {
      const currentViewport = viewportRef.current;

      if (event.ctrlKey || event.metaKey) {
        event.preventDefault();
        event.stopPropagation();
        const nextScale = clamp(
          currentViewport.scale * Math.exp(-event.deltaY * workspaceZoomWheelFactor),
          minWorkspaceScale,
          maxWorkspaceScale,
        );
        applyZoomFromClientPoint(currentViewport, nextScale, event.clientX, event.clientY);
        return;
      }

      if (isWindowHeaderTarget(event.target)) {
        panViewportFromWheel(event, currentViewport);
        return;
      }

      if (isWindowTarget(event.target)) {
        return;
      }

      panViewportFromWheel(event, currentViewport);
    };

    const handleGestureStart = (event: Event) => {
      gestureZoomRef.current = {
        viewport: viewportRef.current,
        scale: viewportRef.current.scale,
      };
      event.preventDefault();
      event.stopPropagation();
    };

    const handleGestureChange = (event: Event) => {
      const gesture = event as Event & { scale?: number; clientX?: number; clientY?: number };
      const state = gestureZoomRef.current;
      if (!state) {
        return;
      }
      const rect = node.getBoundingClientRect();
      const clientX = typeof gesture.clientX === "number" ? gesture.clientX : rect.left + rect.width / 2;
      const clientY = typeof gesture.clientY === "number" ? gesture.clientY : rect.top + rect.height / 2;
      const nextScale = clamp(
        state.scale * (typeof gesture.scale === "number" ? gesture.scale : 1),
        minWorkspaceScale,
        maxWorkspaceScale,
      );
      applyZoomFromClientPoint(state.viewport, nextScale, clientX, clientY);
      event.preventDefault();
      event.stopPropagation();
    };

    const handleGestureEnd = (event: Event) => {
      gestureZoomRef.current = null;
      event.preventDefault();
      event.stopPropagation();
    };

    node.addEventListener("wheel", handleWheel, { passive: false, capture: true });
    node.addEventListener("gesturestart", handleGestureStart as EventListener, { passive: false, capture: true });
    node.addEventListener("gesturechange", handleGestureChange as EventListener, { passive: false, capture: true });
    node.addEventListener("gestureend", handleGestureEnd as EventListener, { passive: false, capture: true });

    return () => {
      node.removeEventListener("wheel", handleWheel, true);
      node.removeEventListener("gesturestart", handleGestureStart as EventListener, true);
      node.removeEventListener("gesturechange", handleGestureChange as EventListener, true);
      node.removeEventListener("gestureend", handleGestureEnd as EventListener, true);
    };
  }, [setViewport]);

  const getCanvasPointFromClient = (clientX: number, clientY: number) => {
    const node = workspaceViewportRef.current;
    if (!node) {
      return { x: 0, y: 0 };
    }
    return getCanvasPoint(clientX, clientY, node.getBoundingClientRect(), viewport);
  };

  return (
    <section className="workspace">
      <div
        ref={workspaceViewportRef}
        className="workspace-viewport"
        data-panning={isPanning ? "true" : "false"}
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
                  onCwdChange={(cwd) => setWindowCwd(window.id, cwd)}
                />
              </Suspense>
            </WindowFrame>
          ))}
        </div>
      </div>
    </section>
  );
}

function formatShellListLabel(cwd: string | undefined, index: number) {
  if (!cwd) {
    return `Shell ${index}`;
  }
  return cwd.replace(/^\/home\/ubuntu(?=\/|$)/, "~");
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
  children: ReactNode;
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
        <strong>{layoutWindow.title}</strong>
        <div className="actions">
          <button
            className="icon-action-button danger"
            type="button"
            aria-label="Close shell"
            title="Close shell"
            onPointerDown={(event) => event.stopPropagation()}
            onClick={onClose}
          >
            <X className="icon-svg" weight="regular" />
          </button>
        </div>
      </header>
      <div className="window-body">{children}</div>
    </div>
  );
}

function StateBadge({ state }: { state: string }) {
  const normalized = state.toLowerCase();
  const tone =
    normalized === "running" || normalized === "ready"
      ? "success"
      : normalized === "failed" || normalized === "error"
        ? "danger"
        : normalized === "creating" || normalized === "starting" || normalized === "saving"
          ? "warning"
          : "neutral";

  return <span className={`state-badge state-badge-${tone}`}>{state.toLowerCase()}</span>;
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
