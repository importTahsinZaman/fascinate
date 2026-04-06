import type {
  EnvVar,
  GitDiffBatchRequest,
  GitFileDiff,
  GitRepoStatus,
  Machine,
  MachineEnv,
  Snapshot,
  TerminalSession,
  User,
  WorkspaceLayout,
  WorkspaceWindow,
} from "./api";

type MockTerminalRecord = {
  id: string;
  machineName: string;
  cwd: string;
  lines: string[];
};

type MockRepoRecord = {
  repoRoot: string;
  branch: string;
  additions: number;
  deletions: number;
  files: NonNullable<GitRepoStatus["files"]>;
  diffs: Record<string, GitFileDiff>;
};

type MockState = {
  signedIn: boolean;
  user: User;
  machines: Machine[];
  snapshots: Snapshot[];
  envVars: EnvVar[];
  workspace: WorkspaceLayout;
  terminalCounter: number;
  terminals: Map<string, MockTerminalRecord>;
  reposByCwd: Record<string, MockRepoRecord>;
};

const mockUser: User = {
  id: "mock-user-1",
  email: "designer@example.com",
  is_admin: true,
};

const mockMachines: Machine[] = [
  {
    id: "machine-m1",
    name: "m-1",
    state: "RUNNING",
    primary_port: 3000,
    host_id: "fascinate-local",
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
  },
  {
    id: "machine-cool-space",
    name: "cool-space",
    state: "RUNNING",
    primary_port: 3000,
    host_id: "fascinate-local",
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
  },
  {
    id: "machine-prototype-lab",
    name: "prototype-lab",
    state: "RUNNING",
    primary_port: 3000,
    host_id: "fascinate-local",
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
  },
];

const mockWorkspaceWindows: WorkspaceWindow[] = [
  {
    id: "window-m1",
    machineName: "m-1",
    title: "m-1 shell",
    sessionId: "mock-session-m1",
    x: 0,
    y: 0,
    width: 796,
    height: 900,
    z: 2,
  },
  {
    id: "window-cool-space",
    machineName: "cool-space",
    title: "cool-space shell",
    sessionId: "mock-session-cool-space",
    x: 796,
    y: 0,
    width: 796,
    height: 900,
    z: 3,
  },
];

const metadataIndexPatch = [
  "diff --git a/connector/src/main/java/com/aisi/connector/controller/MetadataIndexController.java b/connector/src/main/java/com/aisi/connector/controller/MetadataIndexController.java",
  "index 81dfeb0..f3220ef 100644",
  "--- a/connector/src/main/java/com/aisi/connector/controller/MetadataIndexController.java",
  "+++ b/connector/src/main/java/com/aisi/connector/controller/MetadataIndexController.java",
  "@@ -1,6 +1,7 @@",
  " package com.aisi.connector.controller;",
  " ",
  " import com.aisi.connector.config.SapProperties;",
  "+import jakarta.annotation.PostConstruct;",
  " import com.aisi.connector.mock.MockMetadataIndexService;",
  " import com.aisi.connector.model.ConnectorResponse;",
  " import com.aisi.connector.service.MetadataIndexService;",
  "@@ -24,6 +25,20 @@ public class MetadataIndexController {",
  "     this.props = props;",
  " }",
  " ",
  "+@PostConstruct",
  "+void validateServices() {",
  "+    if (props.isMockMode() && mockService == null) {",
  "+        throw new IllegalStateException(",
  "+            \"Mock mode is enabled (sap.mock-mode) but MockMetadataIndexService bean is not available. \"",
  "+                + \"Ensure sap.mock-mode is configured correctly.\");",
  "+    }",
  "+    if (!props.isMockMode() && realService == null) {",
  "+        throw new IllegalStateException(",
  "+            \"Real mode is enabled but MetadataIndexService bean is not available. \"",
  "+                + \"Ensure sap.mock-mode is configured correctly.\");",
  "+    }",
  "+}",
  "+",
  " @GetMapping(\"/probe-changes\")",
  " public ConnectorResponse probeChanges(@RequestParam(value = \"since\", required = true) String since) {",
  "     Object data;",
].join("\n");

