import {
  Suspense,
  lazy,
  startTransition,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from "react";
import { ArrowClockwise, Camera, Eye, EyeSlash, GitFork, Trash, WarningCircle, X } from "@phosphor-icons/react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  forkMachine,
  createShell,
  createMachine,
  createSnapshot,
  deleteEnvVar,
  deleteMachine,
  deleteShell,
  deleteSnapshot,
  getDefaultWorkspace,
  getSession,
  getTerminalGitStatus,
  listEnvVars,
  listMachines,
  listShells,
  listSnapshots,
  logout,
  requestLoginCode,
  saveDefaultWorkspace,
  setEnvVar,
  subscribeToEventStream,
  verifyLogin,
  type EnvVarCatalog,
  type Machine,
  type Shell,
  type Snapshot,
  type WorkspaceWindow,
} from "./api";
import { GitDiffSidebar } from "./git-diff-sidebar";
import { getMachineColorStyle, getMachineColorStyles } from "./machine-colors";
import { useWorkspaceStore, type RemovedMachineWindowsSnapshot } from "./store";
import type { TerminalConnectionState } from "./terminal";

const TerminalView = lazy(async () => import("./terminal").then((module) => ({ default: module.TerminalView })));

type ModalState =
  | { type: "new-machine" }
  | { type: "fork-machine"; machine: Machine }
  | { type: "snapshot-machine"; machine: Machine }
  | { type: "restore-snapshot"; snapshot: Snapshot }
  | { type: "snapshots" }
  | { type: "env-vars" }
  | null;

const windowGitStatusPollIntervalMs = 4_000;
type MachineMutationContext = {
  previousMachines?: Machine[];
  removedWindows: RemovedMachineWindowsSnapshot | null;
};

const emptyEnvVarCatalog: EnvVarCatalog = {
  envVars: [],
  builtinEnvVars: [],
};

const builtinEnvVarExampleByKey: Record<string, string> = {
  FASCINATE_MACHINE_NAME: "tic-tac-toe",
  FASCINATE_MACHINE_ID: "machine-1",
  FASCINATE_PUBLIC_URL: "https://tic-tac-toe.fascinate.dev",
  FASCINATE_PRIMARY_PORT: "3000",
  FASCINATE_BASE_DOMAIN: "fascinate.dev",
  FASCINATE_HOST_ID: "fascinate-01",
  FASCINATE_HOST_REGION: "ca-east",
};

function maskEnvValue(value: string) {
  return "•".repeat(Math.min(Math.max(value.length, 12), 24));
}

function upsertMachineList(current: Machine[] | undefined, machine: Machine) {
  if (!current) {
    return [machine];
  }

  const next = [...current];
  const existingIndex = next.findIndex((item) => item.id === machine.id || item.name === machine.name);
  if (existingIndex >= 0) {
    next[existingIndex] = machine;
    return next;
  }
  next.push(machine);
  return next;
}

