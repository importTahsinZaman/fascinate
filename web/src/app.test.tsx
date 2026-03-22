import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter } from "react-router-dom";
import { App } from "./app";
import { useWorkspaceStore } from "./store";

vi.mock("./terminal", () => ({
  TerminalView: ({ machineName, title }: { machineName: string; title: string }) => (
    <div data-testid={`terminal-${machineName}`}>{title}</div>
  ),
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

  it("renders the toolbelt workspace and opens terminal windows", async () => {
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

    expect(await screen.findByRole("button", { name: "Shells" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Machines" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Snapshots" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Env Vars" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Log out" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Reset view" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Shells" }));
    expect(await screen.findByText("Open a shell")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Open shell" }));

    expect(await screen.findByTestId("terminal-m-1")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Snapshots" }));
    expect(await screen.findByText("Saved snapshots")).toBeTruthy();
    expect(screen.getAllByText("baseline").length).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole("button", { name: "Env Vars" }));
    expect(await screen.findByText("FRONTEND_URL")).toBeTruthy();

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        "/v1/workspaces/default",
        expect.objectContaining({ method: "PUT" }),
      );
    });
  });
});
