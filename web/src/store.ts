import { create } from "zustand";
import type { WorkspaceLayout, WorkspaceViewport, WorkspaceWindow } from "./api";

type GitDiffSidebarState = {
  windowID: string | null;
  selectedPath: string | null;
  selectedPreviousPath?: string;
};

export type RemovedMachineWindowsSnapshot = {
  machineName: string;
  orderedWindowIDs: string[];
  windows: WorkspaceWindow[];
  windowCwds: Record<string, string>;
  viewportFocusRequest: { windowID: string; requestID: string } | null;
  gitDiffSidebar: GitDiffSidebarState;
};

type WorkspaceState = {
  windows: WorkspaceWindow[];
  windowCwds: Record<string, string>;
  viewport: WorkspaceViewport;
  viewportFocusRequest: { windowID: string; requestID: string } | null;
  gitDiffSidebar: GitDiffSidebarState;
  hydrated: boolean;
  hydrate: (layout: WorkspaceLayout) => void;
  openShellWindow: (shell: { shellId: string; machineName: string; title: string }) => string;
  setWindowShell: (id: string, shellId: string, title?: string) => void;
  setWindowCwd: (id: string, cwd: string) => void;
  openGitDiffSidebar: (windowID: string) => void;
  closeGitDiffSidebar: () => void;
  selectGitDiffSidebarFile: (path: string, previousPath?: string) => void;
  clearGitDiffSidebarFile: () => void;
  removeWindowsForMachine: (machineName: string) => RemovedMachineWindowsSnapshot | null;
  closeWindowsForShell: (shellId: string) => void;
  pruneMissingShells: (shellIDs: string[]) => void;
  restoreRemovedWindows: (snapshot: RemovedMachineWindowsSnapshot) => void;
  closeWindow: (id: string) => void;
  focusWindow: (id: string) => void;
  requestViewportFocus: (id: string) => void;
  clearViewportFocusRequest: () => void;
  moveWindowToIndex: (id: string, index: number) => void;
  moveWindowEarlier: (id: string) => void;
  moveWindowLater: (id: string) => void;
  setViewport: (viewport: WorkspaceViewport) => void;
  serialize: () => WorkspaceLayout;
};

const defaultViewport: WorkspaceViewport = { x: 120, y: 96, scale: 1 };
const defaultWindowSize = { width: 796, height: 900 };
const orderedWindowGap = 0;
const defaultGitDiffSidebarState: GitDiffSidebarState = {
  windowID: null,
  selectedPath: null,
};