function updateMachineState(current: Machine[] | undefined, machineName: string, nextState: string) {
  if (!current) {
    return current;
  }
  return current.map((machine) =>
    machine.name === machineName
      ? {
          ...machine,
          state: nextState,
          updated_at: new Date().toISOString(),
        }
      : machine,
  );
}

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
  const [editingEnvVar, setEditingEnvVar] = useState<{ key: string; value: string } | null>(null);
  const [revealedEnvVarKeys, setRevealedEnvVarKeys] = useState<Record<string, boolean>>({});
  const [shellCloseError, setShellCloseError] = useState<unknown>(null);
  const [closingShellIDs, setClosingShellIDs] = useState<string[]>([]);

  const hydrate = useWorkspaceStore((state) => state.hydrate);
  const openShellWindow = useWorkspaceStore((state) => state.openShellWindow);
  const windows = useWorkspaceStore((state) => state.windows);
  const windowCwds = useWorkspaceStore((state) => state.windowCwds);
  const closeWindow = useWorkspaceStore((state) => state.closeWindow);
  const closeWindowsForShell = useWorkspaceStore((state) => state.closeWindowsForShell);
  const pruneMissingShells = useWorkspaceStore((state) => state.pruneMissingShells);
  const removeWindowsForMachine = useWorkspaceStore((state) => state.removeWindowsForMachine);
  const restoreRemovedWindows = useWorkspaceStore((state) => state.restoreRemovedWindows);
  const focusWindow = useWorkspaceStore((state) => state.focusWindow);
  const requestViewportFocus = useWorkspaceStore((state) => state.requestViewportFocus);
  const moveWindowToIndex = useWorkspaceStore((state) => state.moveWindowToIndex);
  const closeGitDiffSidebar = useWorkspaceStore((state) => state.closeGitDiffSidebar);
  const gitDiffSidebarWindowID = useWorkspaceStore((state) => state.gitDiffSidebar.windowID);

  const machinesQuery = useQuery({
    queryKey: ["machines"],
    queryFn: listMachines,
  });
  const snapshotsQuery = useQuery({
    queryKey: ["snapshots"],
    queryFn: listSnapshots,
  });
  const shellsQuery = useQuery({
    queryKey: ["shells"],
    queryFn: listShells,
  });
  const envVarsQuery = useQuery({ queryKey: ["env-vars"], queryFn: listEnvVars });
  const workspaceQuery = useQuery({ queryKey: ["workspace"], queryFn: getDefaultWorkspace });

  useEffect(() => {
    if (workspaceQuery.data) {
      startTransition(() => hydrate(workspaceQuery.data));
    }
  }, [hydrate, workspaceQuery.data]);

  useEffect(() => {
    if (!shellsQuery.data) {
      return;
    }
    startTransition(() => pruneMissingShells(shellsQuery.data.map((shell) => shell.id)));
  }, [pruneMissingShells, shellsQuery.data]);

  useEffect(() => {
    const unsubscribe = subscribeToEventStream((event) => {
      if (event.kind.startsWith("machine.")) {
        void queryClient.invalidateQueries({ queryKey: ["machines"] });
      }
      if (event.kind.startsWith("snapshot.")) {
        void queryClient.invalidateQueries({ queryKey: ["snapshots"] });
      }
      if (event.kind.startsWith("shell.")) {
        const shellID = typeof event.payload?.shell_id === "string" ? event.payload.shell_id : "";
        if (event.kind === "shell.deleted" && shellID) {
          startTransition(() => closeWindowsForShell(shellID));
        }
        void queryClient.invalidateQueries({ queryKey: ["shells"] });
      }
    });
    return unsubscribe;
  }, [closeWindowsForShell, queryClient]);

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

  useEffect(() => {
    if (modal?.type === "env-vars") {
      return;
    }
    setEditingEnvVar(null);
    setRevealedEnvVarKeys({});
  }, [modal]);

  useEffect(() => {
    if (modal || !gitDiffSidebarWindowID) {
      return;
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        closeGitDiffSidebar();
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => {
      window.removeEventListener("keydown", handleKeyDown);
    };
  }, [closeGitDiffSidebar, gitDiffSidebarWindowID, modal]);

  const createMachineMutation = useMutation({
    mutationFn: ({ name, snapshotName }: { name: string; snapshotName?: string }) =>
      createMachine(name, snapshotName),
    onSuccess: (machine) => {
      queryClient.setQueryData<Machine[]>(["machines"], (current) => upsertMachineList(current, machine));
      setMachineName("");
      setRestoreTarget("");
      setModal(null);
      void queryClient.invalidateQueries({ queryKey: ["machines"] });
      void queryClient.invalidateQueries({ queryKey: ["snapshots"] });
    },
  });
  const deleteMachineMutation = useMutation<void, Error, string, MachineMutationContext>({
    mutationFn: deleteMachine,
    onMutate: async (machineName) => {
      await queryClient.cancelQueries({ queryKey: ["machines"] });
      const previousMachines = queryClient.getQueryData<Machine[]>(["machines"]);
      const removedWindows = removeWindowsForMachine(machineName);
      queryClient.setQueryData<Machine[]>(["machines"], (current) => updateMachineState(current, machineName, "DELETING"));
      return { previousMachines, removedWindows };
    },
    onError: (_error, _machineName, context) => {
      if (context?.previousMachines) {
        queryClient.setQueryData(["machines"], context.previousMachines);
      }
      if (context?.removedWindows) {
        restoreRemovedWindows(context.removedWindows);
      }
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["machines"] });
    },
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
  const createEnvMutation = useMutation({
    mutationFn: ({ key, value }: { key: string; value: string }) => setEnvVar(key, value),
    onSuccess: () => {
      setEnvForm({ key: "", value: "" });
      void queryClient.invalidateQueries({ queryKey: ["env-vars"] });
    },
  });
  const updateEnvMutation = useMutation({
    mutationFn: ({ key, value }: { key: string; value: string }) => setEnvVar(key, value),
    onSuccess: () => {
      setEditingEnvVar(null);
      void queryClient.invalidateQueries({ queryKey: ["env-vars"] });
    },
  });
  const deleteEnvMutation = useMutation({
    mutationFn: deleteEnvVar,
    onSuccess: (_result, key) => {
      setEditingEnvVar((current) => (current?.key === key ? null : current));
      void queryClient.invalidateQueries({ queryKey: ["env-vars"] });
    },
  });
  const logoutMutation = useMutation({
    mutationFn: logout,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["session"] });
      window.location.reload();
    },
  });

  const machineList = machinesQuery.data ?? [];
  const shellList = shellsQuery.data ?? [];
  const machineColorStyles = useMemo(
    () => getMachineColorStyles([...machineList.map((machine) => machine.name), ...windows.map((window) => window.machineName)]),
    [machineList, windows],
  );
  const frontmostWindowZ = useMemo(
    () => windows.reduce((max, window) => Math.max(max, window.z), 0),
    [windows],
  );
  const frontmostWindowID = useMemo(
    () => windows.find((window) => window.z === frontmostWindowZ)?.id ?? null,
    [frontmostWindowZ, windows],
  );
  const snapshotList = snapshotsQuery.data ?? [];
  const envCatalog = envVarsQuery.data ?? emptyEnvVarCatalog;
  const envList = envCatalog.envVars;
  const builtinEnvList = envCatalog.builtinEnvVars;
  const windowByShellID = useMemo(() => {
    const items = new Map<string, WorkspaceWindow>();
    for (const window of windows) {
      if (!window.shellId) {
        continue;
      }
      items.set(window.shellId, window);
    }
    return items;
  }, [windows]);
  const shellCountByMachine = useMemo(() => {
    const counts = new Map<string, number>();
    for (const shell of shellList) {
      counts.set(shell.machine_name, (counts.get(shell.machine_name) ?? 0) + 1);
    }
    return counts;
  }, [shellList]);
  const orderedSidebarShells = useMemo(() => {
    const shellsByID = new Map(shellList.map((shell) => [shell.id, shell] as const));
    const ordered: Array<{ shell: Shell; window: WorkspaceWindow | null }> = [];
    const seenShellIDs = new Set<string>();
    for (const window of windows) {
      if (!window.shellId) {
        continue;
      }
      const shell = shellsByID.get(window.shellId);
      if (!shell) {
        continue;
      }
      ordered.push({ shell, window });
      seenShellIDs.add(shell.id);
    }
    for (const shell of shellList) {
      if (seenShellIDs.has(shell.id)) {
        continue;
      }
      ordered.push({ shell, window: null });
    }
    return ordered;
  }, [shellList, windows]);
  const sidebarShellNodeRefs = useRef(new Map<string, HTMLDivElement>());
  const sidebarShellDragStateRef = useRef<{
    pointerId: number;
    startY: number;
    windowID: string;
    dragging: boolean;
  } | null>(null);
  const [draggingSidebarShellID, setDraggingSidebarShellID] = useState<string | null>(null);

  const renderBuiltinEnvDescription = (key: string, description: string) => {
    const example = builtinEnvVarExampleByKey[key];
    if (!example) {
      return description;
    }
    return (
      <>
        {description} Example: <code>{example}</code>
      </>
    );
  };

  const openOrFocusShellWindow = (shell: Shell) => {
    const existingWindow = windowByShellID.get(shell.id);
    if (existingWindow) {
      focusWindow(existingWindow.id);
      requestViewportFocus(existingWindow.id);
      return;
    }
    const windowID = openShellWindow({
      shellId: shell.id,
      machineName: shell.machine_name,
      title: shell.name,
    });
    focusWindow(windowID);
    requestViewportFocus(windowID);
  };

  const createShellMutation = useMutation({
    mutationFn: ({ machineName, name }: { machineName: string; name?: string }) => createShell(machineName, name),
    onSuccess: (shell) => {
      queryClient.setQueryData<Shell[]>(["shells"], (current) => {
        const next = current ? [...current.filter((item) => item.id !== shell.id), shell] : [shell];
        next.sort((left, right) => left.created_at.localeCompare(right.created_at) || left.id.localeCompare(right.id));
        return next;
      });
      openOrFocusShellWindow(shell);
      void queryClient.invalidateQueries({ queryKey: ["shells"] });
    },
  });

  const openMachineShell = (machineName: string) => {
    const nextShellNumber = (shellCountByMachine.get(machineName) ?? 0) + 1;
    const shellName = nextShellNumber === 1 ? `${machineName} shell` : `${machineName} shell ${nextShellNumber}`;
    createShellMutation.mutate({ machineName, name: shellName });
  };

  const closeShellResource = async (shellID: string) => {
    setShellCloseError(null);
    setClosingShellIDs((current) => (current.includes(shellID) ? current : [...current, shellID]));
    try {
      await deleteShell(shellID);
      startTransition(() => closeWindowsForShell(shellID));
      queryClient.setQueryData<Shell[]>(["shells"], (current) =>
        current ? current.filter((shell) => shell.id !== shellID) : current,
      );
    } catch (error) {
      setShellCloseError(error);
      return;
    } finally {
      setClosingShellIDs((current) => current.filter((id) => id !== shellID));
    }
    void queryClient.invalidateQueries({ queryKey: ["shells"] });
  };

  const closeShellWindow = async (window: WorkspaceWindow) => {
    if (window.shellId) {
      await closeShellResource(window.shellId);
      return;
    }
    closeWindow(window.id);
  };

  const focusShellWindow = (windowID: string) => {
    focusWindow(windowID);
    requestViewportFocus(windowID);
  };

  const reorderSidebarShellFromPointer = (windowID: string, clientY: number) => {
    const orderedWindowIDs = windows.map((window) => window.id);
    if (!orderedWindowIDs.includes(windowID)) {
      return;
    }

    let targetIndex = orderedWindowIDs.length - 1;
    for (let index = 0; index < orderedWindowIDs.length; index += 1) {
      const node = sidebarShellNodeRefs.current.get(orderedWindowIDs[index]);
      if (!node) {
        continue;
      }
      const rect = node.getBoundingClientRect();
      const midpoint = rect.top + rect.height / 2;
      if (clientY < midpoint) {
        targetIndex = index;
        break;
      }
    }

    moveWindowToIndex(windowID, targetIndex);
  };

  const startSidebarShellDrag = (event: ReactPointerEvent<HTMLElement>, windowID: string) => {
    if (event.button !== 0) {
      return;
    }
    focusShellWindow(windowID);
    sidebarShellDragStateRef.current = {
      pointerId: event.pointerId,
      startY: event.clientY,
      windowID,
      dragging: false,
    };
    if (typeof event.currentTarget.setPointerCapture === "function") {
      event.currentTarget.setPointerCapture(event.pointerId);
    }
  };

  const continueSidebarShellDrag = (event: ReactPointerEvent<HTMLElement>, windowID: string) => {
    const dragState = sidebarShellDragStateRef.current;
    if (!dragState || dragState.pointerId !== event.pointerId || dragState.windowID !== windowID) {
      return;
    }
    if (!dragState.dragging) {
      if (Math.abs(event.clientY - dragState.startY) < 6) {
        return;
      }
      dragState.dragging = true;
      setDraggingSidebarShellID(windowID);
    }
    reorderSidebarShellFromPointer(windowID, event.clientY);
    event.preventDefault();
  };

  const finishSidebarShellDrag = (event: ReactPointerEvent<HTMLElement>, windowID: string) => {
    const dragState = sidebarShellDragStateRef.current;
    if (!dragState || dragState.pointerId !== event.pointerId || dragState.windowID !== windowID) {
      return;
    }
    if (
      typeof event.currentTarget.hasPointerCapture === "function" &&
      event.currentTarget.hasPointerCapture(event.pointerId) &&
      typeof event.currentTarget.releasePointerCapture === "function"
    ) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    sidebarShellDragStateRef.current = null;
    setDraggingSidebarShellID(null);
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
        description="Set env vars for every Fascinate VM. Use ${NAME} to reference."
        onClose={() => setModal(null)}
        wide
      >
        <div className="app-modal-body">
          <section className="app-modal-section">
            <label className="field">
              <span>Name</span>
              <input
                value={envForm.key}
                onChange={(event) => setEnvForm((current) => ({ ...current, key: event.target.value }))}
                placeholder="OPENAI_API_KEY"
              />
            </label>
            <label className="field">
              <span>Value</span>
              <input
                value={envForm.value}
                onChange={(event) => setEnvForm((current) => ({ ...current, value: event.target.value }))}
                placeholder="sk-..."
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
                onClick={() => createEnvMutation.mutate(envForm)}
                disabled={!envForm.key || !envForm.value || createEnvMutation.isPending}
              >
                {createEnvMutation.isPending ? "Saving…" : "Save env var"}
              </button>
            </div>
            <StatusError mutationError={createEnvMutation.error} />
          </section>

          <section className="app-modal-section">
            <div className="app-modal-section-heading">
              <h3>Fascinate Env Vars</h3>
            </div>
            <div className="sidebar-record-list">
              {envList.map((entry) => (
                <article key={entry.key} className="sidebar-record env-var-record">
                  <div>
                    <div className="sidebar-record-heading">
                      <strong>{entry.key}</strong>
                    </div>
                    {editingEnvVar?.key === entry.key ? (
                      <div className="env-var-editor">
                        <label className="field">
                          <span>Name</span>
                          <input value={editingEnvVar.key} disabled />
                        </label>
                        <label className="field">
                          <span>Value</span>
                          <input
                            autoFocus
                            value={editingEnvVar.value}
                            onChange={(event) =>
                              setEditingEnvVar((current) =>
                                current?.key === entry.key ? { ...current, value: event.target.value } : current,
                              )
                            }
                          />
                        </label>
                      </div>
                    ) : (
                      <div
                        className={`muted env-var-record-value${revealedEnvVarKeys[entry.key] ? "" : " env-var-record-value-masked"}`}
                      >
                        {revealedEnvVarKeys[entry.key] ? entry.value : maskEnvValue(entry.value)}
                      </div>
                    )}
                  </div>
                  <div className="actions">
                    {editingEnvVar?.key === entry.key ? (
                      <>
                        <button type="button" onClick={() => setEditingEnvVar(null)}>
                          Cancel
                        </button>
                        <button
                          className="button-primary"
                          type="button"
                          onClick={() => updateEnvMutation.mutate(editingEnvVar)}
                          disabled={!editingEnvVar.value || updateEnvMutation.isPending}
                        >
                          {updateEnvMutation.isPending ? "Saving…" : "Save"}
                        </button>
                      </>
                    ) : (
                      <>
                        <button
                          className="icon-action-button"
                          type="button"
                          aria-label={`${revealedEnvVarKeys[entry.key] ? "Hide" : "Show"} value for ${entry.key}`}
                          title={`${revealedEnvVarKeys[entry.key] ? "Hide" : "Show"} value for ${entry.key}`}
                          onClick={() =>
                            setRevealedEnvVarKeys((current) => ({
                              ...current,
                              [entry.key]: !current[entry.key],
                            }))
                          }
                        >
                          {revealedEnvVarKeys[entry.key] ? (
                            <EyeSlash className="icon-svg" weight="regular" />
                          ) : (
                            <Eye className="icon-svg" weight="regular" />
                          )}
                        </button>
                        <button
                          type="button"
                          aria-label={`Edit ${entry.key}`}
                          onClick={() => setEditingEnvVar({ key: entry.key, value: entry.value })}
                        >
                          Edit
                        </button>
                        <button
                          className="danger"
                          type="button"
                          aria-label={`Delete ${entry.key}`}
                          onClick={() => deleteEnvMutation.mutate(entry.key)}
                        >
                          Delete
                        </button>
                      </>
                    )}
                  </div>
                </article>
              ))}
              {builtinEnvList.map((entry) => (
                <article key={entry.key} className="sidebar-record sidebar-record-static">
                  <div>
                    <div className="sidebar-record-heading">
                      <strong>{entry.key}</strong>
                      <span className="inline-chip">built-in</span>
                    </div>
                    <div className="muted">{renderBuiltinEnvDescription(entry.key, entry.description)}</div>
                  </div>
                </article>
              ))}
            </div>
            <StatusError mutationError={updateEnvMutation.error ?? deleteEnvMutation.error} />
          </section>
        </div>
      </AppModal>
    );
  };

  return (
    <main className="command-center">
      <WorkspaceAutosave enabled={workspaceQuery.isSuccess} />
      <div className="command-center-workspace">
        <WorkspaceRail
          machineColorStyles={machineColorStyles}
          frontmostWindowZ={frontmostWindowZ}
          closingShellIDs={closingShellIDs}
          onCloseShell={(window) => closeShellWindow(window)}
        />
      </div>
      <aside className="control-sidebar" aria-label="Workspace controls">
        <section className="sidebar-shells-section">
          <div className="sidebar-section-header sidebar-section-header-stacked">
            <h2>Shells</h2>
          </div>
          <div className="sidebar-shell-list">
            {orderedSidebarShells.length === 0 ? (
              <div className="sidebar-shells-empty">
                <strong>No shells open</strong>
                <p className="muted">Create a shell from a machine below or from the CLI to start working.</p>
              </div>
            ) : (
              orderedSidebarShells.map(({ shell, window }) => {
                const shellLabel = formatShellListLabel(window ? windowCwds[window.id] : shell.cwd, shell.name);
                const isOpen = Boolean(window);
                const rowID = window?.id ?? shell.id;
                return (
                  <div
                    key={shell.id}
                    className="sidebar-shell-row"
                    data-window-id={window?.id ?? undefined}
                    style={machineColorStyles[shell.machine_name] ?? getMachineColorStyle(shell.machine_name)}
                    data-active={window && frontmostWindowID === window.id ? "true" : "false"}
                    data-dragging={isOpen && draggingSidebarShellID === rowID ? "true" : "false"}
                    ref={(node) => {
                      if (!isOpen) {
                        return;
                      }
                      if (node) {
                        sidebarShellNodeRefs.current.set(rowID, node);
                        return;
                      }
                      sidebarShellNodeRefs.current.delete(rowID);
                    }}
                  >
                    <button
                      type="button"
                      className="sidebar-shell-focus"
                      aria-label={shellLabel}
                      title={`${shellLabel} · ${shell.machine_name}`}
                      onClick={() => openOrFocusShellWindow(shell)}
                      onPointerDown={(event) => {
                        if (window) {
                          startSidebarShellDrag(event, window.id);
                        }
                      }}
                      onPointerMove={(event) => {
                        if (window) {
                          continueSidebarShellDrag(event, window.id);
                        }
                      }}
                      onPointerUp={(event) => {
                        if (window) {
                          finishSidebarShellDrag(event, window.id);
                        }
                      }}
                      onPointerCancel={(event) => {
                        if (window) {
                          finishSidebarShellDrag(event, window.id);
                        }
                      }}
                    >
                      <span className="sidebar-shell-cwd">{shellLabel}</span>
                      <span className="sidebar-shell-machine" aria-hidden="true">
                        <span className="machine-color-dot sidebar-shell-machine-dot" />
                        <span className="sidebar-shell-machine-name">{shell.machine_name}</span>
                      </span>
                    </button>
                    <button
                      type="button"
                      className="sidebar-shell-close"
                      aria-label={`Delete ${shellLabel}`}
                      title={`Delete ${shellLabel}`}
                      onClick={() => {
                        void closeShellResource(shell.id);
                      }}
                      disabled={closingShellIDs.includes(shell.id)}
                    >
                      <X className="icon-svg" weight="regular" />
                    </button>
                  </div>
                );
              })
            )}
          </div>
          <StatusError mutationError={shellCloseError} />
        </section>

        <section className="sidebar-machines-section sidebar-section">
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
            <div className="sidebar-machine-list sidebar-machine-list-compact">
              {machineList.map((machine) => {
                const normalizedMachineState = machine.state.toUpperCase();
                const isPendingMachine = ["CREATING", "STARTING", "STOPPING", "DELETING"].includes(normalizedMachineState);
                const isRunningMachine = normalizedMachineState === "RUNNING" || normalizedMachineState === "READY";
                const machinePublicURL = `https://${machine.name}.fascinate.dev`;
                return (
                  <article
                    key={machine.id}
                    className="machine-card"
                    aria-busy={isPendingMachine}
                    style={machineColorStyles[machine.name] ?? getMachineColorStyle(machine.name)}
                  >
                    <div className="machine-card-header">
                      <div className="machine-card-title">
                        <span className="machine-color-dot" aria-hidden="true" />
                        <div className="machine-card-title-meta">
                          <strong>{machine.name}</strong>
                        </div>
                      </div>
                      <div className="actions machine-card-actions">
                        {isPendingMachine ? (
                          <span
                            className="machine-card-pending-indicator"
                            role="status"
                            aria-label={`${normalizedMachineState.toLowerCase()} ${machine.name}`}
                          >
                            <span className="basic-spinner" aria-hidden="true" />
                          </span>
                        ) : (
                          <>
                            {isRunningMachine ? (
                              <>
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
                              </>
                            ) : null}
                            <button
                              className="icon-action-button danger"
                              type="button"
                              aria-label={`Delete ${machine.name}`}
                              title={`Delete ${machine.name}`}
                              onClick={() => deleteMachineMutation.mutate(machine.name)}
                            >
                              <Trash className="icon-svg" weight="regular" />
                            </button>
                          </>
                        )}
                      </div>
                    </div>
                    <div className="machine-card-footer">
                      <a
                        className="sidebar-text-button"
                        href={machinePublicURL}
                        target="_blank"
                        rel="noreferrer"
                        aria-label={`Open app for ${machine.name}`}
                        title={machinePublicURL}
                      >
                        <span className="sidebar-text-button-label">Open app</span>
                      </a>
                      {isRunningMachine && !isPendingMachine ? (
                        <button
                          className="sidebar-text-button"
                          type="button"
                          onClick={() => openMachineShell(machine.name)}
                        >
                          <span className="sidebar-text-button-label">New shell</span>
                        </button>
                      ) : null}
                    </div>
                  </article>
                );
              })}
            </div>
          )}
          <StatusError mutationError={createShellMutation.error ?? deleteMachineMutation.error} />
        </section>

        <section className="control-sidebar-footer">
          <div className="control-sidebar-manage">
            <h2 className="control-sidebar-manage-label">Manage</h2>
            <div className="control-sidebar-manage-actions">
              <button className="sidebar-text-button" type="button" onClick={() => setModal({ type: "env-vars" })}>
                <span className="sidebar-text-button-label">Env vars</span>
              </button>
              <button className="sidebar-text-button" type="button" onClick={() => setModal({ type: "snapshots" })}>
                <span className="sidebar-text-button-label">Snapshots</span>
              </button>
            </div>
          </div>
        </section>
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
      </aside>
      <GitDiffSidebar />
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

