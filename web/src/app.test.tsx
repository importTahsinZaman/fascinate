import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter } from "react-router-dom";
import { App } from "./app";
import { useWorkspaceStore } from "./store";

vi.mock("./terminal", () => ({
  TerminalView: ({
    machineName,
    title,
  }: {
    machineName: string;
    title: string;
    onCwdChange?: (cwd: string) => void;
  }) => <div data-testid={`terminal-${machineName}`}>{title}</div>,
}));

function renderApp() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </QueryClientProvider>,
  );
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function rect(width: number, height: number): DOMRect {
  return {
    x: 0,
    y: 0,
    left: 0,
    top: 0,
    width,
    height,
    right: width,
    bottom: height,
    toJSON: () => ({}),
  } as DOMRect;
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

type AppFetchOverride = (path: string, init?: RequestInit) => Response | Promise<Response> | undefined;

const builtinEnvVarsResponse = [
  { key: "FASCINATE_MACHINE_NAME", description: "Name of the current VM." },
  { key: "FASCINATE_PUBLIC_URL", description: "Public HTTPS URL for the current VM, routed to its primary port." },
];

function createAuthenticatedFetchMock(override?: AppFetchOverride) {
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input);
    const overridden = override?.(path, init);
    if (overridden !== undefined) {
      return overridden;
    }
    if (path === "/v1/auth/session") {
      return jsonResponse({ user: { id: "user-1", email: "dev@example.com", is_admin: false } });
    }
    if (path === "/v1/machines") {
      return jsonResponse({
        machines: [
          {
            id: "machine-1",
            name: "m-1",
            state: "RUNNING",
            primary_port: 3000,
            created_at: "2026-03-22T00:00:00Z",
            updated_at: "2026-03-22T00:00:00Z",
          },
        ],
      });
    }
    if (path === "/v1/snapshots") {
      return jsonResponse({ snapshots: [] });
    }
    if (path === "/v1/env-vars") {
      return jsonResponse({ env_vars: [], builtin_env_vars: builtinEnvVarsResponse });
    }
    if (path === "/v1/workspaces/default") {
      if (init?.method === "PUT") {
        const body = JSON.parse(String(init.body)) as { layout: unknown };
        return jsonResponse({ name: "default", layout: body.layout });
      }
      return jsonResponse({ name: "default", layout: { version: 2, windows: [], viewport: { x: 120, y: 96, scale: 1 } } });
    }
    throw new Error(`unexpected request ${path}`);
  });
}

async function seedShellSession(windowIndex: number, sessionId: string, cwd: string) {
  const windowId = useWorkspaceStore.getState().windows[windowIndex].id;
  await act(async () => {
    useWorkspaceStore.getState().setWindowSession(windowId, sessionId);
    useWorkspaceStore.getState().setWindowCwd(windowId, cwd);
  });
}

function getSidebarShellLabels() {
  return screen
    .getAllByRole("button")
    .filter((button) => button.classList.contains("sidebar-shell-focus"))
    .map((button) => {
      const cwd = button.querySelector(".sidebar-shell-cwd")?.textContent?.trim() ?? "";
      const machineName = button.querySelector(".sidebar-shell-machine-name")?.textContent?.trim() ?? "";
      return `${cwd} ${machineName}`.trim();
    });
}

