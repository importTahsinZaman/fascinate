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

  it("opens terminals as distinct windows", () => {
    useWorkspaceStore.getState().openTerminal("m-1");
    useWorkspaceStore.getState().openTerminal("m-1");

    const { windows } = useWorkspaceStore.getState();
    expect(windows).toHaveLength(2);
    expect(windows[0].id).not.toEqual(windows[1].id);
    expect(overlaps(windows[0], windows[1])).toBe(false);
    expect(windows[1].x).toBe(windows[0].x);
    expect(windows[1].y - windows[0].y).toBe(windows[0].height);
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
    expect(useWorkspaceStore.getState().windows[0].width).toBe(1297);
    expect(useWorkspaceStore.getState().windows[0].height).toBe(907);
    expect(useWorkspaceStore.getState().viewport).toEqual({ x: 120, y: 96, scale: 1 });
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

  it("persists canvas viewport state", () => {
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

  it("prevents windows from overlapping when moved", () => {
    useWorkspaceStore.getState().openTerminal("m-1");
    useWorkspaceStore.getState().openTerminal("m-2");

    const windows = useWorkspaceStore.getState().windows;
    const target = windows[1];
    useWorkspaceStore.getState().moveWindow(target.id, windows[0].x, windows[0].y);

    const moved = useWorkspaceStore.getState().windows[1];
    expect(overlaps(windows[0], moved)).toBe(false);
    expect(moved.x).toBe(windows[0].x);
    expect(moved.y - windows[0].y).toBe(windows[0].height);
  });
});
