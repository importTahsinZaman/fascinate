import {
  startTransition,
  useDeferredValue,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useQuery } from "@tanstack/react-query";
import {
  getTerminalGitDiff,
  getTerminalGitStatus,
  type GitChangedFile,
  type GitFileDiff,
} from "./api";
import {
  parseUnifiedDiff,
  type ParsedGitDiff,
  type UnifiedDiffCollapsedRow,
  type UnifiedDiffLineRow,
} from "./git-diff";
import { highlightDiffRows, type HighlightedDiffLineMap } from "./shiki-highlight";
import { useWorkspaceStore } from "./store";

const statusPollIntervalMs = 4_000;
const initialDiffPageSize = 2;
const diffPageStep = 3;
const diffPagePreloadThresholdPx = 720;

export function GitDiffSidebar() {
  const windows = useWorkspaceStore((state) => state.windows);
  const windowCwds = useWorkspaceStore((state) => state.windowCwds);
  const gitDiffSidebar = useWorkspaceStore((state) => state.gitDiffSidebar);
  const closeGitDiffSidebar = useWorkspaceStore((state) => state.closeGitDiffSidebar);

  const activeWindow = useMemo(
    () => windows.find((window) => window.id === gitDiffSidebar.windowID) ?? null,
    [gitDiffSidebar.windowID, windows],
  );
  const sessionId = activeWindow?.sessionId ?? "";
  const cwd = activeWindow ? windowCwds[activeWindow.id] ?? "" : "";

  const statusQuery = useQuery({
    queryKey: ["terminal-git-status", sessionId, cwd],
    queryFn: () => getTerminalGitStatus(sessionId, cwd),
    enabled: Boolean(gitDiffSidebar.windowID && sessionId && cwd),
    refetchInterval: gitDiffSidebar.windowID ? statusPollIntervalMs : false,
    refetchOnWindowFocus: false,
    retry: false,
  });

  const files = statusQuery.data?.state === "ready" ? statusQuery.data.files ?? [] : [];
  const deferredFiles = useDeferredValue(files);
  const [visibleFileCount, setVisibleFileCount] = useState(initialDiffPageSize);
  const streamRef = useRef<HTMLDivElement | null>(null);
  const [streamNode, setStreamNode] = useState<HTMLDivElement | null>(null);
  const loadMoreRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    setVisibleFileCount(initialDiffPageSize);
  }, [gitDiffSidebar.windowID, sessionId, cwd, statusQuery.data?.repo_root, deferredFiles.length]);

  const visibleFiles = useMemo(
    () => deferredFiles.slice(0, visibleFileCount),
    [deferredFiles, visibleFileCount],
  );

  useEffect(() => {
    if (visibleFileCount >= deferredFiles.length) {
      return;
    }
    if (typeof IntersectionObserver === "undefined") {
      startTransition(() => {
        setVisibleFileCount((current) => Math.min(deferredFiles.length, current + diffPageStep));
      });
      return;
    }
    const root = streamNode;
    const target = loadMoreRef.current;
    if (!root || !target) {
      return;
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries.some((entry) => entry.isIntersecting)) {
          return;
        }
        startTransition(() => {
          setVisibleFileCount((current) => Math.min(deferredFiles.length, current + diffPageStep));
        });
      },
      {
        root,
        rootMargin: `0px 0px ${diffPagePreloadThresholdPx}px 0px`,
      },
    );
    observer.observe(target);
    return () => observer.disconnect();
  }, [deferredFiles.length, streamNode, visibleFileCount]);

  if (!gitDiffSidebar.windowID || !activeWindow) {
    return null;
  }

  return (
    <aside className="git-diff-sidebar" aria-label="Git diff sidebar">
      <header className="git-diff-sidebar-header">
        <div className="git-diff-sidebar-header-copy">
          <h2>{activeWindow.machineName}</h2>
          <div className="git-diff-sidebar-header-meta">
            <span
              className="git-diff-sidebar-header-cwd"
              title={cwd || "Waiting for shell context from the active terminal session."}
            >
              {cwd || "Waiting for shell context from the active terminal session."}
            </span>
            {statusQuery.data?.branch ? (
              <span className="git-diff-sidebar-header-branch">{statusQuery.data.branch}</span>
            ) : null}
          </div>
        </div>
        <div className="git-diff-sidebar-header-actions">
          <button
            type="button"
            onClick={() => void statusQuery.refetch()}
            disabled={!sessionId || !cwd || statusQuery.isFetching}
          >
            {statusQuery.isFetching ? "Refreshing…" : "Refresh"}
          </button>
          <button type="button" onClick={closeGitDiffSidebar}>
            Close
          </button>
        </div>
      </header>

      <div className="git-diff-sidebar-body">
        {!sessionId ? (
          <SidebarStateCard
            title="Waiting for shell session"
            description="The shell window has not established a browser terminal session yet."
          />
        ) : !cwd ? (
          <SidebarStateCard
            title="Waiting for shell context"
            description="Fascinate is still waiting for the shell to report its current working directory."
          />
        ) : statusQuery.isPending ? (
          <SidebarStateCard
            title="Loading repository changes"
            description="Inspecting the active shell working tree."
          />
        ) : statusQuery.error ? (
          <SidebarStateCard
            title="Unable to load git status"
            description={
              statusQuery.error instanceof Error
                ? statusQuery.error.message
                : "Git inspection failed for this shell."
            }
            actionLabel="Retry"
            onAction={() => void statusQuery.refetch()}
          />
        ) : statusQuery.data?.state === "not_repo" ? (
          <SidebarStateCard
            title="This shell is not in a git repository"
            description="Move the shell into a repository directory to inspect file changes here."
          />
        ) : statusQuery.data?.state !== "ready" ? (
          <SidebarStateCard
            title="Git inspection unavailable"
            description="Fascinate could not resolve repository state for this shell."
          />
        ) : files.length === 0 ? (
          <SidebarStateCard
            title="Working tree is clean"
            description="This repository has no changed files right now."
          />
        ) : (
          <section
            ref={(node: HTMLDivElement | null) => {
              streamRef.current = node;
              setStreamNode(node);
            }}
            className="git-diff-stream"
            aria-label="Git file diffs"
          >
            <div className="git-diff-stream-summary">
              <div>
                <strong>
                  {files.length} changed file{files.length === 1 ? "" : "s"}
                </strong>
              </div>
            </div>

            {visibleFiles.map((file) => (
              <GitDiffFileCard
                key={`${file.previous_path ?? ""}:${file.path}`}
                cwd={cwd}
                file={file}
                repoRoot={statusQuery.data.repo_root ?? ""}
                sessionId={sessionId}
                scrollRoot={streamNode}
              />
            ))}

            {visibleFileCount < deferredFiles.length ? (
              <div className="git-diff-more-indicator">
                <strong>More file diffs load as you scroll.</strong>
                <span>
                  Showing {visibleFileCount} of {deferredFiles.length} changed file
                  {deferredFiles.length === 1 ? "" : "s"}.
                </span>
              </div>
            ) : null}
            {visibleFileCount < deferredFiles.length ? (
              <div ref={loadMoreRef} className="git-diff-load-more-sentinel" aria-hidden="true" />
            ) : null}
          </section>
        )}
      </div>
    </aside>
  );
}

