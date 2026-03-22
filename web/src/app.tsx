import { Suspense, forwardRef, lazy, startTransition, useEffect, useLayoutEffect, useRef, useState, type ButtonHTMLAttributes, type ReactNode } from "react";
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
  type Machine,
  type WorkspaceViewport,
  type WorkspaceWindow,
} from "./api";
import { useWorkspaceStore } from "./store";

const TerminalView = lazy(async () => import("./terminal").then((module) => ({ default: module.TerminalView })));

type ToolbeltPopover = "shell" | "machine" | "snapshot" | "env";

type WorkspaceBounds = {
  width: number;
  height: number;
};

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
  const [activePopover, setActivePopover] = useState<ToolbeltPopover | null>(null);
  const [machineName, setMachineName] = useState("");
  const [snapshotSource, setSnapshotSource] = useState("");
  const [cloneSource, setCloneSource] = useState("");
  const [cloneTarget, setCloneTarget] = useState("");
  const [snapshotMachine, setSnapshotMachine] = useState("");
  const [snapshotName, setSnapshotName] = useState("");
  const [restoreSnapshot, setRestoreSnapshot] = useState("");
  const [restoreTarget, setRestoreTarget] = useState("");
  const [envForm, setEnvForm] = useState({ key: "", value: "" });
  const [envPreview, setEnvPreview] = useState<Record<string, string> | null>(null);
  const [envPreviewMachine, setEnvPreviewMachine] = useState("");
  const [popoverAnchor, setPopoverAnchor] = useState<number | null>(null);

  const hydrate = useWorkspaceStore((state) => state.hydrate);
  const openTerminal = useWorkspaceStore((state) => state.openTerminal);

  const toolbeltShellRef = useRef<HTMLDivElement | null>(null);
  const toolbeltButtonRefs = useRef<Partial<Record<ToolbeltPopover, HTMLButtonElement | null>>>({});

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

  useLayoutEffect(() => {
    if (!activePopover) {
      setPopoverAnchor(null);
      return;
    }
    const shell = toolbeltShellRef.current;
    const button = toolbeltButtonRefs.current[activePopover];
    if (!shell || !button) {
      return;
    }

    const updateAnchor = () => {
      const shellRect = shell.getBoundingClientRect();
      const buttonRect = button.getBoundingClientRect();
      setPopoverAnchor(buttonRect.left - shellRect.left + buttonRect.width / 2);
    };

    updateAnchor();
    window.addEventListener("resize", updateAnchor);
    return () => window.removeEventListener("resize", updateAnchor);
  }, [activePopover]);

  useEffect(() => {
    if (!activePopover) {
      return;
    }

    const handlePointerDown = (event: PointerEvent) => {
      if (toolbeltShellRef.current?.contains(event.target as Node)) {
        return;
      }
      setActivePopover(null);
    };

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setActivePopover(null);
      }
    };

    document.addEventListener("pointerdown", handlePointerDown);
    window.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown);
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [activePopover]);

  const createMachineMutation = useMutation({
    mutationFn: ({ name, snapshotName }: { name: string; snapshotName?: string }) =>
      createMachine(name, snapshotName),
    onSuccess: () => {
      setMachineName("");
      setSnapshotSource("");
      setRestoreTarget("");
      setRestoreSnapshot("");
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
    onSuccess: () => {
      setCloneSource("");
      setCloneTarget("");
      void queryClient.invalidateQueries({ queryKey: ["machines"] });
    },
  });
  const createSnapshotMutation = useMutation({
    mutationFn: ({ machine, name }: { machine: string; name: string }) => createSnapshot(machine, name),
    onSuccess: () => {
      setSnapshotMachine("");
      setSnapshotName("");
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
  const envPreviewMutation = useMutation({
    mutationFn: getMachineEnv,
    onSuccess: (value) => setEnvPreview(value.entries),
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

  const togglePopover = (popover: ToolbeltPopover) => {
    setActivePopover((current) => (current === popover ? null : popover));
  };

  const registerToolbeltButton = (popover: ToolbeltPopover) => (node: HTMLButtonElement | null) => {
    toolbeltButtonRefs.current[popover] = node;
  };

  const renderPopover = () => {
    if (!activePopover) {
      return null;
    }

    switch (activePopover) {
      case "shell":
        return (
          <div className="toolbelt-popover-inner">
            <section className="toolbelt-section">
              <div>
                <div className="eyebrow">Shells</div>
                <h2>Open a shell</h2>
              </div>
              {machineList.length === 0 ? (
                <p className="muted">Create a machine first, then open shells from here.</p>
              ) : (
                <div className="toolbelt-list">
                  {machineList.map((machine) => (
                    <article key={machine.id} className="toolbelt-item">
                      <div>
                        <strong>{machine.name}</strong>
                        <div className="muted">{machine.state}</div>
                      </div>
                      <div className="actions">
                        <button
                          onClick={() => {
                            openTerminal(machine.name, `${machine.name} shell`);
                            setActivePopover(null);
                          }}
                        >
                          Open shell
                        </button>
                        {machine.url ? (
                          <a className="toolbelt-link" href={machine.url} target="_blank" rel="noreferrer">
                            Open app
                          </a>
                        ) : null}
                      </div>
                    </article>
                  ))}
                </div>
              )}
            </section>
          </div>
        );
      case "machine":
        return (
          <div className="toolbelt-popover-inner">
            <section className="toolbelt-section">
              <div>
                <div className="eyebrow">Machines</div>
                <h2>Create machine</h2>
              </div>
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

            <section className="toolbelt-section">
              <h2>Clone machine</h2>
              <label className="field">
                <span>Source</span>
                <select value={cloneSource} onChange={(event) => setCloneSource(event.target.value)}>
                  <option value="">Select machine</option>
                  {machineList.map((machine) => (
                    <option key={machine.id} value={machine.name}>
                      {machine.name}
                    </option>
                  ))}
                </select>
              </label>
              <label className="field">
                <span>Target</span>
                <input value={cloneTarget} onChange={(event) => setCloneTarget(event.target.value)} placeholder="m-1-v2" />
              </label>
              <button
                onClick={() => cloneMachineMutation.mutate({ source: cloneSource, target: cloneTarget })}
                disabled={!cloneSource || !cloneTarget || cloneMachineMutation.isPending}
              >
                {cloneMachineMutation.isPending ? "Cloning…" : "Clone machine"}
              </button>
              <StatusError mutationError={cloneMachineMutation.error} />
            </section>

            <section className="toolbelt-section">
              <h2>Active machines</h2>
              {machineList.length === 0 ? (
                <p className="muted">No machines yet.</p>
              ) : (
                <div className="toolbelt-list">
                  {machineList.map((machine) => (
                    <article key={machine.id} className="toolbelt-item">
                      <div>
                        <strong>{machine.name}</strong>
                        <div className="muted">{machine.state}</div>
                      </div>
                      <div className="actions">
                        <button onClick={() => openTerminal(machine.name, `${machine.name} shell`)}>Open shell</button>
                        <button className="danger" onClick={() => deleteMachineMutation.mutate(machine.name)}>
                          Delete
                        </button>
                      </div>
                    </article>
                  ))}
                </div>
              )}
              <StatusError mutationError={deleteMachineMutation.error} />
            </section>
          </div>
        );
      case "snapshot":
        return (
          <div className="toolbelt-popover-inner">
            <section className="toolbelt-section">
              <div>
                <div className="eyebrow">Snapshots</div>
                <h2>Create snapshot</h2>
              </div>
              <label className="field">
                <span>Machine</span>
                <select value={snapshotMachine} onChange={(event) => setSnapshotMachine(event.target.value)}>
                  <option value="">Select machine</option>
                  {machineList.map((machine) => (
                    <option key={machine.id} value={machine.name}>
                      {machine.name}
                    </option>
                  ))}
                </select>
              </label>
              <label className="field">
                <span>Snapshot name</span>
                <input value={snapshotName} onChange={(event) => setSnapshotName(event.target.value)} placeholder="m-1-snapshot" />
              </label>
              <button
                onClick={() => createSnapshotMutation.mutate({ machine: snapshotMachine, name: snapshotName })}
                disabled={!snapshotMachine || !snapshotName || createSnapshotMutation.isPending}
              >
                {createSnapshotMutation.isPending ? "Saving…" : "Create snapshot"}
              </button>
              <StatusError mutationError={createSnapshotMutation.error} />
            </section>

            <section className="toolbelt-section">
              <h2>Restore snapshot</h2>
              <label className="field">
                <span>Snapshot</span>
                <select value={restoreSnapshot} onChange={(event) => setRestoreSnapshot(event.target.value)}>
                  <option value="">Select snapshot</option>
                  {snapshotList.map((snapshot) => (
                    <option key={snapshot.id} value={snapshot.name}>
                      {snapshot.name}
                    </option>
                  ))}
                </select>
              </label>
              <label className="field">
                <span>New machine name</span>
                <input value={restoreTarget} onChange={(event) => setRestoreTarget(event.target.value)} placeholder="snapshot-vm" />
              </label>
              <button
                onClick={() => createMachineMutation.mutate({ name: restoreTarget, snapshotName: restoreSnapshot })}
                disabled={!restoreSnapshot || !restoreTarget || createMachineMutation.isPending}
              >
                {createMachineMutation.isPending ? "Restoring…" : "Restore machine"}
              </button>
              <StatusError mutationError={createMachineMutation.error} />
            </section>

            <section className="toolbelt-section">
              <h2>Saved snapshots</h2>
              {snapshotList.length === 0 ? (
                <p className="muted">No snapshots yet.</p>
              ) : (
                <div className="toolbelt-list">
                  {snapshotList.map((snapshot) => (
                    <article key={snapshot.id} className="toolbelt-item">
                      <div>
                        <strong>{snapshot.name}</strong>
                        <div className="muted">{snapshot.source_machine_name ?? "snapshot"} · {snapshot.state}</div>
                      </div>
                      <div className="actions">
                        <button className="danger" onClick={() => deleteSnapshotMutation.mutate(snapshot.name)}>
                          Delete
                        </button>
                      </div>
                    </article>
                  ))}
                </div>
              )}
              <StatusError mutationError={deleteSnapshotMutation.error} />
            </section>
          </div>
        );
      case "env":
        return (
          <div className="toolbelt-popover-inner">
            <section className="toolbelt-section">
              <div>
                <div className="eyebrow">Environment</div>
                <h2>Manage env vars</h2>
              </div>
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
              <button
                onClick={() => setEnvMutation.mutate(envForm)}
                disabled={!envForm.key || !envForm.value || setEnvMutation.isPending}
              >
                {setEnvMutation.isPending ? "Saving…" : "Save env var"}
              </button>
              <StatusError mutationError={setEnvMutation.error} />
            </section>

            <section className="toolbelt-section">
              <h2>Saved env vars</h2>
              {envList.length === 0 ? (
                <p className="muted">No env vars set yet.</p>
              ) : (
                <div className="toolbelt-list">
                  {envList.map((entry) => (
                    <article key={entry.key} className="toolbelt-item">
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
              )}
              <StatusError mutationError={deleteEnvMutation.error} />
            </section>

            <section className="toolbelt-section">
              <h2>Preview machine env</h2>
              <label className="field">
                <span>Machine</span>
                <select value={envPreviewMachine} onChange={(event) => setEnvPreviewMachine(event.target.value)}>
                  <option value="">Select machine</option>
                  {machineList.map((machine) => (
                    <option key={machine.id} value={machine.name}>
                      {machine.name}
                    </option>
                  ))}
                </select>
              </label>
              <button
                onClick={() => envPreviewMutation.mutate(envPreviewMachine)}
                disabled={!envPreviewMachine || envPreviewMutation.isPending}
              >
                {envPreviewMutation.isPending ? "Loading…" : "Preview env"}
              </button>
              {envPreview ? <pre className="env-preview">{JSON.stringify(envPreview, null, 2)}</pre> : null}
              {!envPreview ? <p className="muted">Preview the rendered values for a machine here.</p> : null}
              <StatusError mutationError={envPreviewMutation.error} />
            </section>
          </div>
        );
    }
  };

  return (
    <main className="command-center">
      <WorkspaceAutosave enabled={workspaceQuery.isSuccess} />
      <WorkspaceCanvas />
      <div ref={toolbeltShellRef} className="toolbelt-shell">
        <div className="toolbelt">
          <ToolbeltButton
            ref={registerToolbeltButton("shell")}
            active={activePopover === "shell"}
            onClick={() => togglePopover("shell")}
            aria-label="Shells"
            title="Shells"
          >
            <ShellIcon />
          </ToolbeltButton>
          <ToolbeltButton
            ref={registerToolbeltButton("machine")}
            active={activePopover === "machine"}
            onClick={() => togglePopover("machine")}
            aria-label="Machines"
            title="Machines"
          >
            <MachineIcon />
          </ToolbeltButton>
          <ToolbeltButton
            ref={registerToolbeltButton("snapshot")}
            active={activePopover === "snapshot"}
            onClick={() => togglePopover("snapshot")}
            aria-label="Snapshots"
            title="Snapshots"
          >
            <SnapshotIcon />
          </ToolbeltButton>
          <ToolbeltButton
            ref={registerToolbeltButton("env")}
            active={activePopover === "env"}
            onClick={() => togglePopover("env")}
            aria-label="Env Vars"
            title="Env Vars"
          >
            <EnvIcon />
          </ToolbeltButton>
          <div className="toolbelt-divider" />
          <ToolbeltButton
            danger
            onClick={() => logoutMutation.mutate()}
            disabled={logoutMutation.isPending}
            aria-label={logoutMutation.isPending ? "Logging out" : "Log out"}
            title={email}
          >
            <LogoutIcon />
          </ToolbeltButton>
        </div>
        {activePopover ? (
          <div
            className="toolbelt-popover"
            style={{ left: popoverAnchor ?? 0, visibility: popoverAnchor === null ? "hidden" : "visible" }}
          >
            {renderPopover()}
          </div>
        ) : null}
      </div>
    </main>
  );
}

const ToolbeltButton = forwardRef<HTMLButtonElement, {
  active?: boolean;
  children: ReactNode;
  danger?: boolean;
} & ButtonHTMLAttributes<HTMLButtonElement>>(function ToolbeltButton({ active, children, danger, ...props }, ref) {
  return (
    <button
      ref={ref}
      className={`toolbelt-button${danger ? " danger" : ""}`}
      data-active={active ? "true" : "false"}
      type="button"
      {...props}
    >
      {children}
    </button>
  );
});

function ShellIcon() {
  return (
    <svg className="icon-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true">
      <path d="M4 6.75h16v10.5H4z" />
      <path d="m7.5 10 2.5 2-2.5 2" />
      <path d="M12.75 14h3.75" />
    </svg>
  );
}

function MachineIcon() {
  return (
    <svg className="icon-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true">
      <rect x="4" y="5.5" width="16" height="6.5" rx="1.5" />
      <rect x="4" y="12" width="16" height="6.5" rx="1.5" />
      <path d="M7.5 8.75h.01M7.5 15.25h.01M10.5 8.75h5M10.5 15.25h5" />
    </svg>
  );
}

function SnapshotIcon() {
  return (
    <svg className="icon-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true">
      <path d="M7.5 8.25H6A2.25 2.25 0 0 0 3.75 10.5v6A2.25 2.25 0 0 0 6 18.75h12a2.25 2.25 0 0 0 2.25-2.25v-6A2.25 2.25 0 0 0 18 8.25h-1.5" />
      <path d="M9 8.25V6.75A3 3 0 0 1 12 3.75a3 3 0 0 1 3 3v1.5" />
      <circle cx="12" cy="13.5" r="2.25" />
    </svg>
  );
}

function EnvIcon() {
  return (
    <svg className="icon-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true">
      <path d="M8 7.75A2.75 2.75 0 1 0 8 13.25h8" />
      <path d="M16 10.75A2.75 2.75 0 1 1 16 16.25H8" />
    </svg>
  );
}

function LogoutIcon() {
  return (
    <svg className="icon-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true">
      <path d="M10.5 5.25H7.5A2.25 2.25 0 0 0 5.25 7.5v9A2.25 2.25 0 0 0 7.5 18.75h3" />
      <path d="M13.5 8.25 18 12l-4.5 3.75" />
      <path d="M10.5 12H18" />
    </svg>
  );
}

function CloseIcon() {
  return (
    <svg className="icon-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true">
      <path d="m7 7 10 10M17 7 7 17" />
    </svg>
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
  const viewportRef = useRef(viewport);
  const boundsRef = useRef<WorkspaceBounds>({ width: 0, height: 0 });
  const panStateRef = useRef<{
    pointerId: number;
    startClientX: number;
    startClientY: number;
    viewport: WorkspaceViewport;
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

    const handleWheel = (event: WheelEvent) => {
      const target = event.target as HTMLElement | null;
      if (target?.closest(".window-frame") || target?.closest(".toolbelt-popover")) {
        return;
      }

      event.preventDefault();
      const currentViewport = viewportRef.current;
      const currentBounds = boundsRef.current;
      const rect = node.getBoundingClientRect();

      if (event.ctrlKey || event.metaKey) {
        const nextScale = clamp(
          currentViewport.scale * Math.exp(-event.deltaY * workspaceZoomWheelFactor),
          minWorkspaceScale,
          maxWorkspaceScale,
        );
        setViewport(
          zoomViewportFromScreenPoint(
            currentViewport,
            nextScale,
            { x: event.clientX - rect.left, y: event.clientY - rect.top },
            currentBounds,
          ),
        );
        return;
      }

      setViewport(
        clampViewport(
          {
            ...currentViewport,
            x: currentViewport.x - event.deltaX,
            y: currentViewport.y - event.deltaY,
          },
          currentBounds,
        ),
      );
    };

    const preventGesture = (event: Event) => {
      event.preventDefault();
    };

    node.addEventListener("wheel", handleWheel, { passive: false });
    node.addEventListener("gesturestart", preventGesture as EventListener, { passive: false });
    node.addEventListener("gesturechange", preventGesture as EventListener, { passive: false });
    node.addEventListener("gestureend", preventGesture as EventListener, { passive: false });

    return () => {
      node.removeEventListener("wheel", handleWheel);
      node.removeEventListener("gesturestart", preventGesture as EventListener);
      node.removeEventListener("gesturechange", preventGesture as EventListener);
      node.removeEventListener("gestureend", preventGesture as EventListener);
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
          if (target.closest(".window-frame") || target.closest(".toolbelt-shell")) {
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
            <CloseIcon />
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