function WorkspaceRail({
  machineColorStyles,
  frontmostWindowZ,
  closingShellIDs,
  onCloseShell,
}: {
  machineColorStyles: Record<string, CSSProperties>;
  frontmostWindowZ: number;
  closingShellIDs: string[];
  onCloseShell: (window: WorkspaceWindow) => Promise<void>;
}) {
  const windows = useWorkspaceStore((state) => state.windows);
  const windowCwds = useWorkspaceStore((state) => state.windowCwds);
  const focusWindow = useWorkspaceStore((state) => state.focusWindow);
  const viewportFocusRequest = useWorkspaceStore((state) => state.viewportFocusRequest);
  const clearViewportFocusRequest = useWorkspaceStore((state) => state.clearViewportFocusRequest);
  const moveWindowToIndex = useWorkspaceStore((state) => state.moveWindowToIndex);
  const setWindowCwd = useWorkspaceStore((state) => state.setWindowCwd);
  const openGitDiffSidebar = useWorkspaceStore((state) => state.openGitDiffSidebar);
  const gitDiffSidebarWindowID = useWorkspaceStore((state) => state.gitDiffSidebar.windowID);

  const workspaceViewportRef = useRef<HTMLDivElement | null>(null);
  const windowNodeRefs = useRef(new Map<string, HTMLDivElement>());
  const dragStateRef = useRef<{
    pointerId: number;
    startX: number;
    windowID: string;
    dragging: boolean;
  } | null>(null);
  const [draggingWindowID, setDraggingWindowID] = useState<string | null>(null);
  const [windowConnectionStates, setWindowConnectionStates] = useState<Record<string, TerminalConnectionState>>({});

  useEffect(() => {
    const windowIDs = new Set(windows.map((window) => window.id));
    setWindowConnectionStates((current) => {
      let changed = false;
      const next: Record<string, TerminalConnectionState> = {};
      for (const [windowID, connectionState] of Object.entries(current)) {
        if (!windowIDs.has(windowID)) {
          changed = true;
          continue;
        }
        next[windowID] = connectionState;
      }
      return changed ? next : current;
    });
  }, [windows]);

  useEffect(() => {
    if (!viewportFocusRequest) {
      return;
    }
    const node = windowNodeRefs.current.get(viewportFocusRequest.windowID);
    if (!node) {
      clearViewportFocusRequest();
      return;
    }
    node.scrollIntoView({
      behavior:
        typeof window !== "undefined" &&
        typeof window.matchMedia === "function" &&
        window.matchMedia("(prefers-reduced-motion: reduce)").matches
          ? "auto"
          : "smooth",
      inline: "center",
      block: "nearest",
    });
    clearViewportFocusRequest();
  }, [clearViewportFocusRequest, viewportFocusRequest, windows]);

  useEffect(() => {
    if (!draggingWindowID) {
      document.body.classList.remove("workspace-shell-dragging");
      return;
    }
    document.body.classList.add("workspace-shell-dragging");
    return () => {
      document.body.classList.remove("workspace-shell-dragging");
    };
  }, [draggingWindowID]);

  const reorderWindowFromPointer = (windowID: string, clientX: number) => {
    const orderedWindowIDs = windows.map((window) => window.id);
    if (!orderedWindowIDs.includes(windowID)) {
      return;
    }

    let targetIndex = orderedWindowIDs.length - 1;
    for (let index = 0; index < orderedWindowIDs.length; index += 1) {
      const node = windowNodeRefs.current.get(orderedWindowIDs[index]);
      if (!node) {
        continue;
      }
      const rect = node.getBoundingClientRect();
      const midpoint = rect.left + rect.width / 2;
      if (clientX < midpoint) {
        targetIndex = index;
        break;
      }
    }

    moveWindowToIndex(windowID, targetIndex);
  };

  const startWindowHeaderDrag = (event: ReactPointerEvent<HTMLElement>, windowID: string) => {
    if (event.button !== 0) {
      return;
    }
    focusWindow(windowID);
    dragStateRef.current = {
      pointerId: event.pointerId,
      startX: event.clientX,
      windowID,
      dragging: false,
    };
    if (typeof event.currentTarget.setPointerCapture === "function") {
      event.currentTarget.setPointerCapture(event.pointerId);
    }
  };

  const continueWindowHeaderDrag = (event: ReactPointerEvent<HTMLElement>, windowID: string) => {
    const dragState = dragStateRef.current;
    if (!dragState || dragState.pointerId !== event.pointerId || dragState.windowID !== windowID) {
      return;
    }
    if (!dragState.dragging) {
      if (Math.abs(event.clientX - dragState.startX) < 10) {
        return;
      }
      dragState.dragging = true;
      setDraggingWindowID(windowID);
    }
    reorderWindowFromPointer(windowID, event.clientX);
    event.preventDefault();
  };

  const finishWindowHeaderDrag = (event: ReactPointerEvent<HTMLElement>, windowID: string) => {
    const dragState = dragStateRef.current;
    if (!dragState || dragState.pointerId !== event.pointerId || dragState.windowID !== windowID) {
      return;
    }
    if (
      typeof event.currentTarget.hasPointerCapture === "function" &&
      event.currentTarget.hasPointerCapture(event.pointerId) &&
      typeof event.currentTarget.releasePointerCapture === "function"
    ) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    dragStateRef.current = null;
    setDraggingWindowID(null);
  };

  return (
    <section className="workspace">
      <div ref={workspaceViewportRef} className="workspace-viewport workspace-strip-viewport">
        <div className="workspace-strip">
          {windows.length === 0 ? (
            <div className="workspace-empty-state">
              <strong>No shells open</strong>
              <p>Open a shell from the machines sidebar to place it into the horizontal workspace.</p>
            </div>
          ) : null}
          {windows.map((window, index) => (
            <div
              key={window.id}
              className="workspace-strip-item"
              data-window-id={window.id}
              ref={(node) => {
                if (node) {
                  windowNodeRefs.current.set(window.id, node);
                  return;
                }
                windowNodeRefs.current.delete(window.id);
              }}
            >
              <WindowFrame
                window={window}
                machineColorStyle={machineColorStyles[window.machineName] ?? getMachineColorStyle(window.machineName)}
                onClose={() => {
                  void onCloseShell(window);
                }}
                isClosing={Boolean(window.shellId && closingShellIDs.includes(window.shellId))}
                onFocus={() => focusWindow(window.id)}
                onHeaderPointerDown={(event) => startWindowHeaderDrag(event, window.id)}
                onHeaderPointerMove={(event) => continueWindowHeaderDrag(event, window.id)}
                onHeaderPointerUp={(event) => finishWindowHeaderDrag(event, window.id)}
                onHeaderPointerCancel={(event) => finishWindowHeaderDrag(event, window.id)}
                onOpenGitDiff={() => {
                  focusWindow(window.id);
                  openGitDiffSidebar(window.id);
                }}
                isLeadingWindow={index === 0}
                isFrontmost={window.z === frontmostWindowZ}
                isGitDiffActive={gitDiffSidebarWindowID === window.id}
                isDragging={draggingWindowID === window.id}
                cwd={windowCwds[window.id]}
                connectionState={windowConnectionStates[window.id]}
              >
                {window.shellId ? (
                  <Suspense fallback={<div className="terminal-loading">Opening terminal…</div>}>
                    <TerminalView
                      shellId={window.shellId}
                      machineName={window.machineName}
                      title={window.title}
                      onCwdChange={(cwd) => setWindowCwd(window.id, cwd)}
                      onConnectionStateChange={(connectionState) => {
                        setWindowConnectionStates((current) => {
                          if (current[window.id] === connectionState) {
                            return current;
                          }
                          return {
                            ...current,
                            [window.id]: connectionState,
                          };
                        });
                      }}
                    />
                  </Suspense>
                ) : (
                  <div className="terminal-loading">Shell metadata is unavailable.</div>
                )}
              </WindowFrame>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

function formatCwdDisplay(cwd: string | undefined) {
  if (!cwd) {
    return undefined;
  }
  return cwd.replace(/^\/home\/ubuntu(?=\/|$)/, "~");
}

function formatShellListLabel(cwd: string | undefined, fallback: string) {
  const displayCwd = formatCwdDisplay(cwd);
  if (!displayCwd) {
    return fallback;
  }
  return displayCwd;
}

function WindowFrame({
  window: layoutWindow,
  machineColorStyle,
  children,
  onClose,
  isClosing,
  onFocus,
  onHeaderPointerDown,
  onHeaderPointerMove,
  onHeaderPointerUp,
  onHeaderPointerCancel,
  onOpenGitDiff,
  isLeadingWindow,
  isFrontmost,
  isGitDiffActive,
  isDragging,
  cwd,
  connectionState,
}: {
  window: WorkspaceWindow;
  machineColorStyle: CSSProperties;
  children: ReactNode;
  onClose: () => void;
  isClosing: boolean;
  onFocus: () => void;
  onHeaderPointerDown: (event: ReactPointerEvent<HTMLElement>) => void;
  onHeaderPointerMove: (event: ReactPointerEvent<HTMLElement>) => void;
  onHeaderPointerUp: (event: ReactPointerEvent<HTMLElement>) => void;
  onHeaderPointerCancel: (event: ReactPointerEvent<HTMLElement>) => void;
  onOpenGitDiff: () => void;
  isLeadingWindow: boolean;
  isFrontmost: boolean;
  isGitDiffActive: boolean;
  isDragging: boolean;
  cwd?: string;
  connectionState?: TerminalConnectionState;
}) {
  const displayCwd = formatCwdDisplay(cwd) ?? layoutWindow.title;

  return (
    <div
      className="window-frame"
      style={machineColorStyle}
      data-active={isFrontmost ? "true" : "false"}
      data-dragging={isDragging ? "true" : "false"}
      onPointerDown={onFocus}
    >
      <header
        className="window-header"
        data-dragging={isDragging ? "true" : "false"}
        onPointerDown={onHeaderPointerDown}
        onPointerMove={onHeaderPointerMove}
        onPointerUp={onHeaderPointerUp}
        onPointerCancel={onHeaderPointerCancel}
      >
        {isLeadingWindow ? <div className="window-header-brand">Fascinate</div> : null}
        <div className="window-header-main">
          <div className="window-header-actions window-header-actions-start">
            <button
              className="window-header-button window-header-button-icon window-header-button-danger"
              type="button"
              aria-label="Close shell"
              title="Close shell"
              onPointerDown={(event) => event.stopPropagation()}
              onDoubleClick={(event) => event.stopPropagation()}
              onClick={onClose}
              disabled={isClosing}
            >
              <X className="icon-svg" weight="regular" />
            </button>
          </div>
          <div className="window-header-title">
            <strong title={displayCwd}>{displayCwd}</strong>
          </div>
          <div className="window-header-actions window-header-actions-end">
            <WindowGitDiffButton
              title={layoutWindow.title}
              shellId={layoutWindow.shellId}
              cwd={cwd}
              isFrontmost={isFrontmost}
              isActive={isGitDiffActive}
              onOpen={onOpenGitDiff}
            />
            <WindowShellConnectionIndicator connectionState={connectionState} />
            <div className="window-header-machine-meta" aria-hidden="true">
              <span className="machine-color-dot window-header-machine-dot" />
              <span className="window-header-machine-name">{layoutWindow.machineName}</span>
            </div>
          </div>
        </div>
      </header>
      <div className="window-body">{children}</div>
    </div>
  );
}

function WindowShellConnectionIndicator({ connectionState }: { connectionState?: TerminalConnectionState }) {
  if (connectionState !== "reconnecting" && connectionState !== "error") {
    return null;
  }

  const label =
    connectionState === "reconnecting" ? "Reconnecting shell…" : "Shell needs attention before it can reconnect.";

  return (
    <span
      className="window-shell-status"
      data-state={connectionState}
      role="status"
      aria-live="polite"
      aria-label={label}
      title={label}
    >
      {connectionState === "reconnecting" ? (
        <ArrowClockwise className="icon-svg window-shell-status-icon-spinning" weight="regular" />
      ) : (
        <WarningCircle className="icon-svg" weight="regular" />
      )}
    </span>
  );
}

function WindowGitDiffButton({
  title,
  shellId,
  cwd,
  isFrontmost,
  isActive,
  onOpen,
}: {
  title: string;
  shellId?: string;
  cwd?: string;
  isFrontmost: boolean;
  isActive: boolean;
  onOpen: () => void;
}) {
  const statusQuery = useQuery({
    queryKey: ["terminal-git-status", shellId, cwd],
    queryFn: () => getTerminalGitStatus(shellId ?? "", cwd ?? ""),
    enabled: Boolean(shellId && cwd),
    staleTime: 15_000,
    refetchInterval: shellId && cwd && (isFrontmost || isActive) ? windowGitStatusPollIntervalMs : false,
    refetchOnWindowFocus: false,
    retry: false,
  });

  if (statusQuery.data?.state !== "ready") {
    return null;
  }

  const additions = statusQuery.data.additions ?? 0;
  const deletions = statusQuery.data.deletions ?? 0;

  return (
    <button
      className="window-git-diff-button"
      type="button"
      aria-label={`Open git diff for ${title}`}
      title={`Open git diff for ${title}`}
      data-active={isActive ? "true" : "false"}
      onPointerDown={(event) => event.stopPropagation()}
      onDoubleClick={(event) => event.stopPropagation()}
      onClick={onOpen}
    >
      <span className="window-git-diff-button-stat window-git-diff-button-stat-added">
        +{additions.toLocaleString()}
      </span>
      <span className="window-git-diff-button-stat window-git-diff-button-stat-deleted">
        -{deletions.toLocaleString()}
      </span>
    </button>
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