const metadataServicePatch = [
  "diff --git a/connector/src/main/java/com/aisi/connector/service/MetadataIndexService.java b/connector/src/main/java/com/aisi/connector/service/MetadataIndexService.java",
  "index 84ef000..ad910ef 100644",
  "--- a/connector/src/main/java/com/aisi/connector/service/MetadataIndexService.java",
  "+++ b/connector/src/main/java/com/aisi/connector/service/MetadataIndexService.java",
  "@@ -48,10 +48,13 @@ public class MetadataIndexService {",
  "         return response;",
  "     }",
  " ",
  "-    private String normalize(String value) {",
  "-        return value == null ? null : value.trim();",
  "+    private Optional<String> normalize(String value) {",
  "+        if (value == null) {",
  "+            return Optional.empty();",
  "+        }",
  "+        return Optional.of(value.trim());",
  "     }",
  " }",
].join("\n");

function createInitialMockState(): MockState {
  const terminals = new Map<string, MockTerminalRecord>([
    [
      "mock-session-m1",
      {
        id: "mock-session-m1",
        machineName: "m-1",
        cwd: "/home/ubuntu/aisi",
        lines: [
          "Welcome back.",
          "repo: ~/aisi",
          "branch: feature/metadata-guardrails",
          "",
          "ubuntu@m-1:~/aisi$ ./gradlew test",
          "> Task :connector:test",
          "BUILD SUCCESSFUL in 8s",
          "",
          "ubuntu@m-1:~/aisi$ ",
        ],
      },
    ],
    [
      "mock-session-cool-space",
      {
        id: "mock-session-cool-space",
        machineName: "cool-space",
        cwd: "/home/ubuntu/project-alpha",
        lines: [
          "Product exploration sandbox",
          "",
          "ubuntu@cool-space:~/project-alpha$ pnpm dev",
          "VITE v7.3.1 ready in 410 ms",
          "Local: http://127.0.0.1:5173/",
          "",
          "ubuntu@cool-space:~/project-alpha$ ",
        ],
      },
    ],
  ]);

  return {
    signedIn: true,
    user: clone(mockUser),
    machines: clone(mockMachines),
    snapshots: [
      {
        id: "snapshot-baseline",
        name: "baseline",
        source_machine_name: "m-1",
        state: "READY",
        created_at: "2026-04-01T00:00:00Z",
        updated_at: "2026-04-01T00:00:00Z",
      },
    ],
    envVars: [
      { key: "FASCINATE_PUBLIC_URL", value: "https://fascinate.dev" },
      { key: "APP_THEME", value: "dark" },
    ],
    workspace: {
      version: 3,
      windows: clone(mockWorkspaceWindows),
      viewport: { x: 120, y: 96, scale: 1 },
    },
    terminalCounter: 3,
    terminals,
    reposByCwd: {
      "/home/ubuntu/aisi": {
        repoRoot: "/home/ubuntu/aisi",
        branch: "feature/metadata-guardrails",
        additions: 18,
        deletions: 4,
        files: [
          {
            path: "connector/src/main/java/com/aisi/connector/controller/MetadataIndexController.java",
            kind: "modified",
            worktree_status: "M",
          },
          {
            path: "connector/src/main/java/com/aisi/connector/service/MetadataIndexService.java",
            kind: "modified",
            worktree_status: "M",
          },
        ],
        diffs: {
          "connector/src/main/java/com/aisi/connector/controller/MetadataIndexController.java": {
            state: "ready",
            path: "connector/src/main/java/com/aisi/connector/controller/MetadataIndexController.java",
            patch: metadataIndexPatch,
            additions: 15,
            deletions: 0,
          },
          "connector/src/main/java/com/aisi/connector/service/MetadataIndexService.java": {
            state: "ready",
            path: "connector/src/main/java/com/aisi/connector/service/MetadataIndexService.java",
            patch: metadataServicePatch,
            additions: 3,
            deletions: 4,
          },
        },
      },
      "/home/ubuntu/project-alpha": {
        repoRoot: "/home/ubuntu/project-alpha",
        branch: "main",
        additions: 0,
        deletions: 0,
        files: [],
        diffs: {},
      },
    },
  };
}

let state = createInitialMockState();
let pendingMachineReadyTimeouts: Array<ReturnType<typeof setTimeout>> = [];

function clearPendingMachineReadyTimeouts() {
  for (const timeout of pendingMachineReadyTimeouts) {
    clearTimeout(timeout);
  }
  pendingMachineReadyTimeouts = [];
}

