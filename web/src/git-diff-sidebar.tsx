import {
  Fragment,
  startTransition,
  useDeferredValue,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  ArrowDown,
  ArrowUp,
  ArrowsClockwise,
  ArrowsVertical,
  CaretDown,
  CaretRight,
  CheckCircle,
  Check,
  CopySimple,
  GitBranch,
  X,
} from "@phosphor-icons/react";
import { useQueries, useQuery } from "@tanstack/react-query";
import {
  getTerminalGitDiffBatch,
  getTerminalGitStatus,
  type GitChangedFile,
  type GitFileDiff,
} from "./api";
import {
  computeInlineDiffRanges,
  parseUnifiedDiff,
  type InlineDiffRange,
  type ParsedGitDiff,
  type UnifiedDiffLineRow,
} from "./git-diff";
import { highlightDiffRows, type HighlightedDiffLineMap, type HighlightedToken } from "./shiki-highlight";
import { useWorkspaceStore } from "./store";

const statusPollIntervalMs = 4_000;
const diffBatchSize = 6;
const initialDiffPageSize = diffBatchSize;
const diffPageStep = diffBatchSize;
const diffPagePreloadThresholdPx = 1_200;
const collapsedRevealStep = 5;

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
  const totalAdditions = statusQuery.data?.state === "ready" ? statusQuery.data.additions ?? 0 : null;
  const totalDeletions = statusQuery.data?.state === "ready" ? statusQuery.data.deletions ?? 0 : null;
  const deferredFiles = useDeferredValue(files);
  const [visibleFileCount, setVisibleFileCount] = useState(initialDiffPageSize);
  const [streamNode, setStreamNode] = useState<HTMLDivElement | null>(null);
  const loadMoreRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    setVisibleFileCount(initialDiffPageSize);
  }, [gitDiffSidebar.windowID, sessionId, cwd, statusQuery.data?.repo_root, deferredFiles.length]);

  const visibleFiles = useMemo(
    () => deferredFiles.slice(0, visibleFileCount),
    [deferredFiles, visibleFileCount],
  );
  const visibleBatchIndices = useMemo(
    () => Array.from({ length: Math.ceil(visibleFiles.length / diffBatchSize) }, (_, index) => index),
    [visibleFiles.length],
  );
  const diffBatchQueries = useQueries({
    queries: visibleBatchIndices.map((batchIndex) => {
      const batchFiles = deferredFiles.slice(batchIndex * diffBatchSize, (batchIndex + 1) * diffBatchSize);
      return {
        queryKey: [
          "terminal-git-diff-batch",
          sessionId,
          cwd,
          statusQuery.data?.repo_root ?? "",
          batchIndex,
          batchFiles.map((file) => gitDiffFileKey(file.path, file.previous_path)).join("|"),
        ],
        queryFn: () =>
          getTerminalGitDiffBatch(sessionId, {
            cwd,
            repo_root: statusQuery.data?.repo_root ?? "",
            files: batchFiles.map((file) => ({
              path: file.path,
              previous_path: file.previous_path,
              kind: file.kind,
              index_status: file.index_status,
              worktree_status: file.worktree_status,
            })),
          }),
        enabled: Boolean(sessionId && cwd && statusQuery.data?.repo_root && batchFiles.length > 0),
        refetchOnWindowFocus: false,
        retry: false,
        staleTime: 15_000,
      };
    }),
  });
  const diffByFileKey = useMemo(() => {
    const entries = new Map<string, GitFileDiff>();
    for (const query of diffBatchQueries) {
      for (const diff of query.data?.diffs ?? []) {
        entries.set(gitDiffFileKey(diff.path, diff.previous_path), diff);
      }
    }
    return entries;
  }, [diffBatchQueries]);
  const batchStateByFileKey = useMemo(() => {
    const entries = new Map<
      string,
      {
        isPending: boolean;
        error: unknown;
        refetch: () => Promise<unknown>;
      }
    >();
    visibleBatchIndices.forEach((batchIndex, queryIndex) => {
      const query = diffBatchQueries[queryIndex];
      const batchFiles = deferredFiles.slice(batchIndex * diffBatchSize, (batchIndex + 1) * diffBatchSize);
      for (const file of batchFiles) {
        entries.set(gitDiffFileKey(file.path, file.previous_path), {
          isPending: query.isPending,
          error: query.error,
          refetch: query.refetch,
        });
      }
    });
    return entries;
  }, [deferredFiles, diffBatchQueries, visibleBatchIndices]);

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
          <div className="git-diff-sidebar-header-primary">
            <h2>{activeWindow.machineName}</h2>
            <span
              className="git-diff-sidebar-header-cwd"
              title={cwd || "Waiting for shell context from the active terminal session."}
            >
              {cwd || "Waiting for shell context from the active terminal session."}
            </span>
          </div>
          <div className="git-diff-sidebar-header-meta">
            {statusQuery.data?.state === "ready" && statusQuery.data.branch ? (
              <span className="git-diff-sidebar-header-branch" title={statusQuery.data.branch}>
                <GitBranch size={12} weight="bold" aria-hidden="true" />
                {statusQuery.data.branch}
              </span>
            ) : null}
            {statusQuery.data?.state === "ready" ? (
              <span className="git-diff-sidebar-header-file-count">
                {formatChangedFilesLabel(files.length)}
              </span>
            ) : null}
          </div>
        </div>
        <div className="git-diff-sidebar-header-actions">
          {totalAdditions !== null && totalDeletions !== null ? (
            <div className="git-diff-sidebar-header-totals" aria-label="Overall changed lines">
              <span className="git-diff-stat git-diff-stat-added">+{totalAdditions}</span>
              <span className="git-diff-stat git-diff-stat-deleted">-{totalDeletions}</span>
            </div>
          ) : null}
          <button
            className="git-diff-sidebar-action"
            type="button"
            onClick={() => void statusQuery.refetch()}
            disabled={!sessionId || !cwd || statusQuery.isFetching}
            aria-label={statusQuery.isFetching ? "Refreshing diff" : "Refresh diff"}
            title={statusQuery.isFetching ? "Refreshing diff" : "Refresh diff"}
          >
            <ArrowsClockwise
              className={`icon-svg${statusQuery.isFetching ? " git-diff-sidebar-action-icon-spinning" : ""}`}
              weight="regular"
            />
          </button>
          <button
            className="git-diff-sidebar-action"
            type="button"
            onClick={closeGitDiffSidebar}
            aria-label="Close diff sidebar"
            title="Close diff sidebar"
          >
            <X className="icon-svg" weight="regular" />
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
          <SidebarCleanState />
        ) : (
          <section
            ref={(node: HTMLDivElement | null) => {
              setStreamNode(node);
            }}
            className="git-diff-stream"
            aria-label="Git file diffs"
          >
            {visibleFiles.map((file) => {
              const fileKey = gitDiffFileKey(file.path, file.previous_path);
              const diff = diffByFileKey.get(fileKey);
              const batchState = batchStateByFileKey.get(fileKey);
              return (
                <GitDiffFileCard
                  key={fileKey}
                  diff={diff}
                  error={diff ? null : batchState?.error}
                  file={file}
                  isPending={Boolean(!diff && batchState?.isPending)}
                  onRetry={batchState ? () => void batchState.refetch() : undefined}
                />
              );
            })}

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
  diff,
  error,
  file,
  isPending,
  onRetry,
}: {
  diff?: GitFileDiff;
  error: unknown;
  file: GitChangedFile;
  isPending: boolean;
  onRetry?: () => void;
}) {
  const [isCollapsed, setIsCollapsed] = useState(false);

  const parsedDiff = useMemo<ParsedGitDiff | null>(() => {
    if (!diff?.patch || diff.state !== "ready") {
      return null;
    }
    return parseUnifiedDiff(diff.patch);
  }, [diff]);

  return (
    <article className="git-diff-file-card" data-collapsed={isCollapsed ? "true" : "false"}>
      <div className="git-diff-file-header-shell">
        <FilePanelHeader
          collapsed={isCollapsed}
          diff={diff}
          file={file}
          onToggleCollapse={() => setIsCollapsed((current) => !current)}
        />
      </div>

      {!isCollapsed ? (
        <div className="git-diff-file-body">
          {isPending ? (
            <div className="git-diff-file-loading" role="status" aria-live="polite">
              <span>Loading file diff</span>
            </div>
          ) : error ? (
            <SidebarStateCard
              title="Unable to load this file diff"
              description={
                error instanceof Error
                  ? error.message
                  : "The selected file patch could not be loaded."
              }
              actionLabel={onRetry ? "Retry" : undefined}
              onAction={onRetry}
              compact
            />
          ) : !diff ? (
            <SidebarStateCard
              title="Diff unavailable"
              description="Fascinate could not load this file patch."
              compact
            />
          ) : diff.state !== "ready" ? (
            <div className="git-diff-nonrenderable">
              <strong>Inline diff unavailable</strong>
              <p>{diff.message ?? "Fascinate cannot render this file as inline text."}</p>
            </div>
          ) : parsedDiff && parsedDiff.rows.length > 0 ? (
            <UnifiedDiffRows path={file.path} rows={parsedDiff.rows} />
          ) : (
            <div className="git-diff-nonrenderable">
              <strong>No inline hunks available</strong>
              <p>This file has no textual hunks to render in the unified diff view.</p>
            </div>
          )}
        </div>
      ) : null}
    </article>
  );
}

