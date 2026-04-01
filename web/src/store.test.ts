import { beforeEach, describe, expect, it } from "vitest";
import { useWorkspaceStore } from "./store";

function overlaps(
  first: { x: number; y: number; width: number; height: number },
  second: { x: number; y: number; width: number; height: number },
) {
  return (
    first.x < second.x + second.width &&
    first.x + first.width > second.x &&
    first.y < second.y + second.height &&
    first.y + first.height > second.y
  );
}

describe("workspace store", () => {
  beforeEach(() => {
    useWorkspaceStore.setState({
      windows: [],
      windowCwds: {},
      viewportFocusRequest: null,
      gitDiffSidebar: { windowID: null, selectedPath: null },
      hydrated: false,
    });
  });

  it("opens terminals as distinct ordered shells", () => {
    useWorkspaceStore.getState().openTerminal("m-1");
    useWorkspaceStore.getState().openTerminal("m-1");

    const { windows } = useWorkspaceStore.getState();
    expect(windows).toHaveLength(2);
    expect(windows[0].id).not.toEqual(windows[1].id);
    expect(overlaps(windows[0], windows[1])).toBe(false);
    expect(windows[1].x).toBeGreaterThan(windows[0].x);
    expect(windows[1].y).toBe(windows[0].y);
  });

  it("hydrates only once", () => {
    useWorkspaceStore.getState().hydrate({
      version: 1,
      windows: [
        {
          id: "a",
          machineName: "m-1",
          title: "m-1 shell",
          sessionId: "term-1",
          x: 1,
          y: 2,
          width: 500,
          height: 300,
          z: 1,
        },
      ],
    });
    useWorkspaceStore.getState().hydrate({ version: 1, windows: [] });

    expect(useWorkspaceStore.getState().windows).toHaveLength(1);
    expect(useWorkspaceStore.getState().windows[0].sessionId).toBe("term-1");
    expect(useWorkspaceStore.getState().windows[0].width).toBe(796);
    expect(useWorkspaceStore.getState().windows[0].height).toBe(900);
    expect(useWorkspaceStore.getState().viewport).toEqual({ x: 120, y: 96, scale: 1 });
  });

  it("collapses legacy freeform layouts into left-to-right shell order", () => {
    useWorkspaceStore.getState().hydrate({
      version: 2,
      windows: [
        {
          id: "b",
          machineName: "m-2",
          title: "m-2 shell",
          sessionId: "term-2",
          x: 980,
          y: 280,
          width: 600,
          height: 400,
          z: 2,
        },
        {
          id: "a",
          machineName: "m-1",
          title: "m-1 shell",
          sessionId: "term-1",
          x: 120,
          y: 40,
          width: 600,
          height: 400,
          z: 1,
        },
      ],
    });

    const { windows } = useWorkspaceStore.getState();
    expect(windows.map((window) => window.id)).toEqual(["a", "b"]);
    expect(windows[0].x).toBe(0);
    expect(windows[1].x).toBeGreaterThan(windows[0].x);
    expect(windows.every((window) => window.y === 0)).toBe(true);
  });

  it("stores terminal session ids on windows", () => {
    useWorkspaceStore.getState().openTerminal("m-1");
    const windowId = useWorkspaceStore.getState().windows[0].id;

    useWorkspaceStore.getState().setWindowSession(windowId, "term-1");

    expect(useWorkspaceStore.getState().windows[0].sessionId).toBe("term-1");
  });

  it("stores cwd metadata separately from persisted window layout", () => {
    useWorkspaceStore.getState().openTerminal("m-1");
    const windowId = useWorkspaceStore.getState().windows[0].id;

    useWorkspaceStore.getState().setWindowCwd(windowId, "/home/ubuntu/space-shooter");

    expect(useWorkspaceStore.getState().windowCwds[windowId]).toBe("/home/ubuntu/space-shooter");
    expect(useWorkspaceStore.getState().serialize().windows[0]).not.toHaveProperty("cwd");
  });

  it("clears cwd metadata when a window closes", () => {
    useWorkspaceStore.getState().openTerminal("m-1");
    const windowId = useWorkspaceStore.getState().windows[0].id;

    useWorkspaceStore.getState().setWindowCwd(windowId, "/home/ubuntu/space-shooter");
    useWorkspaceStore.getState().closeWindow(windowId);

    expect(useWorkspaceStore.getState().windowCwds[windowId]).toBeUndefined();
  });

  it("keeps git diff sidebar state out of persisted layouts and clears it when the shell closes", () => {
    useWorkspaceStore.getState().openTerminal("m-1");
    const windowId = useWorkspaceStore.getState().windows[0].id;

    useWorkspaceStore.getState().openGitDiffSidebar(windowId);
    useWorkspaceStore.getState().selectGitDiffSidebarFile("web/src/app.tsx");

    expect(useWorkspaceStore.getState().gitDiffSidebar).toEqual({
      windowID: windowId,
      selectedPath: "web/src/app.tsx",
      selectedPreviousPath: undefined,
    });
    expect(useWorkspaceStore.getState().serialize()).not.toHaveProperty("gitDiffSidebar");

    useWorkspaceStore.getState().closeWindow(windowId);

    expect(useWorkspaceStore.getState().gitDiffSidebar).toEqual({ windowID: null, selectedPath: null });
  });

  it("persists compatibility viewport state separately from shell order", () => {
    useWorkspaceStore.getState().hydrate({
      version: 2,
      windows: [],
      viewport: { x: 320, y: 240, scale: 1.3 },
    });

    expect(useWorkspaceStore.getState().viewport).toEqual({ x: 320, y: 240, scale: 1.3 });

    useWorkspaceStore.getState().setViewport({ x: 440, y: 360, scale: 0.9 });

    expect(useWorkspaceStore.getState().serialize().viewport).toEqual({ x: 440, y: 360, scale: 0.9 });
  });

  it("stores and clears viewport focus requests separately from persisted layout", () => {
    useWorkspaceStore.getState().openTerminal("m-1");
    const windowId = useWorkspaceStore.getState().windows[0].id;

    useWorkspaceStore.getState().requestViewportFocus(windowId);

    expect(useWorkspaceStore.getState().viewportFocusRequest).toMatchObject({ windowID: windowId });
    expect(useWorkspaceStore.getState().serialize()).not.toHaveProperty("viewportFocusRequest");

    useWorkspaceStore.getState().clearViewportFocusRequest();

    expect(useWorkspaceStore.getState().viewportFocusRequest).toBeNull();
  });

  it("moves shells to a target index without overlapping them", () => {
    useWorkspaceStore.getState().openTerminal("m-1");
    useWorkspaceStore.getState().openTerminal("m-2");

    const windows = useWorkspaceStore.getState().windows;
    const target = windows[1];
    useWorkspaceStore.getState().moveWindowToIndex(target.id, 0);

    const reordered = useWorkspaceStore.getState().windows;
    expect(reordered[0].id).toBe(target.id);
    expect(overlaps(reordered[0], reordered[1])).toBe(false);
    expect(reordered[0].x).toBeLessThan(reordered[1].x);
  });
});