function scheduleMockMachineReady(name: string, delayMs: number) {
  const timeout = setTimeout(() => {
    state.machines = state.machines.map((machine) =>
      machine.name === name
        ? {
            ...machine,
            state: "RUNNING",
            updated_at: new Date().toISOString(),
          }
        : machine,
    );
    pendingMachineReadyTimeouts = pendingMachineReadyTimeouts.filter((item) => item !== timeout);
  }, delayMs);
  pendingMachineReadyTimeouts = [...pendingMachineReadyTimeouts, timeout];
}

export function isMockUIEnabled() {
  return import.meta.env.VITE_FASCINATE_UI_MOCK === "1";
}

export function resetMockControlPlaneState() {
  clearPendingMachineReadyTimeouts();
  state = createInitialMockState();
}

export async function getMockSession() {
  return state.signedIn ? clone(state.user) : null;
}

export async function requestMockLoginCode(_email: string) {
  return { status: "verification code sent" };
}

export async function verifyMockLogin(email: string, _code: string) {
  state.signedIn = true;
  state.user = {
    ...state.user,
    email: email.trim() || state.user.email,
  };
  return {
    user: clone(state.user),
    expires_at: futureTimestamp(),
  };
}

export async function mockLogout() {
  state.signedIn = false;
}

export async function listMockMachines() {
  return clone(state.machines);
}

export async function createMockMachine(name: string, snapshotName?: string) {
  const machine: Machine = {
    id: `machine-${slug(name)}`,
    name,
    state: "CREATING",
    primary_port: 3000,
    host_id: "fascinate-local",
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
  };
  state.machines = [...state.machines, machine];
  scheduleMockMachineReady(name, snapshotName ? 700 : 1_500);
  if (snapshotName) {
    state.snapshots = [
      ...state.snapshots,
      {
        id: `snapshot-${slug(name)}-restore`,
        name: `${name}-from-${snapshotName}`,
        source_machine_name: name,
        state: "READY",
        created_at: "2026-04-01T00:00:00Z",
        updated_at: "2026-04-01T00:00:00Z",
      },
    ];
  }
  return clone(machine);
}

export async function deleteMockMachine(name: string) {
  state.machines = state.machines.filter((machine) => machine.name !== name);
  state.workspace = {
    ...state.workspace,
    windows: state.workspace.windows.filter((window) => window.machineName !== name),
  };
  for (const [sessionId, session] of state.terminals.entries()) {
    if (session.machineName === name) {
      state.terminals.delete(sessionId);
    }
  }
}

export async function forkMockMachine(sourceName: string, targetName: string) {
  const source = state.machines.find((machine) => machine.name === sourceName);
  const forked: Machine = {
    id: `machine-${slug(targetName)}`,
    name: targetName,
    state: "RUNNING",
    primary_port: source?.primary_port ?? 3000,
    host_id: "fascinate-local",
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
  };
  state.machines = [...state.machines, forked];
  return clone(forked);
}

export async function listMockSnapshots() {
  return clone(state.snapshots);
}

export async function createMockSnapshot(machineName: string, snapshotName: string) {
  const snapshot: Snapshot = {
    id: `snapshot-${slug(snapshotName)}`,
    name: snapshotName,
    source_machine_name: machineName,
    state: "READY",
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
  };
  state.snapshots = [...state.snapshots, snapshot];
  return clone(snapshot);
}

export async function deleteMockSnapshot(name: string) {
  state.snapshots = state.snapshots.filter((snapshot) => snapshot.name !== name);
}

export async function listMockEnvVars() {
  return clone(state.envVars);
}

export async function setMockEnvVar(key: string, value: string) {
  const next = { key, value };
  state.envVars = [...state.envVars.filter((entry) => entry.key !== key), next];
  return clone(next);
}

export async function deleteMockEnvVar(key: string) {
  state.envVars = state.envVars.filter((entry) => entry.key !== key);
}

export async function getMockMachineEnv(name: string): Promise<MachineEnv> {
  return {
    machine_name: name,
    entries: Object.fromEntries(state.envVars.map((entry) => [entry.key, entry.value])),
  };
}

export async function getMockDefaultWorkspace() {
  return clone(state.workspace);
}

export async function saveMockDefaultWorkspace(layout: WorkspaceLayout) {
  state.workspace = clone(layout);
  return { name: "default", layout: clone(layout) };
}