function FilePanelHeader({
  collapsed,
  diff,
  file,
  onToggleCollapse,
}: {
  collapsed: boolean;
  diff?: GitFileDiff;
  file: GitChangedFile;
  onToggleCollapse: () => void;
}) {
  const displayName = useMemo(() => file.path.split("/").at(-1) ?? file.path, [file.path]);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) {
      return;
    }
    const timeoutID = globalThis.setTimeout(() => {
      setCopied(false);
    }, 1200);
    return () => globalThis.clearTimeout(timeoutID);
  }, [copied]);

  const copyFilePath = () => {
    if (typeof navigator === "undefined" || typeof navigator.clipboard?.writeText !== "function") {
      return;
    }
    void navigator.clipboard.writeText(file.path)
      .then(() => {
        setCopied(true);
      })
      .catch(() => {
        setCopied(false);
      });
  };

  return (
    <div className={`git-diff-file-header${collapsed ? " git-diff-file-header-collapsed" : ""}`}>
      <div className="git-diff-file-header-main">
        <button
          type="button"
          className="git-diff-file-icon-button"
          onClick={onToggleCollapse}
          aria-label={`${collapsed ? "Expand" : "Collapse"} file`}
        >
          {collapsed ? <CaretRight size={18} weight="bold" /> : <CaretDown size={18} weight="bold" />}
        </button>
        <div className="git-diff-file-header-copy">
          <div className="git-diff-file-header-title">
            <strong>{displayName}</strong>
            <div className="git-diff-file-header-path-row">
              <span className="git-diff-file-path" title={file.path}>
                {file.path}
              </span>
              <button
                type="button"
                className="git-diff-file-copy-button"
                onClick={copyFilePath}
                data-copied={copied ? "true" : "false"}
                aria-label={`${copied ? "Copied" : "Copy"} path ${file.path}`}
                title={`${copied ? "Copied" : "Copy"} path ${file.path}`}
              >
                {copied ? <Check size={14} weight="bold" /> : <CopySimple size={16} />}
              </button>
            </div>
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
  const [collapsedRowState, setCollapsedRowState] = useState<Record<string, { head: number; tail: number; expanded: boolean }>>(
    {},
  );
  const [highlightedLines, setHighlightedLines] = useState<HighlightedDiffLineMap>({});
  const lineRows = useMemo(() => collectLineRows(rows), [rows]);
  const inlineDiffRanges = useMemo(() => computeInlineDiffRanges(lineRows), [lineRows]);

  useEffect(() => {
    setCollapsedRowState({});
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
      {rows.map((row) => {
        if (row.type !== "collapsed") {
          return (
            <UnifiedDiffLine
              key={row.id}
              highlightedLine={highlightedLines[row.id]}
              inlineDiffRanges={inlineDiffRanges[row.id]}
              row={row}
            />
          );
        }

        const collapsedState = collapsedRowState[row.id] ?? { head: 0, tail: 0, expanded: false };
        const remainingHiddenCount = Math.max(0, row.rows.length - collapsedState.head - collapsedState.tail);
        if (collapsedState.expanded || remainingHiddenCount === 0) {
          return (
            <ExpandedContextRows
              key={row.id}
              highlightedLines={highlightedLines}
              inlineDiffRanges={inlineDiffRanges}
              rows={row.rows}
            />
          );
        }

        const visibleHeadRows = collapsedState.head > 0 ? row.rows.slice(0, collapsedState.head) : [];
        const visibleTailRows = collapsedState.tail > 0 ? row.rows.slice(row.rows.length - collapsedState.tail) : [];

        return (
          <Fragment key={row.id}>
            <ExpandedContextRows highlightedLines={highlightedLines} inlineDiffRanges={inlineDiffRanges} rows={visibleHeadRows} />
            <CollapsedContextRow
              hiddenCount={remainingHiddenCount}
              showEdgeControls={row.rows.length > collapsedRevealStep * 2 || collapsedState.head > 0 || collapsedState.tail > 0}
              topCount={Math.min(collapsedRevealStep, remainingHiddenCount)}
              bottomCount={Math.min(collapsedRevealStep, remainingHiddenCount)}
              onExpandAll={() =>
                setCollapsedRowState((current) => ({
                  ...current,
                  [row.id]: { head: row.rows.length, tail: 0, expanded: true },
                }))
              }
              onExpandHead={() =>
                setCollapsedRowState((current) => {
                  const nextState = current[row.id] ?? { head: 0, tail: 0, expanded: false };
                  return {
                    ...current,
                    [row.id]: {
                      ...nextState,
                      head: Math.min(row.rows.length - nextState.tail, nextState.head + collapsedRevealStep),
                    },
                  };
                })
              }
              onExpandTail={() =>
                setCollapsedRowState((current) => {
                  const nextState = current[row.id] ?? { head: 0, tail: 0, expanded: false };
                  return {
                    ...current,
                    [row.id]: {
                      ...nextState,
                      tail: Math.min(row.rows.length - nextState.head, nextState.tail + collapsedRevealStep),
                    },
                  };
                })
              }
            />
            <ExpandedContextRows highlightedLines={highlightedLines} inlineDiffRanges={inlineDiffRanges} rows={visibleTailRows} />
          </Fragment>
        );
      })}
    </div>
  );
}

function ExpandedContextRows({
  highlightedLines,
  inlineDiffRanges,
  rows,
}: {
  highlightedLines: HighlightedDiffLineMap;
  inlineDiffRanges: Record<string, InlineDiffRange[]>;
  rows: UnifiedDiffLineRow[];
}) {
  return (
    <>
      {rows.map((row) => (
        <UnifiedDiffLine
          key={row.id}
          highlightedLine={highlightedLines[row.id]}
          inlineDiffRanges={inlineDiffRanges[row.id]}
          row={row}
        />
      ))}
    </>
  );
}

function CollapsedContextRow({
  hiddenCount,
  showEdgeControls,
  topCount,
  bottomCount,
  onExpandAll,
  onExpandHead,
  onExpandTail,
}: {
  hiddenCount: number;
  showEdgeControls: boolean;
  topCount: number;
  bottomCount: number;
  onExpandAll: () => void;
  onExpandHead: () => void;
  onExpandTail: () => void;
}) {
  return (
    <div className="git-diff-collapsed" role="group" aria-label={`${hiddenCount} hidden unchanged lines`}>
      <div className="git-diff-collapsed-inner">
        <div className="git-diff-collapsed-copy">
          <span className="git-diff-collapsed-actions">
            {showEdgeControls ? (
              <button
                type="button"
                className="git-diff-collapsed-button"
                onClick={onExpandHead}
                aria-label={`Reveal ${topCount} earlier unchanged line${topCount === 1 ? "" : "s"}`}
                title={`Expand ${topCount} lines up`}
              >
                <ArrowUp size={12} />
                <span>
                  {topCount} line{topCount === 1 ? "" : "s"}
                </span>
              </button>
            ) : null}
            <button
              type="button"
              className="git-diff-collapsed-button"
              onClick={onExpandAll}
              aria-label={`Expand all ${hiddenCount} unchanged line${hiddenCount === 1 ? "" : "s"}`}
              title="Expand all lines"
            >
              <ArrowsVertical size={12} />
              <span>
                All {hiddenCount} line{hiddenCount === 1 ? "" : "s"}
              </span>
            </button>
            {showEdgeControls ? (
              <button
                type="button"
                className="git-diff-collapsed-button"
                onClick={onExpandTail}
                aria-label={`Reveal ${bottomCount} later unchanged line${bottomCount === 1 ? "" : "s"}`}
                title={`Expand ${bottomCount} lines down`}
              >
                <ArrowDown size={12} />
                <span>
                  {bottomCount} line{bottomCount === 1 ? "" : "s"}
                </span>
              </button>
            ) : null}
          </span>
        </div>
      </div>
    </div>
  );
}

function UnifiedDiffLine({
  highlightedLine,
  inlineDiffRanges,
  row,
}: {
  highlightedLine?: HighlightedDiffLineMap[string];
  inlineDiffRanges?: InlineDiffRange[];
  row: UnifiedDiffLineRow;
}) {
  const lineNumber = row.kind === "delete" ? row.oldLineNumber : row.newLineNumber ?? row.oldLineNumber;
  const tokens = useMemo(
    () => buildDisplayTokens(highlightedLine, row.text, inlineDiffRanges),
    [highlightedLine, inlineDiffRanges, row.text],
  );

  return (
    <div className={`git-diff-unified-row git-diff-unified-row-${row.kind}`} role="row">
      <div className="git-diff-unified-gutter">{lineNumber ?? ""}</div>
      <div className="git-diff-unified-code">
        <code>
          {tokens.map((token, index) => (
            <span
              key={`${row.id}-${index}`}
              className={`git-diff-token${token.changed ? " git-diff-token-inline git-diff-token-inline-add" : ""}`}
              style={tokenStyle(token.fontStyle, token.color)}
            >
              {token.content}
            </span>
          ))}
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

function SidebarCleanState() {
  return (
    <div className="git-diff-clean-state">
      <section className="git-diff-clean-state-card" aria-label="Working tree is clean">
        <div className="git-diff-clean-state-badge" aria-hidden="true">
          <CheckCircle size={18} weight="fill" />
        </div>
        <div className="git-diff-clean-state-copy">
          <span className="git-diff-clean-state-label">Repo status</span>
          <strong>Working tree is clean</strong>
          <p>This repository has no changed files right now.</p>
          <span className="git-diff-clean-state-note">Changes from this shell appear here automatically.</span>
        </div>
      </section>
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

function formatChangedFilesLabel(count: number) {
  return `${count} file${count === 1 ? "" : "s"} changed`;
}

function gitDiffFileKey(path: string, previousPath?: string) {
  return `${previousPath ?? ""}:${path}`;
}

function tokenStyle(fontStyle: number | undefined, color: string | undefined) {
  return {
    color,
    fontStyle: fontStyle && (fontStyle & 1) !== 0 ? "italic" : "normal",
    fontWeight: fontStyle && (fontStyle & 2) !== 0 ? 700 : 400,
    textDecoration: fontStyle && (fontStyle & 4) !== 0 ? "underline" : "none",
  } as const;
}

function buildDisplayTokens(
  highlightedLine: HighlightedToken[] | undefined,
  text: string,
  ranges: InlineDiffRange[] | undefined,
) {
  const tokens = highlightedLine && highlightedLine.length > 0 ? highlightedLine : [{ content: text }];
  if (!ranges || ranges.length === 0) {
    return tokens.map((token) => ({ ...token, changed: false }));
  }

  const normalizedRanges = ranges
    .filter((range) => range.end > range.start)
    .sort((left, right) => left.start - right.start);
  if (normalizedRanges.length === 0) {
    return tokens.map((token) => ({ ...token, changed: false }));
  }

  const rendered: Array<HighlightedToken & { changed: boolean }> = [];
  let offset = 0;

  for (const token of tokens) {
    const tokenStart = offset;
    const tokenEnd = tokenStart + token.content.length;
    offset = tokenEnd;

    if (token.content.length === 0) {
      rendered.push({ ...token, changed: false });
      continue;
    }

    let cursor = tokenStart;
    for (const range of normalizedRanges) {
      if (range.end <= tokenStart || range.start >= tokenEnd) {
        continue;
      }
      const overlapStart = Math.max(range.start, tokenStart);
      const overlapEnd = Math.min(range.end, tokenEnd);

      if (overlapStart > cursor) {
        rendered.push({
          ...token,
          content: token.content.slice(cursor - tokenStart, overlapStart - tokenStart),
          changed: false,
        });
      }

      if (overlapEnd > overlapStart) {
        rendered.push({
          ...token,
          content: token.content.slice(overlapStart - tokenStart, overlapEnd - tokenStart),
          changed: true,
        });
      }
      cursor = overlapEnd;
    }

    if (cursor < tokenEnd) {
      rendered.push({
        ...token,
        content: token.content.slice(cursor - tokenStart),
        changed: false,
      });
    }
  }

  return rendered.filter((token) => token.content.length > 0);
}
