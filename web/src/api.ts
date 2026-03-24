export type User = {
  id: string;
  email: string;
  is_admin: boolean;
};

export type Machine = {
  id: string;
  name: string;
  state: string;
  url?: string;
  primary_port: number;
  host_id?: string;
  created_at: string;
  updated_at: string;
};

export type Snapshot = {
  id: string;
  name: string;
  source_machine_name?: string;
  state: string;
  created_at: string;
  updated_at: string;
};

export type EnvVar = {
  key: string;
  value: string;
};

export type MachineEnv = {
  machine_name: string;
  entries: Record<string, string>;
};

export type TerminalSession = {
 id: string;
 machine_name: string;
 host_id: string;
 attach_url: string;
 expires_at: string;
};

export type GitChangedFile = {
  path: string;
  previous_path?: string;
  kind: string;
  index_status?: string;
  worktree_status?: string;
};

export type GitRepoStatus = {
  state: string;
  repo_root?: string;
  branch?: string;
  additions?: number;
  deletions?: number;
  files?: GitChangedFile[];
};

export type GitDiffBatchFile = {
  path: string;
  previous_path?: string;
  kind?: string;
  index_status?: string;
  worktree_status?: string;
};

export type GitDiffBatchRequest = {
  cwd: string;
  repo_root: string;
  files: GitDiffBatchFile[];
};

export type GitFileDiff = {
  state: string;
  path: string;
  previous_path?: string;
  patch?: string;
  additions?: number;
  deletions?: number;
  message?: string;
};

export type WorkspaceLayout = {
  version: number;
  windows: WorkspaceWindow[];
  viewport?: WorkspaceViewport;
};

export type WorkspaceViewport = {
  x: number;
  y: number;
  scale: number;
};

export type WorkspaceWindow = {
  id: string;
  machineName: string;
  title: string;
  sessionId?: string;
  x: number;
  y: number;
  width: number;
  height: number;
  z: number;
};

export class HttpError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers ?? {}),
    },
    ...init,
  });

  if (!response.ok) {
    let message = response.statusText;
    try {
      const body = (await response.json()) as { error?: string };
      if (body.error) {
        message = body.error;
      }
    } catch {
      // ignore
    }
    throw new HttpError(response.status, message);
  }

  if (response.status === 204) {
    return undefined as T;
  }

  return (await response.json()) as T;
}

export async function getSession(): Promise<User | null> {
  try {
    const body = await request<{ user: User }>("/v1/auth/session");
    return body.user;
  } catch (error) {
    if (error instanceof HttpError && error.status === 401) {
      return null;
    }
    throw error;
  }
}

export async function requestLoginCode(email: string) {
  return request<{ status: string }>("/v1/auth/request-code", {
    method: "POST",
    body: JSON.stringify({ email }),
  });
}

export async function verifyLogin(email: string, code: string) {
  return request<{ user: User; expires_at: string }>("/v1/auth/verify", {
    method: "POST",
    body: JSON.stringify({ email, code }),
  });
}

export async function logout() {
  return request<void>("/v1/auth/logout", { method: "POST" });
}

export async function listMachines() {
  const body = await request<{ machines: Machine[] }>("/v1/machines");
  return body.machines;
}

export async function createMachine(name: string, snapshotName?: string) {
  return request<Machine>("/v1/machines", {
    method: "POST",
    body: JSON.stringify({ name, snapshot_name: snapshotName ?? "" }),
  });
}

export async function deleteMachine(name: string) {
  return request<void>(`/v1/machines/${encodeURIComponent(name)}`, {
    method: "DELETE",
  });
}

export async function forkMachine(sourceName: string, targetName: string) {
  return request<Machine>(`/v1/machines/${encodeURIComponent(sourceName)}/fork`, {
    method: "POST",
    body: JSON.stringify({ target_name: targetName }),
  });
}

export async function listSnapshots() {
  const body = await request<{ snapshots: Snapshot[] }>("/v1/snapshots");
  return body.snapshots;
}

export async function createSnapshot(machineName: string, snapshotName: string) {
  return request<Snapshot>("/v1/snapshots", {
    method: "POST",
    body: JSON.stringify({ machine_name: machineName, snapshot_name: snapshotName }),
  });
}

export async function deleteSnapshot(name: string) {
  return request<void>(`/v1/snapshots/${encodeURIComponent(name)}`, {
    method: "DELETE",
  });
}

export async function listEnvVars() {
  const body = await request<{ env_vars: { key: string; value: string }[] }>("/v1/env-vars");
  return body.env_vars;
}

export async function setEnvVar(key: string, value: string) {
  return request<EnvVar>("/v1/env-vars", {
    method: "PUT",
    body: JSON.stringify({ key, value }),
  });
}

export async function deleteEnvVar(key: string) {
  return request<void>(`/v1/env-vars/${encodeURIComponent(key)}`, {
    method: "DELETE",
  });
}

export async function getMachineEnv(name: string) {
  return request<MachineEnv>(`/v1/machines/${encodeURIComponent(name)}/env`);
}

export async function getDefaultWorkspace() {
  const body = await request<{ name: string; layout: WorkspaceLayout }>("/v1/workspaces/default");
  return body.layout;
}

export async function saveDefaultWorkspace(layout: WorkspaceLayout) {
  return request<{ name: string; layout: WorkspaceLayout }>("/v1/workspaces/default", {
    method: "PUT",
    body: JSON.stringify({ layout }),
  });
}

export async function createTerminalSession(machineName: string, cols: number, rows: number) {
  return request<TerminalSession>("/v1/terminal/sessions", {
    method: "POST",
    body: JSON.stringify({ machine_name: machineName, cols, rows }),
  });
}

export async function attachTerminalSession(sessionId: string, cols: number, rows: number) {
  return request<TerminalSession>("/v1/terminal/sessions/" + encodeURIComponent(sessionId) + "/attach", {
    method: "POST",
    body: JSON.stringify({ cols, rows }),
  });
}

export async function deleteTerminalSession(sessionId: string) {
  return request<void>("/v1/terminal/sessions/" + encodeURIComponent(sessionId), {
    method: "DELETE",
  });
}

export async function getTerminalGitStatus(sessionId: string, cwd: string) {
  return request<GitRepoStatus>("/v1/terminal/sessions/" + encodeURIComponent(sessionId) + "/git/status", {
    method: "POST",
    body: JSON.stringify({ cwd }),
  });
}

export async function getTerminalGitDiffBatch(sessionId: string, diffRequest: GitDiffBatchRequest) {
  return request<{ diffs: GitFileDiff[] }>("/v1/terminal/sessions/" + encodeURIComponent(sessionId) + "/git/diffs", {
    method: "POST",
    body: JSON.stringify(diffRequest),
  });
}

export function toWebSocketURL(path: string) {
  const url = new URL(path, window.location.origin);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  return url.toString();
}