export const useWorkspaceStore = create<WorkspaceState>((set, get) => ({
  windows: [],
  windowCwds: {},
  viewport: defaultViewport,
  viewportFocusRequest: null,
  gitDiffSidebar: defaultGitDiffSidebarState,
  hydrated: false,
  hydrate: (layout) =>
    set((state) => {
      if (state.hydrated) {
        return state;
      }

      const incomingWindows = Array.isArray(layout.windows) ? [...layout.windows] : [];
      if ((layout.version ?? 1) < 3) {
        incomingWindows.sort((left, right) => {
          if (left.x !== right.x) {
            return left.x - right.x;
          }
          if (left.y !== right.y) {
            return left.y - right.y;
          }
          return left.z - right.z;
        });
      }

      const windows = normalizeOrderedWindows(
        incomingWindows.map((item, index) => ({
          id: item.id,
          machineName: item.machineName,
          title: item.title,
          shellId: item.shellId ?? item.sessionId,
          x: item.x,
          y: item.y,
          width: defaultWindowSize.width,
          height: defaultWindowSize.height,
          z: Number.isFinite(item.z) ? item.z : index + 1,
        })),
      );

      return {
        windows,
        windowCwds: {},
        viewport: normalizeViewport(layout.viewport),
        viewportFocusRequest: null,
        gitDiffSidebar: defaultGitDiffSidebarState,
        hydrated: true,
      };
    }),
  openShellWindow: ({ shellId, machineName, title }) => {
    const windowID = crypto.randomUUID();
    set((state) => {
      const nextZ = state.windows.reduce((max, item) => Math.max(max, item.z), 0) + 1;
      const window: WorkspaceWindow = {
        id: windowID,
        machineName,
        title: title ?? `${machineName} shell`,
        shellId,
        x: 0,
        y: 0,
        width: defaultWindowSize.width,
        height: defaultWindowSize.height,
        z: nextZ,
      };
      return {
        windows: normalizeOrderedWindows([...state.windows, window]),
      };
    });
    return windowID;
  },
  setWindowShell: (id, shellId, title) =>
    set((state) => ({
      windows: state.windows.map((item) =>
        item.id === id
          ? {
              ...item,
              shellId,
              title: title ?? item.title,
            }
          : item,
      ),
    })),
  setWindowCwd: (id, cwd) =>
    set((state) => {
      const nextCwd = cwd.trim();
      if (nextCwd === "") {
        if (!(id in state.windowCwds)) {
          return state;
        }
        const windowCwds = { ...state.windowCwds };
        delete windowCwds[id];
        return { windowCwds };
      }
      if (state.windowCwds[id] === nextCwd) {
        return state;
      }
      return {
        windowCwds: {
          ...state.windowCwds,
          [id]: nextCwd,
        },
      };
    }),
  openGitDiffSidebar: (windowID) =>
    set((state) => {
      if (state.gitDiffSidebar.windowID === windowID) {
        return { gitDiffSidebar: defaultGitDiffSidebarState };
      }
      return {
        gitDiffSidebar: {
          windowID,
          selectedPath: null,
        },
      };
    }),
  closeGitDiffSidebar: () =>
    set((state) => (state.gitDiffSidebar.windowID ? { gitDiffSidebar: defaultGitDiffSidebarState } : state)),
  selectGitDiffSidebarFile: (path, previousPath) =>
    set((state) => {
      if (state.gitDiffSidebar.selectedPath === path && state.gitDiffSidebar.selectedPreviousPath === previousPath) {
        return state;
      }
      return {
        gitDiffSidebar: {
          ...state.gitDiffSidebar,
          selectedPath: path,
          selectedPreviousPath: previousPath,
        },
      };
    }),
  clearGitDiffSidebarFile: () =>
    set((state) => {
      if (state.gitDiffSidebar.selectedPath === null && state.gitDiffSidebar.selectedPreviousPath === undefined) {
        return state;
      }
      return {
        gitDiffSidebar: {
          ...state.gitDiffSidebar,
          selectedPath: null,
          selectedPreviousPath: undefined,
        },
      };
    }),
  removeWindowsForMachine: (machineName) => {
    let snapshot: RemovedMachineWindowsSnapshot | null = null;
    set((state) => {
      const removedWindows = state.windows.filter((window) => window.machineName === machineName);
      if (removedWindows.length === 0) {
        return state;
      }

      const removedWindowIDs = new Set(removedWindows.map((window) => window.id));
      const windowCwds = { ...state.windowCwds };
      const removedWindowCwds: Record<string, string> = {};
      for (const [windowID, cwd] of Object.entries(windowCwds)) {
        if (!removedWindowIDs.has(windowID)) {
          continue;
        }
        removedWindowCwds[windowID] = cwd;
        delete windowCwds[windowID];
      }

      const removedViewportFocusRequest =
        state.viewportFocusRequest && removedWindowIDs.has(state.viewportFocusRequest.windowID)
          ? state.viewportFocusRequest
          : null;
      const removedGitDiffSidebar =
        state.gitDiffSidebar.windowID && removedWindowIDs.has(state.gitDiffSidebar.windowID)
          ? { ...state.gitDiffSidebar }
          : defaultGitDiffSidebarState;

      snapshot = {
        machineName,
        orderedWindowIDs: state.windows.map((window) => window.id),
        windows: removedWindows,
        windowCwds: removedWindowCwds,
        viewportFocusRequest: removedViewportFocusRequest,
        gitDiffSidebar: removedGitDiffSidebar,
      };

      return {
        windows: normalizeOrderedWindows(state.windows.filter((window) => !removedWindowIDs.has(window.id))),
        windowCwds,
        viewportFocusRequest: removedViewportFocusRequest ? null : state.viewportFocusRequest,
        gitDiffSidebar:
          removedGitDiffSidebar.windowID !== null ? defaultGitDiffSidebarState : state.gitDiffSidebar,
      };
    });
    return snapshot;
  },
  closeWindowsForShell: (shellId) =>
    set((state) => {
      const removedWindowIDs = new Set(
        state.windows.filter((window) => window.shellId === shellId).map((window) => window.id),
      );
      if (removedWindowIDs.size === 0) {
        return state;
      }
      const windowCwds = { ...state.windowCwds };
      for (const windowID of removedWindowIDs) {
        delete windowCwds[windowID];
      }
      return {
        windows: normalizeOrderedWindows(state.windows.filter((window) => !removedWindowIDs.has(window.id))),
        windowCwds,
        viewportFocusRequest:
          state.viewportFocusRequest && removedWindowIDs.has(state.viewportFocusRequest.windowID)
            ? null
            : state.viewportFocusRequest,
        gitDiffSidebar:
          state.gitDiffSidebar.windowID && removedWindowIDs.has(state.gitDiffSidebar.windowID)
            ? defaultGitDiffSidebarState
            : state.gitDiffSidebar,
      };
    }),
  pruneMissingShells: (shellIDs) =>
    set((state) => {
      const validShellIDs = new Set(shellIDs);
      const removedWindowIDs = new Set(
        state.windows
          .filter((window) => !window.shellId || !validShellIDs.has(window.shellId))
          .map((window) => window.id),
      );
      if (removedWindowIDs.size === 0) {
        return state;
      }
      const windowCwds = { ...state.windowCwds };
      for (const windowID of removedWindowIDs) {
        delete windowCwds[windowID];
      }
      return {
        windows: normalizeOrderedWindows(state.windows.filter((window) => !removedWindowIDs.has(window.id))),
        windowCwds,
        viewportFocusRequest:
          state.viewportFocusRequest && removedWindowIDs.has(state.viewportFocusRequest.windowID)
            ? null
            : state.viewportFocusRequest,
        gitDiffSidebar:
          state.gitDiffSidebar.windowID && removedWindowIDs.has(state.gitDiffSidebar.windowID)
            ? defaultGitDiffSidebarState
            : state.gitDiffSidebar,
      };
    }),
  restoreRemovedWindows: (snapshot) =>
    set((state) => {
      const restoredWindows = snapshot.windows.filter(
        (removedWindow) => !state.windows.some((currentWindow) => currentWindow.id === removedWindow.id),
      );
      const restoredWindowIDs = new Set(restoredWindows.map((window) => window.id));

      const orderedWindows = restoreOrderedWindows(state.windows, restoredWindows, snapshot.orderedWindowIDs);
      const windowCwds = { ...state.windowCwds };
      for (const [windowID, cwd] of Object.entries(snapshot.windowCwds)) {
        if (!restoredWindowIDs.has(windowID) || windowID in windowCwds) {
          continue;
        }
        windowCwds[windowID] = cwd;
      }

      return {
        windows: normalizeOrderedWindows(orderedWindows),
        windowCwds,
        viewportFocusRequest: state.viewportFocusRequest ?? snapshot.viewportFocusRequest,
        gitDiffSidebar: state.gitDiffSidebar.windowID ? state.gitDiffSidebar : snapshot.gitDiffSidebar,
      };
    }),
  closeWindow: (id) =>
    set((state) => {
      const windowCwds = { ...state.windowCwds };
      delete windowCwds[id];
      return {
        windows: normalizeOrderedWindows(state.windows.filter((item) => item.id !== id)),
        windowCwds,
        viewportFocusRequest:
          state.viewportFocusRequest?.windowID === id ? null : state.viewportFocusRequest,
        gitDiffSidebar:
          state.gitDiffSidebar.windowID === id ? defaultGitDiffSidebarState : state.gitDiffSidebar,
      };
    }),
  focusWindow: (id) =>
    set((state) => {
      const nextZ = state.windows.reduce((max, item) => Math.max(max, item.z), 0) + 1;
      return {
        windows: state.windows.map((item) =>
          item.id === id
            ? {
                ...item,
                z: nextZ,
              }
            : item,
        ),
      };
    }),
  requestViewportFocus: (id) =>
    set({
      viewportFocusRequest: {
        windowID: id,
        requestID: crypto.randomUUID(),
      },
    }),
  clearViewportFocusRequest: () => set({ viewportFocusRequest: null }),
  moveWindowToIndex: (id, targetIndex) =>
    set((state) => ({
      windows: normalizeOrderedWindows(reorderWindowToIndex(state.windows, id, targetIndex)),
    })),
  moveWindowEarlier: (id) =>
    set((state) => ({
      windows: normalizeOrderedWindows(moveWindowByOffset(state.windows, id, -1)),
    })),
  moveWindowLater: (id) =>
    set((state) => ({
      windows: normalizeOrderedWindows(moveWindowByOffset(state.windows, id, 1)),
    })),
  setViewport: (viewport) =>
    set({
      viewport: normalizeViewport(viewport),
    }),
  serialize: () => ({
    version: 4,
    windows: get().windows,
    viewport: get().viewport,
  }),
}));