describe("App", () => {
  beforeEach(() => {
    Object.defineProperty(window, "matchMedia", {
      configurable: true,
      value: vi.fn().mockReturnValue({
        matches: false,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        addListener: vi.fn(),
        removeListener: vi.fn(),
        dispatchEvent: vi.fn(),
      }),
    });
    Object.defineProperty(HTMLElement.prototype, "scrollIntoView", {
      configurable: true,
      value: vi.fn(),
    });
    useWorkspaceStore.setState({
      windows: [],
      windowCwds: {},
      viewport: { x: 120, y: 96, scale: 1 },
      viewportFocusRequest: null,
      gitDiffSidebar: { windowID: null, selectedPath: null },
      hydrated: false,
    });
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("requests a login code for anonymous users", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path === "/v1/auth/session") {
        return new Response(JSON.stringify({ error: "authentication required" }), { status: 401 });
      }
      if (path === "/v1/auth/request-code") {
        expect(init?.method).toBe("POST");
        return jsonResponse({ status: "verification code sent" }, 202);
      }
      throw new Error(`unexpected request ${path}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    renderApp();

    expect(await screen.findByText(/Browser command center/i)).toBeTruthy();
    fireEvent.change(screen.getByPlaceholderText("you@example.com"), {
      target: { value: "dev@example.com" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send code" }));

    expect(await screen.findByPlaceholderText("123456")).toBeTruthy();
    expect(fetchMock).toHaveBeenCalledWith(
      "/v1/auth/request-code",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("renders the sidebar workspace, modals, and opens terminal windows", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path === "/v1/auth/session") {
        return jsonResponse({ user: { id: "user-1", email: "dev@example.com", is_admin: false } });
      }
      if (path === "/v1/machines") {
        return jsonResponse({
          machines: [
            {
              id: "machine-1",
              name: "m-1",
              state: "RUNNING",
              primary_port: 3000,
              created_at: "2026-03-22T00:00:00Z",
              updated_at: "2026-03-22T00:00:00Z",
            },
          ],
        });
      }
      if (path === "/v1/snapshots") {
        return jsonResponse({
          snapshots: [
            {
              id: "snapshot-1",
              name: "baseline",
              source_machine_name: "m-1",
              state: "READY",
              created_at: "2026-03-22T00:00:00Z",
              updated_at: "2026-03-22T00:00:00Z",
            },
          ],
        });
      }
      if (path === "/v1/env-vars") {
        return jsonResponse({
          env_vars: [{ key: "OPEN_AI_KEY", value: "sk-proj-1234567890abcdefghij" }],
          builtin_env_vars: builtinEnvVarsResponse,
        });
      }
      if (path === "/v1/workspaces/default") {
        if (init?.method === "PUT") {
          const body = JSON.parse(String(init.body)) as { layout: unknown };
          return jsonResponse({ name: "default", layout: body.layout });
        }
        return jsonResponse({ name: "default", layout: { version: 2, windows: [], viewport: { x: 120, y: 96, scale: 1 } } });
      }
      throw new Error(`unexpected request ${path}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    renderApp();

    expect(await screen.findByRole("heading", { name: "Machines" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "New machine" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Env vars" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Snapshots" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Sign out" })).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "New machine" }));
    expect(await screen.findByRole("dialog", { name: "Create machine" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Close modal" }));

    fireEvent.click(screen.getByRole("button", { name: "Fork m-1" }));
    expect(await screen.findByRole("dialog", { name: "Fork machine" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Close modal" }));

    fireEvent.click(screen.getByRole("button", { name: "Snapshot m-1" }));
    expect(await screen.findByRole("dialog", { name: "Create snapshot" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Close modal" }));

    fireEvent.click(screen.getByRole("button", { name: "Env vars" }));
    expect(await screen.findByRole("dialog", { name: "Environment variables" })).toBeTruthy();
    expect(screen.getByText("Set env vars for every Fascinate VM. Use ${NAME} to reference.")).toBeTruthy();
    expect(screen.getByRole("textbox", { name: "Name" })).toBeTruthy();
    expect(screen.getByText("OPEN_AI_KEY")).toBeTruthy();
    expect(screen.queryByText("sk-proj-1234567890abcdefghij")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Show value for OPEN_AI_KEY" }));
    expect(screen.getByText("sk-proj-1234567890abcdefghij")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Edit OPEN_AI_KEY" }));
    expect((screen.getByPlaceholderText("OPENAI_API_KEY") as HTMLInputElement).value).toBe("");
    expect(screen.getByDisplayValue("OPEN_AI_KEY")).toBeTruthy();
    expect(screen.getByDisplayValue("sk-proj-1234567890abcdefghij")).toBeTruthy();
    expect(screen.getByText("FASCINATE_PUBLIC_URL")).toBeTruthy();
    expect(screen.getByText("Public HTTPS URL for the current VM, routed to its primary port. Example:")).toBeTruthy();
    expect(screen.getByText("https://tic-tac-toe.fascinate.dev")).toBeTruthy();
    expect(screen.queryByText("env")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Close modal" }));

    fireEvent.click(screen.getByRole("button", { name: "Snapshots" }));
    expect(await screen.findByRole("dialog", { name: "Snapshots" })).toBeTruthy();
    expect(screen.getByText("baseline")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Close modal" }));

    fireEvent.click(screen.getByRole("button", { name: "New shell" }));

    expect(await screen.findByTestId("terminal-m-1")).toBeTruthy();
    expect(await screen.findByRole("button", { name: "Shell 1" })).toBeTruthy();

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/v1/workspaces/default",
        expect.objectContaining({ method: "PUT" }),
      );
    });
  });

  it("reveals new shells in the horizontal strip without changing viewport state", async () => {
    const fetchMock = createAuthenticatedFetchMock();
    vi.stubGlobal("fetch", fetchMock);

    renderApp();

    fireEvent.click(await screen.findByRole("button", { name: "New shell" }));

    expect(await screen.findByTestId("terminal-m-1")).toBeTruthy();
    await waitFor(() => {
      expect(HTMLElement.prototype.scrollIntoView).toHaveBeenCalled();
    });
    expect(useWorkspaceStore.getState().viewport).toEqual({ x: 120, y: 96, scale: 1 });
    expect(useWorkspaceStore.getState().viewportFocusRequest).toBeNull();
  });

  it("shows newly created machines as creating until the backend reports them running", async () => {
    let machines = [] as Array<{
      id: string;
      name: string;
      state: string;
      primary_port: number;
      created_at: string;
      updated_at: string;
    }>;

    const fetchMock = createAuthenticatedFetchMock((path, init) => {
      if (path === "/v1/machines" && !init?.method) {
        return jsonResponse({ machines });
      }
      if (path === "/v1/machines" && init?.method === "POST") {
        const machine = {
          id: "machine-fresh-box",
          name: "fresh-box",
          state: "CREATING",
          primary_port: 3000,
          created_at: "2026-04-06T00:00:00Z",
          updated_at: "2026-04-06T00:00:00Z",
        };
        machines = [machine];
        return jsonResponse(machine, 202);
      }
      return undefined;
    });
    vi.stubGlobal("fetch", fetchMock);

    renderApp();

    fireEvent.click(await screen.findByRole("button", { name: "New machine" }));
    fireEvent.change(screen.getByRole("textbox", { name: "Name" }), {
      target: { value: "fresh-box" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create machine" }));

    expect(await screen.findByText("fresh-box")).toBeTruthy();
    expect(await screen.findByLabelText("creating fresh-box")).toBeTruthy();

    const machineCard = screen.getByText("fresh-box").closest(".machine-card");
    expect(machineCard?.getAttribute("aria-busy")).toBe("true");
    expect(screen.queryByRole("button", { name: "New shell" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Fork fresh-box" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Snapshot fresh-box" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Delete fresh-box" })).toBeNull();
  });

  it("shows only delete for stopped machines", async () => {
    const fetchMock = createAuthenticatedFetchMock((path, init) => {
      if (path === "/v1/machines" && !init?.method) {
        return jsonResponse({
          machines: [
            {
              id: "machine-1",
              name: "m-1",
              state: "STOPPED",
              primary_port: 3000,
              created_at: "2026-03-22T00:00:00Z",
              updated_at: "2026-03-22T00:00:00Z",
            },
          ],
        });
      }
      return undefined;
    });
    vi.stubGlobal("fetch", fetchMock);

    renderApp();

    expect(await screen.findByRole("button", { name: "Delete m-1" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Start" })).toBeNull();
    expect(screen.getByRole("button", { name: "Delete m-1" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "New shell" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Stop" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Fork m-1" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Snapshot m-1" })).toBeNull();
  });

  it("reveals a shell from the sidebar within the horizontal strip", async () => {
    const fetchMock = createAuthenticatedFetchMock();
    vi.stubGlobal("fetch", fetchMock);

    renderApp();

    fireEvent.click(await screen.findByRole("button", { name: "New shell" }));
    expect(await screen.findByTestId("terminal-m-1")).toBeTruthy();

    fireEvent.click(await screen.findByRole("button", { name: "Shell 1" }));

    await waitFor(() => {
      expect(HTMLElement.prototype.scrollIntoView).toHaveBeenCalled();
    });
    expect(useWorkspaceStore.getState().viewport).toEqual({ x: 120, y: 96, scale: 1 });
    expect(useWorkspaceStore.getState().viewportFocusRequest).toBeNull();
  });

  it("reorders shells by dragging the window header", async () => {
    const fetchMock = createAuthenticatedFetchMock();
    vi.stubGlobal("fetch", fetchMock);
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
      if (this.classList.contains("workspace-strip-item")) {
        const orderedIds = useWorkspaceStore.getState().windows.map((window) => window.id);
        const index = orderedIds.indexOf(this.dataset.windowId ?? "");
        const left = Math.max(0, index) * 400;
        return {
          ...rect(400, 900),
          x: left,
          left,
          right: left + 400,
        } as DOMRect;
      }
      return rect(240, 56);
    });

    renderApp();

    fireEvent.click(await screen.findByRole("button", { name: "New shell" }));
    fireEvent.click(await screen.findByRole("button", { name: "New shell" }));

    await seedShellSession(0, "term-1", "/home/ubuntu/repo-a");
    await seedShellSession(1, "term-2", "/home/ubuntu/repo-b");

    expect(useWorkspaceStore.getState().windows.map((window) => window.title)).toEqual(["m-1 shell", "m-1 shell 2"]);

    const headers = Array.from(document.querySelectorAll(".window-header")) as HTMLElement[];
    fireEvent.pointerDown(headers[1], { button: 0, pointerId: 1, clientX: 460 });
    fireEvent.pointerMove(headers[1], { pointerId: 1, clientX: 40 });
    fireEvent.pointerUp(headers[1], { pointerId: 1, clientX: 40 });

    expect(useWorkspaceStore.getState().windows.map((window) => window.title)).toEqual(["m-1 shell 2", "m-1 shell"]);
    expect(getSidebarShellLabels()).toEqual(["~/repo-b m-1", "~/repo-a m-1"]);
  });

  it("reorders shells by dragging the sidebar shell list", async () => {
    const fetchMock = createAuthenticatedFetchMock();
    vi.stubGlobal("fetch", fetchMock);
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
      if (this.classList.contains("sidebar-shell-row")) {
        const orderedIds = useWorkspaceStore.getState().windows.map((window) => window.id);
        const index = orderedIds.indexOf(this.dataset.windowId ?? "");
        const top = Math.max(0, index) * 48;
        return {
          ...rect(320, 40),
          y: top,
          top,
          bottom: top + 40,
        } as DOMRect;
      }
      return rect(240, 56);
    });

    renderApp();

    fireEvent.click(await screen.findByRole("button", { name: "New shell" }));
    fireEvent.click(await screen.findByRole("button", { name: "New shell" }));

    await seedShellSession(0, "term-1", "/home/ubuntu/repo-a");
    await seedShellSession(1, "term-2", "/home/ubuntu/repo-b");

    let shellButtons = screen
      .getAllByRole("button")
      .filter((button) => button.classList.contains("sidebar-shell-focus"));
    fireEvent.pointerDown(shellButtons[1], { button: 0, pointerId: 1, clientY: 64 });
    fireEvent.pointerMove(shellButtons[1], { pointerId: 1, clientY: 8 });
    fireEvent.pointerUp(shellButtons[1], { pointerId: 1, clientY: 8 });

    expect(useWorkspaceStore.getState().windows.map((window) => window.title)).toEqual(["m-1 shell 2", "m-1 shell"]);
    expect(getSidebarShellLabels()).toEqual(["~/repo-b m-1", "~/repo-a m-1"]);
  });

  it("keeps a shell visible when backend close fails", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path === "/v1/auth/session") {
        return jsonResponse({ user: { id: "user-1", email: "dev@example.com", is_admin: false } });
      }
      if (path === "/v1/machines") {
        return jsonResponse({
          machines: [
            {
              id: "machine-1",
              name: "m-1",
              state: "RUNNING",
              primary_port: 3000,
              created_at: "2026-03-22T00:00:00Z",
              updated_at: "2026-03-22T00:00:00Z",
            },
          ],
        });
      }
      if (path === "/v1/snapshots") {
        return jsonResponse({ snapshots: [] });
      }
      if (path === "/v1/env-vars") {
        return jsonResponse({ env_vars: [], builtin_env_vars: builtinEnvVarsResponse });
      }
      if (path === "/v1/workspaces/default") {
        if (init?.method === "PUT") {
          const body = JSON.parse(String(init.body)) as { layout: unknown };
          return jsonResponse({ name: "default", layout: body.layout });
        }
        return jsonResponse({ name: "default", layout: { version: 2, windows: [], viewport: { x: 120, y: 96, scale: 1 } } });
      }
      if (path === "/v1/terminal/sessions/term-1") {
        return jsonResponse({ error: "close failed" }, 500);
      }
      throw new Error(`unexpected request ${path}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    renderApp();

    fireEvent.click(await screen.findByRole("button", { name: "New shell" }));
    expect(await screen.findByTestId("terminal-m-1")).toBeTruthy();

    const windowId = useWorkspaceStore.getState().windows[0].id;
    await act(async () => {
      useWorkspaceStore.getState().setWindowSession(windowId, "term-1");
    });

    fireEvent.click(screen.getByRole("button", { name: "Delete Shell 1" }));

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/v1/terminal/sessions/term-1",
        expect.objectContaining({ method: "DELETE" }),
      );
    });
    expect(await screen.findByText("close failed")).toBeTruthy();
    expect(screen.getByTestId("terminal-m-1")).toBeTruthy();
    expect(useWorkspaceStore.getState().windows).toHaveLength(1);
  });

  it("optimistically closes machine shell windows and disables machine actions while delete is pending", async () => {
    let machineDeleted = false;
    const deleteRequest = deferred<Response>();
    const fetchMock = createAuthenticatedFetchMock((path, init) => {
      if (path === "/v1/machines") {
        return jsonResponse({
          machines: machineDeleted
            ? []
            : [
                {
                  id: "machine-1",
                  name: "m-1",
                  state: "RUNNING",
                  primary_port: 3000,
                  created_at: "2026-03-22T00:00:00Z",
                  updated_at: "2026-03-22T00:00:00Z",
                },
              ],
        });
      }
      if (path === "/v1/machines/m-1" && init?.method === "DELETE") {
        return deleteRequest.promise;
      }
      if (path.startsWith("/v1/terminal/sessions/")) {
        return jsonResponse({ error: "terminal sessions should be cleaned up by machine delete" }, 500);
      }
      return undefined;
    });
    vi.stubGlobal("fetch", fetchMock);

    renderApp();

    fireEvent.click(await screen.findByRole("button", { name: "New shell" }));
    fireEvent.click(await screen.findByRole("button", { name: "New shell" }));

    expect(await screen.findAllByTestId("terminal-m-1")).toHaveLength(2);

    fireEvent.click(screen.getByRole("button", { name: "Delete m-1" }));

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/v1/machines/m-1",
        expect.objectContaining({ method: "DELETE" }),
      );
    });
    await waitFor(() => expect(useWorkspaceStore.getState().windows).toHaveLength(0));

    expect(screen.queryByTestId("terminal-m-1")).toBeNull();
    expect(screen.queryByRole("button", { name: "New shell" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Fork m-1" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Snapshot m-1" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Delete m-1" })).toBeNull();
    expect(screen.getByLabelText("deleting m-1")).toBeTruthy();
    expect(
      fetchMock.mock.calls.filter(([path]) => String(path).startsWith("/v1/terminal/sessions/")),
    ).toHaveLength(0);

    machineDeleted = true;
    deleteRequest.resolve(new Response(null, { status: 204 }));

    await waitFor(() => {
      expect(screen.queryByRole("button", { name: "Fork m-1" })).toBeNull();
    });
  });

  it("restores machine shell windows if delete fails after optimistic removal", async () => {
    const deleteRequest = deferred<Response>();
    const fetchMock = createAuthenticatedFetchMock((path, init) => {
      if (path === "/v1/machines/m-1" && init?.method === "DELETE") {
        return deleteRequest.promise;
      }
      return undefined;
    });
    vi.stubGlobal("fetch", fetchMock);

    renderApp();

    fireEvent.click(await screen.findByRole("button", { name: "New shell" }));
    fireEvent.click(await screen.findByRole("button", { name: "New shell" }));

    expect(await screen.findAllByTestId("terminal-m-1")).toHaveLength(2);

    fireEvent.click(screen.getByRole("button", { name: "Delete m-1" }));

    await waitFor(() => expect(useWorkspaceStore.getState().windows).toHaveLength(0));
    expect(screen.queryByTestId("terminal-m-1")).toBeNull();

    deleteRequest.resolve(jsonResponse({ error: "delete failed" }, 500));

    expect(await screen.findByText("delete failed")).toBeTruthy();
    expect(await screen.findAllByTestId("terminal-m-1")).toHaveLength(2);
    expect((screen.getByRole("button", { name: "New shell" }) as HTMLButtonElement).disabled).toBe(false);
    expect((screen.getByRole("button", { name: "Fork m-1" }) as HTMLButtonElement).disabled).toBe(false);
    expect((screen.getByRole("button", { name: "Snapshot m-1" }) as HTMLButtonElement).disabled).toBe(false);
    expect((screen.getByRole("button", { name: "Delete m-1" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("keeps sidebar shell order stable when focusing a sibling shell", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input);
      if (path === "/v1/auth/session") {
        return jsonResponse({ user: { id: "user-1", email: "dev@example.com", is_admin: false } });
      }
      if (path === "/v1/machines") {
        return jsonResponse({
          machines: [
            {
              id: "machine-1",
              name: "m-1",
              state: "RUNNING",
              primary_port: 3000,
              created_at: "2026-03-22T00:00:00Z",
              updated_at: "2026-03-22T00:00:00Z",
            },
          ],
        });
      }
      if (path === "/v1/snapshots") {
        return jsonResponse({ snapshots: [] });
      }
      if (path === "/v1/env-vars") {
        return jsonResponse({ env_vars: [], builtin_env_vars: builtinEnvVarsResponse });
      }
      if (path === "/v1/workspaces/default") {
        if (init?.method === "PUT") {
          const body = JSON.parse(String(init.body)) as { layout: unknown };
          return jsonResponse({ name: "default", layout: body.layout });
        }
        return jsonResponse({ name: "default", layout: { version: 2, windows: [], viewport: { x: 120, y: 96, scale: 1 } } });
      }
      throw new Error(`unexpected request ${path}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    renderApp();

    fireEvent.click(await screen.findByRole("button", { name: "New shell" }));
    fireEvent.click(await screen.findByRole("button", { name: "New shell" }));

    expect(await screen.findAllByTestId("terminal-m-1")).toHaveLength(2);

    expect(getSidebarShellLabels()).toEqual(["Shell 1 m-1", "Shell 2 m-1"]);

    fireEvent.click(screen.getByRole("button", { name: "Shell 1" }));

    await waitFor(() => {
      expect(HTMLElement.prototype.scrollIntoView).toHaveBeenCalled();
    });

    expect(getSidebarShellLabels()).toEqual(["Shell 1 m-1", "Shell 2 m-1"]);
  });

  it("streams unified file diffs inline and keeps wheel scroll inside the panel", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(globalThis.navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    const fetchMock = createAuthenticatedFetchMock((path, init) => {
      if (path === "/v1/terminal/sessions/term-1/git/status") {
        expect(JSON.parse(String(init?.body))).toEqual({ cwd: "/home/ubuntu/repo" });
        return jsonResponse({
          state: "ready",
          repo_root: "/home/ubuntu/repo",
          branch: "main",
          additions: 3,
          deletions: 2,
          files: [
            { path: "web/src/app.tsx", kind: "modified", index_status: "M", worktree_status: "M" },
            { path: "web/src/store.ts", kind: "modified", index_status: "M", worktree_status: "M" },
            { path: "README.md", kind: "modified", index_status: "M", worktree_status: "M" },
          ],
        });
      }
      if (path === "/v1/terminal/sessions/term-1/git/diffs") {
        const body = JSON.parse(String(init?.body)) as { files: Array<{ path: string }> };
        expect(body).toMatchObject({
          cwd: "/home/ubuntu/repo",
          repo_root: "/home/ubuntu/repo",
        });
        return jsonResponse({
          diffs: body.files.map((file) => {
            if (file.path === "web/src/app.tsx") {
              return {
                state: "ready",
                path: "web/src/app.tsx",
                additions: 1,
                deletions: 1,
                patch: `diff --git a/web/src/app.tsx b/web/src/app.tsx
index 1111111..2222222 100644
--- a/web/src/app.tsx
+++ b/web/src/app.tsx
@@ -1,20 +1,20 @@
 line 1
 line 2
 line 3
 line 4
 line 5
 line 6
-old alpha
+new alpha
 line 8
 line 9
 line 10
 line 11
 line 12
 line 13
 line 14
 line 15
-old omega
+new omega
line 17
line 18
line 19
line 20
`,
              };
            }
            if (file.path === "web/src/store.ts") {
              return {
                state: "ready",
                path: "web/src/store.ts",
                additions: 1,
                deletions: 1,
                patch: `diff --git a/web/src/store.ts b/web/src/store.ts
@@ -1,2 +1,2 @@
-old store
+new store
 unchanged
`,
              };
            }
            if (file.path === "README.md") {
              return {
                state: "ready",
                path: "README.md",
                additions: 1,
                deletions: 0,
                patch: `diff --git a/README.md b/README.md
@@ -1,0 +1 @@
+stacked diff panel
`,
              };
            }
            throw new Error(`unexpected diff request for ${file.path}`);
          }),
        });
      }
      return undefined;
    });
    vi.stubGlobal("fetch", fetchMock);

    renderApp();

    fireEvent.click(await screen.findByRole("button", { name: "New shell" }));
    expect(await screen.findByTestId("terminal-m-1")).toBeTruthy();

    await seedShellSession(0, "term-1", "/home/ubuntu/repo");
    const diffButton = await screen.findByRole("button", { name: "Open git diff for m-1 shell" });
    expect(diffButton.getAttribute("data-active")).toBe("false");
    expect(diffButton.textContent).toContain("+3");
    expect(diffButton.textContent).toContain("-2");
    fireEvent.click(diffButton);
    await waitFor(() => {
      expect(diffButton.getAttribute("data-active")).toBe("true");
    });

    expect(await screen.findByRole("heading", { name: "m-1" })).toBeTruthy();
    expect(screen.queryByText(/^Git Diff$/)).toBeNull();
    const branchLabel = await screen.findByText("main");
    expect(branchLabel.closest(".git-diff-sidebar-header-branch")?.querySelector("svg")).toBeTruthy();
    expect(await screen.findByText("3 files changed")).toBeTruthy();
    await waitFor(() => {
      const totals = document.querySelector(".git-diff-sidebar-header-totals");
      expect(totals?.textContent).toContain("+3");
      expect(totals?.textContent).toContain("-2");
    });
    expect(screen.getByRole("button", { name: "Refresh diff" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Close diff sidebar" })).toBeTruthy();
    expect((await screen.findAllByText("web/src/app.tsx")).length).toBeGreaterThan(0);
    expect((await screen.findAllByText("web/src/store.ts")).length).toBeGreaterThan(0);
    await waitFor(() => {
      const diffRequests = fetchMock.mock.calls
        .filter(([path]) => path === "/v1/terminal/sessions/term-1/git/diffs")
        .flatMap(([, init]) =>
          JSON.parse(String((init as RequestInit | undefined)?.body)).files.map((file: { path: string }) => file.path),
        );
      expect(diffRequests).toContain("README.md");
    });
    expect(screen.queryByText("Diff queued")).toBeNull();
    const copyButton = screen.getByRole("button", { name: "Copy path web/src/app.tsx" });
    fireEvent.click(copyButton);
    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith("web/src/app.tsx");
    });
    expect(screen.getByRole("button", { name: "Copied path web/src/app.tsx" })).toBeTruthy();
    await waitFor(() => {
      const codeNodes = Array.from(document.querySelectorAll(".git-diff-unified-code code"));
      expect(codeNodes.some((node) => node.textContent === "new alpha")).toBe(true);
      expect(codeNodes.some((node) => node.textContent === "new store")).toBe(true);
    });
    expect(document.querySelector(".git-diff-token-inline-add")).toBeNull();
    expect(screen.getByText("All 2 lines")).toBeTruthy();
    fireEvent.click(screen.getByText("All 2 lines"));
    expect((await screen.findAllByText("line 11")).length).toBeGreaterThan(0);

    expect(await screen.findByText("stacked diff panel")).toBeTruthy();

    const stream = document.querySelector(".git-diff-stream") as HTMLElement | null;
    expect(stream).toBeTruthy();
    const scrollContainer = stream!;
    fireEvent.wheel(scrollContainer, { deltaY: 120, clientX: 640, clientY: 240 });
    expect(useWorkspaceStore.getState().viewport).toMatchObject({ x: 120, y: 96, scale: 1 });
    Object.defineProperty(scrollContainer, "clientHeight", { configurable: true, value: 640 });
    Object.defineProperty(scrollContainer, "scrollHeight", { configurable: true, value: 1800 });
    Object.defineProperty(scrollContainer, "scrollTop", { configurable: true, value: 1320 });
    fireEvent.scroll(scrollContainer);

    await waitFor(() => {
      expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
    });
  });

  it("renders split collapsed controls for large unchanged regions", async () => {
    const fetchMock = createAuthenticatedFetchMock((path) => {
      if (path === "/v1/terminal/sessions/term-1/git/status") {
        return jsonResponse({
          state: "ready",
          repo_root: "/home/ubuntu/repo",
          branch: "main",
          additions: 1,
          deletions: 1,
          files: [{ path: "web/src/app.tsx", kind: "modified", index_status: "M", worktree_status: "M" }],
        });
      }
      if (path === "/v1/terminal/sessions/term-1/git/diffs") {
        return jsonResponse({
          diffs: [{
            state: "ready",
            path: "web/src/app.tsx",
            additions: 1,
            deletions: 1,
            patch: `diff --git a/web/src/app.tsx b/web/src/app.tsx
index 1111111..2222222 100644
--- a/web/src/app.tsx
+++ b/web/src/app.tsx
@@ -1,26 +1,26 @@
line 1
 line 2
 line 3
-old alpha
+new alpha
 line 5
 line 6
 line 7
 line 8
 line 9
 line 10
 line 11
 line 12
 line 13
 line 14
 line 15
 line 16
 line 17
 line 18
 line 19
 line 20
 line 21
 line 22
 line 23
-old omega
+new omega
 line 25
 line 26
`,
          }],
        });
      }
      return undefined;
    });
    vi.stubGlobal("fetch", fetchMock);

    renderApp();

    fireEvent.click(await screen.findByRole("button", { name: "New shell" }));
    expect(await screen.findByTestId("terminal-m-1")).toBeTruthy();

    await seedShellSession(0, "term-1", "/home/ubuntu/repo");
    fireEvent.click(await screen.findByRole("button", { name: "Open git diff for m-1 shell" }));

    expect(await screen.findByRole("heading", { name: "m-1" })).toBeTruthy();
    expect(await screen.findByText("All 13 lines")).toBeTruthy();
    expect((await screen.findAllByText("5 lines")).length).toBe(2);
    const collapsedRow = document.querySelector(".git-diff-collapsed");
    expect(collapsedRow?.parentElement?.classList.contains("git-diff-unified")).toBe(true);
  });

  it("rebinds the git diff sidebar to another shell window", async () => {
    const fetchMock = createAuthenticatedFetchMock((path) => {
      if (path === "/v1/terminal/sessions/term-1/git/status") {
        return jsonResponse({
          state: "ready",
          repo_root: "/home/ubuntu/repo-one",
          branch: "main",
          additions: 1,
          deletions: 1,
          files: [{ path: "web/src/app.tsx", kind: "modified" }],
        });
      }
      if (path === "/v1/terminal/sessions/term-1/git/diffs") {
        return jsonResponse({
          diffs: [{
            state: "ready",
            path: "web/src/app.tsx",
            additions: 1,
            deletions: 1,
            patch: `diff --git a/web/src/app.tsx b/web/src/app.tsx
@@ -1 +1 @@
-first
+second
`,
          }],
        });
      }
      if (path === "/v1/terminal/sessions/term-2/git/status") {
        return jsonResponse({
          state: "ready",
          repo_root: "/home/ubuntu/repo-two",
          branch: "feature",
          additions: 1,
          deletions: 0,
          files: [{ path: "web/src/store.ts", kind: "modified" }],
        });
      }
      if (path === "/v1/terminal/sessions/term-2/git/diffs") {
        return jsonResponse({
          diffs: [{
            state: "ready",
            path: "web/src/store.ts",
            additions: 1,
            deletions: 0,
            patch: `diff --git a/web/src/store.ts b/web/src/store.ts
@@ -1,0 +1 @@
+store state
`,
          }],
        });
      }
      return undefined;
    });
    vi.stubGlobal("fetch", fetchMock);

    renderApp();

    fireEvent.click(await screen.findByRole("button", { name: "New shell" }));
    fireEvent.click(await screen.findByRole("button", { name: "New shell" }));

    await seedShellSession(0, "term-1", "/home/ubuntu/repo-one");
    await seedShellSession(1, "term-2", "/home/ubuntu/repo-two");

    fireEvent.click(await screen.findByRole("button", { name: "Open git diff for m-1 shell" }));
    expect((await screen.findAllByText("web/src/app.tsx")).length).toBeGreaterThan(0);

    fireEvent.click(await screen.findByRole("button", { name: "Open git diff for m-1 shell 2" }));

    expect(await screen.findByRole("heading", { name: "m-1" })).toBeTruthy();
    expect(await screen.findByText("feature")).toBeTruthy();
    expect(await screen.findByText("1 file changed")).toBeTruthy();
    await waitFor(() => {
      const totals = document.querySelector(".git-diff-sidebar-header-totals");
      expect(totals?.textContent).toContain("+1");
      expect(totals?.textContent).toContain("-0");
    });
    expect((await screen.findAllByText("web/src/store.ts")).length).toBeGreaterThan(0);
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/v1/terminal/sessions/term-2/git/status",
        expect.objectContaining({ method: "POST" }),
      );
    });
  });

  it("hides the shell-header git diff chip outside git repositories", async () => {
    const fetchMock = createAuthenticatedFetchMock((path) => {
      if (path === "/v1/terminal/sessions/term-1/git/status") {
        return jsonResponse({ state: "not_repo" });
      }
      return undefined;
    });
    vi.stubGlobal("fetch", fetchMock);

    renderApp();

    fireEvent.click(await screen.findByRole("button", { name: "New shell" }));
    await seedShellSession(0, "term-1", "/home/ubuntu");

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/v1/terminal/sessions/term-1/git/status",
        expect.objectContaining({ method: "POST" }),
      );
    });
    expect(screen.queryByRole("button", { name: "Open git diff for m-1 shell" })).toBeNull();
  });

  it("shows the shell-header git diff chip for clean repositories", async () => {
    const fetchMock = createAuthenticatedFetchMock((path) => {
      if (path === "/v1/terminal/sessions/term-1/git/status") {
        return jsonResponse({
          state: "ready",
          repo_root: "/home/ubuntu/repo",
          branch: "main",
          additions: 0,
          deletions: 0,
          files: [],
        });
      }
      return undefined;
    });
    vi.stubGlobal("fetch", fetchMock);

    renderApp();

    fireEvent.click(await screen.findByRole("button", { name: "New shell" }));
    await seedShellSession(0, "term-1", "/home/ubuntu/repo");

    const diffButton = await screen.findByRole("button", { name: "Open git diff for m-1 shell" });
    expect(diffButton.getAttribute("data-active")).toBe("false");
    expect(diffButton.textContent).toContain("+0");
    expect(diffButton.textContent).toContain("-0");

    fireEvent.click(diffButton);
    await waitFor(() => {
      expect(diffButton.getAttribute("data-active")).toBe("true");
    });
    expect(await screen.findByText("Working tree is clean")).toBeTruthy();
    expect(await screen.findByText("This repository has no changed files right now.")).toBeTruthy();
    expect(await screen.findByText("Changes from this shell appear here automatically.")).toBeTruthy();
  });

  it("closes the git diff sidebar on repeat toggle press and Escape", async () => {
    const fetchMock = createAuthenticatedFetchMock((path) => {
      if (path === "/v1/terminal/sessions/term-1/git/status") {
        return jsonResponse({
          state: "ready",
          repo_root: "/home/ubuntu/repo",
          branch: "main",
          additions: 0,
          deletions: 0,
          files: [],
        });
      }
      return undefined;
    });
    vi.stubGlobal("fetch", fetchMock);

    renderApp();

    fireEvent.click(await screen.findByRole("button", { name: "New shell" }));
    await seedShellSession(0, "term-1", "/home/ubuntu/repo");

    const diffButton = await screen.findByRole("button", { name: "Open git diff for m-1 shell" });

    fireEvent.click(diffButton);
    expect(await screen.findByText("Working tree is clean")).toBeTruthy();
    await waitFor(() => {
      expect(diffButton.getAttribute("data-active")).toBe("true");
    });

    fireEvent.click(diffButton);
    await waitFor(() => {
      expect(diffButton.getAttribute("data-active")).toBe("false");
      expect(screen.queryByText("Working tree is clean")).toBeNull();
    });

    fireEvent.click(diffButton);
    expect(await screen.findByText("Working tree is clean")).toBeTruthy();
    await waitFor(() => {
      expect(diffButton.getAttribute("data-active")).toBe("true");
    });

    fireEvent.keyDown(window, { key: "Escape" });
    await waitFor(() => {
      expect(diffButton.getAttribute("data-active")).toBe("false");
      expect(screen.queryByText("Working tree is clean")).toBeNull();
    });
  });

  it("shows an explicit fallback when a file diff cannot be rendered inline", async () => {
    const fetchMock = createAuthenticatedFetchMock((path) => {
      if (path === "/v1/terminal/sessions/term-1/git/status") {
        return jsonResponse({
          state: "ready",
          repo_root: "/home/ubuntu/repo",
          branch: "main",
          additions: 0,
          deletions: 0,
          files: [{ path: "web/src/app.tsx", kind: "modified" }],
        });
      }
      if (path === "/v1/terminal/sessions/term-1/git/diffs") {
        return jsonResponse({
          diffs: [{
            state: "too_large",
            path: "web/src/app.tsx",
            message: "Patch exceeds the inline rendering limit.",
          }],
        });
      }
      return undefined;
    });
    vi.stubGlobal("fetch", fetchMock);

    renderApp();

    fireEvent.click(await screen.findByRole("button", { name: "New shell" }));
    await seedShellSession(0, "term-1", "/home/ubuntu/repo");

    fireEvent.click(await screen.findByRole("button", { name: "Open git diff for m-1 shell" }));

    expect(await screen.findByText("Inline diff unavailable")).toBeTruthy();
    expect(screen.getByText("Patch exceeds the inline rendering limit.")).toBeTruthy();
  });
});
