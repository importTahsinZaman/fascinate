import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
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

describe("App", () => {
  beforeEach(() => {
    useWorkspaceStore.setState({
      windows: [],
      windowCwds: {},
      viewport: { x: 120, y: 96, scale: 1 },
      hydrated: false,
    });
  });

  afterEach(() => {
    cleanup();
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
        return jsonResponse({ env_vars: [{ key: "FRONTEND_URL", value: "${FASCINATE_PUBLIC_URL}" }] });
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
    expect(screen.getByText("FRONTEND_URL")).toBeTruthy();
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

  it("zooms the workspace on ctrl-wheel over shell content and shell header", async () => {
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
        return jsonResponse({ env_vars: [] });
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

    const terminal = await screen.findByTestId("terminal-m-1");
    fireEvent.wheel(terminal, { ctrlKey: true, deltaY: -100, clientX: 120, clientY: 120 });
    expect(useWorkspaceStore.getState().viewport.scale).toBeGreaterThan(1);

    const zoomedScale = useWorkspaceStore.getState().viewport.scale;
    fireEvent.wheel(screen.getAllByText("m-1 shell")[0], {
      ctrlKey: true,
      deltaY: -100,
      clientX: 140,
      clientY: 48,
    });
    expect(useWorkspaceStore.getState().viewport.scale).toBeGreaterThan(zoomedScale);
  });

  it("pans the workspace on shell header wheel gestures without stealing shell body scroll", async () => {
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
        return jsonResponse({ env_vars: [] });
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

    const terminal = await screen.findByTestId("terminal-m-1");
    fireEvent.wheel(terminal, { deltaX: 20, deltaY: 32, clientX: 120, clientY: 120 });
    expect(useWorkspaceStore.getState().viewport.x).toBe(120);
    expect(useWorkspaceStore.getState().viewport.y).toBe(96);

    fireEvent.wheel(screen.getAllByText("m-1 shell")[0], {
      deltaX: 16,
      deltaY: 24,
      clientX: 140,
      clientY: 48,
    });
    expect(useWorkspaceStore.getState().viewport.x).toBe(104);
    expect(useWorkspaceStore.getState().viewport.y).toBe(72);
  });
});