export async function createMockTerminalSession(machineName: string, _cols: number, _rows: number): Promise<TerminalSession> {
  const id = `mock-session-${state.terminalCounter++}`;
  const cwd = defaultCwdForMachine(machineName);
  state.terminals.set(id, {
    id,
    machineName,
    cwd,
    lines: mockLinesForMachine(machineName, cwd),
  });
  return terminalSessionResponse(id, machineName);
}

export async function attachMockTerminalSession(sessionId: string, _cols: number, _rows: number): Promise<TerminalSession> {
  const session = state.terminals.get(sessionId);
  if (session) {
    return terminalSessionResponse(session.id, session.machineName);
  }
  return createMockTerminalSession("m-1", 120, 40);
}

export async function deleteMockTerminalSession(sessionId: string) {
  state.terminals.delete(sessionId);
}

export function getMockTerminalPresentation(sessionId: string, machineName: string) {
  const session = state.terminals.get(sessionId);
  if (session) {
    return clone(session);
  }
  const cwd = defaultCwdForMachine(machineName);
  return {
    id: sessionId,
    machineName,
    cwd,
    lines: mockLinesForMachine(machineName, cwd),
  };
}

export async function getMockTerminalGitStatus(sessionId: string, cwd: string): Promise<GitRepoStatus> {
  const session = state.terminals.get(sessionId);
  const resolvedCwd = cwd.trim() || session?.cwd || "";
  const repo = state.reposByCwd[resolvedCwd];
  if (!repo) {
    return { state: "not_repo" };
  }
  return {
    state: "ready",
    repo_root: repo.repoRoot,
    branch: repo.branch,
    additions: repo.additions,
    deletions: repo.deletions,
    files: clone(repo.files),
  };
}

export async function getMockTerminalGitDiffBatch(_sessionId: string, diffRequest: GitDiffBatchRequest) {
  const repo = state.reposByCwd[diffRequest.cwd] ?? state.reposByCwd[diffRequest.repo_root];
  if (!repo) {
    return { diffs: [] };
  }
  return {
    diffs: diffRequest.files.map((file) => {
      const diff = repo.diffs[file.path];
      if (diff) {
        return clone(diff);
      }
      return {
        state: "error",
        path: file.path,
        previous_path: file.previous_path,
        message: "No mock diff available for this file.",
      };
    }),
  };
}

function terminalSessionResponse(id: string, machineName: string): TerminalSession {
  return {
    id,
    machine_name: machineName,
    host_id: "mock-host",
    attach_url: `/v1/terminal/sessions/${id}/stream?token=mock-token`,
    expires_at: futureTimestamp(),
  };
}

function futureTimestamp() {
  return new Date(Date.now() + 60 * 60 * 1000).toISOString();
}

function defaultCwdForMachine(machineName: string) {
  switch (machineName) {
    case "m-1":
      return "/home/ubuntu/aisi";
    case "cool-space":
      return "/home/ubuntu/project-alpha";
    default:
      return `/home/ubuntu/${slug(machineName)}`;
  }
}

function mockLinesForMachine(machineName: string, cwd: string) {
  if (machineName === "m-1") {
    return [
      "Fascinate mock terminal",
      `repo: ${cwd.replace("/home/ubuntu", "~")}`,
      "branch: feature/metadata-guardrails",
      "",
      "ubuntu@m-1:~/aisi$ ./gradlew test",
      "> Task :connector:test",
      "BUILD SUCCESSFUL in 8s",
      "",
      "ubuntu@m-1:~/aisi$ ",
    ];
  }
  if (machineName === "cool-space") {
    return [
      "Fascinate mock terminal",
      `repo: ${cwd.replace("/home/ubuntu", "~")}`,
      "branch: main",
      "",
      "ubuntu@cool-space:~/project-alpha$ pnpm dev",
      "VITE ready in 410 ms",
      "Local: http://127.0.0.1:5173/",
      "",
      "ubuntu@cool-space:~/project-alpha$ ",
    ];
  }
  return [
    "Fascinate mock terminal",
    `cwd: ${cwd.replace("/home/ubuntu", "~")}`,
    "",
    `ubuntu@${machineName}:${cwd.replace("/home/ubuntu", "~")}$ `,
  ];
}

function slug(value: string) {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}
