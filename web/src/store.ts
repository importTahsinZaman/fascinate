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
  openTerminal: (machineName: string, title?: string) => void;
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
  moveWindow: (id: string, x: number, y: number) => void;
  setViewport: (viewport: WorkspaceViewport) => void;
  serialize: () => WorkspaceLayout;
};

const defaultViewport: WorkspaceViewport = { x: 120, y: 96, scale: 1 };
const defaultWindowSize = { width: 1297, height: 907 };
const windowMargin = 36;
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
      const windows = Array.isArray(layout.windows)
        ? layout.windows.reduce<WorkspaceWindow[]>((items, item, index) => {
            const position = findAvailablePosition(item.x, item.y, items);
            items.push({
              id: item.id,
              machineName: item.machineName,
              title: item.title,
              sessionId: item.sessionId,
              x: position.x,
              y: position.y,
              width: defaultWindowSize.width,
              height: defaultWindowSize.height,
              z: Number.isFinite(item.z) ? item.z : index + 1,
            });
            return items;
          }, [])
        : [];
      return {
        windows,
        windowCwds: {},
        viewport: normalizeViewport(layout.viewport),
        viewportFocusRequest: null,
        gitDiffSidebar: defaultGitDiffSidebarState,
        hydrated: true,
      };
    }),
  openTerminal: (machineName, title) =>
    set((state) => {
      const nextZ = state.windows.reduce((max, item) => Math.max(max, item.z), 0) + 1;
      const position = findAvailablePosition(windowMargin, windowMargin, state.windows);
      const window: WorkspaceWindow = {
        id: crypto.randomUUID(),
        machineName,
        title: title ?? `${machineName} shell`,
        x: position.x,
        y: position.y,
        width: defaultWindowSize.width,
        height: defaultWindowSize.height,
        z: nextZ,
      };
      return { windows: [...state.windows, window] };
    }),
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
        return state;
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
        windows: state.windows.filter((item) => item.id !== id),
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
  moveWindow: (id, x, y) =>
    set((state) => ({
      windows: state.windows.map((item) => {
        if (item.id !== id) {
          return item;
        }
        const position = findAvailablePosition(x, y, state.windows, id);
        return {
          ...item,
          x: position.x,
          y: position.y,
        };
      }),
    })),
  setViewport: (viewport) =>
    set({
      viewport: normalizeViewport(viewport),
    }),
  serialize: () => ({
    version: 2,
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

function findAvailablePosition(x: number, y: number, windows: WorkspaceWindow[], ignoreID?: string) {
  const originX = Math.max(windowMargin, Math.round(x));
  const originY = Math.max(windowMargin, Math.round(y));

  if (!hasOverlap(originX, originY, windows, ignoreID)) {
    return { x: originX, y: originY };
  }

  const relevantWindows = windows.filter((window) => window.id !== ignoreID);
  const xCandidates = new Set<number>([originX]);
  const yCandidates = new Set<number>([originY]);

  for (const window of relevantWindows) {
    xCandidates.add(Math.max(windowMargin, window.x - defaultWindowSize.width));
    xCandidates.add(Math.max(windowMargin, window.x + defaultWindowSize.width));
    yCandidates.add(Math.max(windowMargin, window.y - defaultWindowSize.height));
    yCandidates.add(Math.max(windowMargin, window.y + defaultWindowSize.height));
  }

  const candidates = Array.from(xCandidates).flatMap((nextX) =>
    Array.from(yCandidates).map((nextY) => ({ x: nextX, y: nextY })),
  );

  candidates.sort((left, right) => {
    const leftDistance = Math.hypot(left.x - originX, left.y - originY);
    const rightDistance = Math.hypot(right.x - originX, right.y - originY);
    if (leftDistance !== rightDistance) {
      return leftDistance - rightDistance;
    }
    if (left.y !== right.y) {
      return left.y - right.y;
    }
    return left.x - right.x;
  });

  for (const candidate of candidates) {
    if (!hasOverlap(candidate.x, candidate.y, windows, ignoreID)) {
      return candidate;
    }
  }

  return { x: originX, y: originY };
}

function hasOverlap(x: number, y: number, windows: WorkspaceWindow[], ignoreID?: string) {
  return windows.some((window) => {
    if (window.id === ignoreID) {
      return false;
    }
    return rectanglesOverlap(
      x,
      y,
      defaultWindowSize.width,
      defaultWindowSize.height,
      window.x,
      window.y,
      defaultWindowSize.width,
      defaultWindowSize.height,
    );
  });
}

function rectanglesOverlap(
  x1: number,
  y1: number,
  width1: number,
  height1: number,
  x2: number,
  y2: number,
  width2: number,
  height2: number,
) {
  return x1 < x2 + width2 && x1 + width1 > x2 && y1 < y2 + height2 && y1 + height1 > y2;
}