function GitDiffFileCard({
  cwd,
  file,
  repoRoot,
  sessionId,
  scrollRoot,
}: {
  cwd: string;
  file: GitChangedFile;
  repoRoot: string;
  sessionId: string;
  scrollRoot: HTMLDivElement | null;
}) {
  const cardRef = useRef<HTMLElement | null>(null);
  const [shouldFetchDiff, setShouldFetchDiff] = useState(false);

  useEffect(() => {
    if (shouldFetchDiff) {
      return;
    }
    const node = cardRef.current;
    if (!node) {
      return;
    }
    if (typeof IntersectionObserver === "undefined") {
      setShouldFetchDiff(true);
      return;
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (!entries.some((entry) => entry.isIntersecting)) {
          return;
        }
        setShouldFetchDiff(true);
        observer.disconnect();
      },
      { root: scrollRoot, rootMargin: "480px 0px" },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [scrollRoot, shouldFetchDiff]);

  const diffQuery = useQuery({
    queryKey: [
      "terminal-git-diff",
      sessionId,
      cwd,
      repoRoot,
      file.path,
      file.previous_path,
      file.kind,
      file.index_status,
      file.worktree_status,
    ],
    queryFn: () =>
      getTerminalGitDiff(sessionId, {
        cwd,
        repo_root: repoRoot,
        path: file.path,
        previous_path: file.previous_path,
        kind: file.kind,
        index_status: file.index_status,
        worktree_status: file.worktree_status,
      }),
    enabled: Boolean(sessionId && cwd && repoRoot && shouldFetchDiff),
    refetchOnWindowFocus: false,
    retry: false,
  });

  const parsedDiff = useMemo<ParsedGitDiff | null>(() => {
    if (!diffQuery.data?.patch || diffQuery.data.state !== "ready") {
      return null;
    }
    return parseUnifiedDiff(diffQuery.data.patch);
  }, [diffQuery.data]);

  return (
    <article ref={cardRef} className="git-diff-file-card">
      <FilePanelHeader diff={diffQuery.data} file={file} />

      {!shouldFetchDiff ? (
        <SidebarStateCard
          title="Diff queued"
          description="Scroll this file into view to load its unified patch."
          compact
        />
      ) : diffQuery.isPending ? (
        <SidebarStateCard title="Loading file diff" description="Fetching this file patch." compact />
      ) : diffQuery.error ? (
        <SidebarStateCard
          title="Unable to load this file diff"
          description={
            diffQuery.error instanceof Error
              ? diffQuery.error.message
              : "The selected file patch could not be loaded."
          }
          actionLabel="Retry"
          onAction={() => void diffQuery.refetch()}
          compact
        />
      ) : !diffQuery.data ? (
        <SidebarStateCard
          title="Diff unavailable"
          description="Fascinate could not load this file patch."
          compact
        />
      ) : diffQuery.data.state !== "ready" ? (
        <div className="git-diff-nonrenderable">
          <strong>Inline diff unavailable</strong>
          <p>{diffQuery.data.message ?? "Fascinate cannot render this file as inline text."}</p>
        </div>
      ) : parsedDiff && parsedDiff.rows.length > 0 ? (
        <UnifiedDiffRows path={file.path} rows={parsedDiff.rows} />
      ) : (
        <div className="git-diff-nonrenderable">
          <strong>No inline hunks available</strong>
          <p>This file has no textual hunks to render in the unified diff view.</p>
        </div>
      )}
    </article>
  );
}

function FilePanelHeader({
  diff,
  file,
}: {
  diff?: GitFileDiff;
  file: GitChangedFile;
}) {
  const displayName = useMemo(() => file.path.split("/").at(-1) ?? file.path, [file.path]);

  return (
    <div className="git-diff-file-header">
      <div className="git-diff-file-header-copy">
        <div className="git-diff-file-header-title">
        <strong>{displayName}</strong>
        <span title={file.path}>{file.path}</span>
        </div>
      </div>
      <div className="git-diff-file-header-meta">
        {typeof diff?.additions === "number" ? (
          <span className="git-diff-stat git-diff-stat-added">+{diff.additions}</span>
        ) : null}
        {typeof diff?.deletions === "number" ? (
          <span className="git-diff-stat git-diff-stat-deleted">-{diff.deletions}</span>
        ) : null}
      </div>
    </div>
  );
}

function UnifiedDiffRows({
  path,
  rows,
}: {
  path: string;
  rows: ParsedGitDiff["rows"];
}) {
  const [expandedRows, setExpandedRows] = useState<Record<string, boolean>>({});
  const [highlightedLines, setHighlightedLines] = useState<HighlightedDiffLineMap>({});
  const lineRows = useMemo(() => collectLineRows(rows), [rows]);

  useEffect(() => {
    setExpandedRows({});
  }, [rows]);

  useEffect(() => {
    let cancelled = false;
    setHighlightedLines({});
    if (lineRows.length === 0) {
      return () => {
        cancelled = true;
      };
    }
    void highlightDiffRows(path, lineRows)
      .then((nextLines) => {
        if (!cancelled) {
          setHighlightedLines(nextLines);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setHighlightedLines({});
        }
      });
    return () => {
      cancelled = true;
    };
  }, [lineRows, path]);

  return (
    <div className="git-diff-unified" role="table" aria-label="Unified diff">
      {rows.map((row) =>
        row.type === "collapsed" ? (
          expandedRows[row.id] ? (
            <ExpandedContextRows key={row.id} highlightedLines={highlightedLines} rows={row.rows} />
          ) : (
            <CollapsedContextRow
              key={row.id}
              row={row}
              onExpand={() => setExpandedRows((current) => ({ ...current, [row.id]: true }))}
            />
          )
        ) : (
          <UnifiedDiffLine key={row.id} highlightedLine={highlightedLines[row.id]} row={row} />
        ),
      )}
    </div>
  );
}

function ExpandedContextRows({
  highlightedLines,
  rows,
}: {
  highlightedLines: HighlightedDiffLineMap;
  rows: UnifiedDiffLineRow[];
}) {
  return (
    <>
      {rows.map((row) => (
        <UnifiedDiffLine key={row.id} highlightedLine={highlightedLines[row.id]} row={row} />
      ))}
    </>
  );
}

function CollapsedContextRow({
  row,
  onExpand,
}: {
  row: UnifiedDiffCollapsedRow;
  onExpand: () => void;
}) {
  return (
    <button
      type="button"
      className="git-diff-collapsed"
      onClick={onExpand}
      aria-label={`Expand ${row.hiddenCount} unchanged line${row.hiddenCount === 1 ? "" : "s"}`}
    >
      <span>All {row.hiddenCount} line{row.hiddenCount === 1 ? "" : "s"}</span>
    </button>
  );
}

function UnifiedDiffLine({
  highlightedLine,
  row,
}: {
  highlightedLine?: HighlightedDiffLineMap[string];
  row: UnifiedDiffLineRow;
}) {
  const lineNumber = row.kind === "delete" ? row.oldLineNumber : row.newLineNumber ?? row.oldLineNumber;

  return (
    <div className={`git-diff-unified-row git-diff-unified-row-${row.kind}`} role="row">
      <div className="git-diff-unified-gutter">{lineNumber ?? ""}</div>
      <div className="git-diff-unified-code">
        <code>
          {highlightedLine && highlightedLine.length > 0
            ? highlightedLine.map((token, index) => (
                <span
                  key={`${row.id}-${index}`}
                  className="git-diff-token"
                  style={tokenStyle(token.fontStyle, token.color)}
                >
                  {token.content}
                </span>
              ))
            : row.text}
        </code>
      </div>
    </div>
  );
}

function SidebarStateCard({
  title,
  description,
  actionLabel,
  onAction,
  compact,
}: {
  title: string;
  description: string;
  actionLabel?: string;
  onAction?: () => void;
  compact?: boolean;
}) {
  return (
    <div className={`git-diff-state${compact ? " git-diff-state-compact" : ""}`}>
      <strong>{title}</strong>
      <p>{description}</p>
      {actionLabel && onAction ? (
        <button type="button" onClick={onAction}>
          {actionLabel}
        </button>
      ) : null}
    </div>
  );
}

function collectLineRows(rows: ParsedGitDiff["rows"]) {
  const lineRows: UnifiedDiffLineRow[] = [];
  for (const row of rows) {
    if (row.type === "line") {
      lineRows.push(row);
      continue;
    }
    lineRows.push(...row.rows);
  }
  return lineRows;
}

function tokenStyle(fontStyle: number | undefined, color: string | undefined) {
  return {
    color,
    fontStyle: fontStyle && (fontStyle & 1) !== 0 ? "italic" : "normal",
    fontWeight: fontStyle && (fontStyle & 2) !== 0 ? 700 : 400,
    textDecoration: fontStyle && (fontStyle & 4) !== 0 ? "underline" : "none",
  } as const;
}