function normalizeViewport(viewport?: Partial<WorkspaceViewport>): WorkspaceViewport {
  if (!viewport) {
    return defaultViewport;
  }
  const x = typeof viewport.x === "number" ? viewport.x : defaultViewport.x;
  const y = typeof viewport.y === "number" ? viewport.y : defaultViewport.y;
  const scale = typeof viewport.scale === "number" ? viewport.scale : defaultViewport.scale;
  return {
    x,
    y,
    scale,
  };
}

function normalizeOrderedWindows(windows: WorkspaceWindow[]) {
  return windows.map((window, index) => ({
    ...window,
    x: index * (defaultWindowSize.width + orderedWindowGap),
    y: 0,
    width: defaultWindowSize.width,
    height: defaultWindowSize.height,
  }));
}

function moveWindowByOffset(windows: WorkspaceWindow[], id: string, offset: -1 | 1) {
  const currentIndex = windows.findIndex((window) => window.id === id);
  if (currentIndex === -1) {
    return windows;
  }

  return reorderWindowToIndex(windows, id, currentIndex + offset);
}

function reorderWindowToIndex(windows: WorkspaceWindow[], id: string, targetIndex: number) {
  const currentIndex = windows.findIndex((window) => window.id === id);
  if (currentIndex === -1) {
    return windows;
  }

  const nextIndex = Math.min(windows.length - 1, Math.max(0, targetIndex));
  if (nextIndex === currentIndex) {
    return windows;
  }

  const items = [...windows];
  const [window] = items.splice(currentIndex, 1);
  items.splice(nextIndex, 0, window);
  return items;
}

function restoreOrderedWindows(
  currentWindows: WorkspaceWindow[],
  restoredWindows: WorkspaceWindow[],
  orderedWindowIDs: string[],
) {
  if (restoredWindows.length === 0) {
    return currentWindows;
  }

  const windowsByID = new Map<string, WorkspaceWindow>();
  for (const window of currentWindows) {
    windowsByID.set(window.id, window);
  }
  for (const window of restoredWindows) {
    windowsByID.set(window.id, window);
  }

  const orderedWindows: WorkspaceWindow[] = [];
  const seenWindowIDs = new Set<string>();
  for (const windowID of orderedWindowIDs) {
    const window = windowsByID.get(windowID);
    if (!window || seenWindowIDs.has(windowID)) {
      continue;
    }
    orderedWindows.push(window);
    seenWindowIDs.add(windowID);
  }
  for (const window of currentWindows) {
    if (seenWindowIDs.has(window.id)) {
      continue;
    }
    orderedWindows.push(window);
    seenWindowIDs.add(window.id);
  }
  for (const window of restoredWindows) {
    if (seenWindowIDs.has(window.id)) {
      continue;
    }
    orderedWindows.push(window);
  }

  return orderedWindows;
}
