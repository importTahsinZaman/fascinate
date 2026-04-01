import { create } from "zustand";
import type { WorkspaceLayout, WorkspaceViewport, WorkspaceWindow } from "./api";

type GitDiffSidebarState = {
  windowID: string | null;
  selectedPath: string | null;
  selectedPreviousPath?: string;
};

type WorkspaceState = {
  windows: WorkspaceWindow[];
  windowCwds: Record<string, string>;
  viewport: WorkspaceViewport;
  viewportFocusRequest: { windowID: string; requestID: string } | null;
  gitDiffSidebar: GitDiffSidebarState;
  hydrated: boolean;
  hydrate: (layout: WorkspaceLayout) => void;
  openTerminal: (machineName: string, title?: string) => string;
  setWindowSession: (id: string, sessionId: string) => void;
  setWindowCwd: (id: string, cwd: string) => void;
  openGitDiffSidebar: (windowID: string) => void;
  closeGitDiffSidebar: () => void;
  selectGitDiffSidebarFile: (path: string, previousPath?: string) => void;
  clearGitDiffSidebarFile: () => void;
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
          sessionId: item.sessionId,
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
  openTerminal: (machineName, title) => {
    const windowID = crypto.randomUUID();
    set((state) => {
      const nextZ = state.windows.reduce((max, item) => Math.max(max, item.z), 0) + 1;
      const window: WorkspaceWindow = {
        id: windowID,
        machineName,
        title: title ?? `${machineName} shell`,
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
  setWindowSession: (id, sessionId) =>
    set((state) => ({
      windows: state.windows.map((item) =>
        item.id === id
          ? {
              ...item,
              sessionId,
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
    version: 3,
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
