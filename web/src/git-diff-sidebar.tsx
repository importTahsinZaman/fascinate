import { useDeferredValue, useEffect, useMemo, useRef, useState, type UIEvent } from "react";
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
  type SplitDiffCollapsedRow,
  type SplitDiffLineRow,
} from "./git-diff";
import { useWorkspaceStore } from "./store";

const statusPollIntervalMs = 4_000;
const initialDiffPageSize = 2;
const diffPageStep = 3;
const diffPagePreloadThresholdPx = 960;

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

  useEffect(() => {
    setVisibleFileCount(initialDiffPageSize);
  }, [gitDiffSidebar.windowID, sessionId, cwd, statusQuery.data?.repo_root, deferredFiles.length]);

  useEffect(() => {
    const container = streamRef.current;
    if (!container || statusQuery.data?.state !== "ready" || visibleFileCount >= deferredFiles.length) {
      return;
    }
    if (container.clientHeight <= 0 || container.scrollHeight <= 0) {
      return;
    }
    if (container.scrollHeight <= container.clientHeight + diffPagePreloadThresholdPx / 2) {
      setVisibleFileCount((current) => Math.min(deferredFiles.length, current + diffPageStep));
    }
  }, [deferredFiles.length, statusQuery.data?.state, visibleFileCount]);

  const visibleFiles = useMemo(
    () => deferredFiles.slice(0, visibleFileCount),
    [deferredFiles, visibleFileCount],
  );

  const handleStreamScroll = (event: UIEvent<HTMLDivElement>) => {
    const { clientHeight, scrollHeight, scrollTop } = event.currentTarget;
    if (scrollTop + clientHeight < scrollHeight - diffPagePreloadThresholdPx) {
      return;
    }
    setVisibleFileCount((current) => Math.min(deferredFiles.length, current + diffPageStep));
  };

  if (!gitDiffSidebar.windowID || !activeWindow) {
    return null;
  }

  return (
    <aside className="git-diff-sidebar" aria-label="Git diff sidebar">
      <header className="git-diff-sidebar-header">
        <div className="git-diff-sidebar-header-copy">
          <div className="eyebrow">Git Diff</div>
          <h2>{activeWindow.title}</h2>
          <p title={cwd || "Waiting for shell context"}>
            {cwd || "Waiting for shell context from the active terminal session."}
          </p>
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
            ref={streamRef}
            className="git-diff-stream"
            aria-label="Git file diffs"
            onScroll={handleStreamScroll}
          >
            <div className="git-diff-stream-summary">
              <div>
                <strong>
                  {files.length} changed file{files.length === 1 ? "" : "s"}
                </strong>
                <span>
                  {statusQuery.data.branch ? `Branch ${statusQuery.data.branch}` : "Working tree"}
                </span>
              </div>
              <span className="git-diff-stream-summary-note">
                Loading diffs in scroll batches for faster rendering.
              </span>
            </div>

            {visibleFiles.map((file) => (
              <GitDiffFileCard
                key={`${file.previous_path ?? ""}:${file.path}`}
                branch={statusQuery.data.branch}
                cwd={cwd}
                file={file}
                repoRoot={statusQuery.data.repo_root ?? ""}
                sessionId={sessionId}
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
          </section>
        )}
      </div>
    </aside>
  );
}

function GitDiffFileCard({
  branch,
  cwd,
  file,
  repoRoot,
  sessionId,
}: {
  branch?: string;
  cwd: string;
  file: GitChangedFile;
  repoRoot: string;
  sessionId: string;
}) {
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
    enabled: Boolean(sessionId && cwd && repoRoot),
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
    <article className="git-diff-file-card">
      <FilePanelHeader branch={branch} diff={diffQuery.data} file={file} repoRoot={repoRoot} />

      {diffQuery.isPending ? (
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
        <SplitDiffRows rows={parsedDiff.rows} />
      ) : (
        <div className="git-diff-nonrenderable">
          <strong>No inline hunks available</strong>
          <p>This file has no textual hunks to render in the split diff view.</p>
        </div>
      )}
    </article>
  );
}

function FilePanelHeader({
  branch,
  diff,
  file,
  repoRoot,
}: {
  branch?: string;
  diff?: GitFileDiff;
  file: GitChangedFile;
  repoRoot: string;
}) {
  return (
    <div className="git-diff-file-header">
      <div className="git-diff-file-header-copy">
        <strong>{file.path}</strong>
        <span title={repoRoot}>
          {file.previous_path ? `${file.previous_path} -> ${file.path}` : repoRoot}
        </span>
      </div>
      <div className="git-diff-file-header-meta">
        <span className={`git-diff-file-kind git-diff-file-kind-${file.kind}`}>
          {formatFileKind(file.kind)}
        </span>
        {branch ? <span className="inline-chip">{branch}</span> : null}
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

function SplitDiffRows({ rows }: { rows: ParsedGitDiff["rows"] }) {
  const [expandedRows, setExpandedRows] = useState<Record<string, boolean>>({});

  useEffect(() => {
    setExpandedRows({});
  }, [rows]);

  return (
    <div className="git-diff-grid" role="table" aria-label="Split diff">
      {rows.map((row) =>
        row.type === "collapsed" ? (
          expandedRows[row.id] ? (
            <ExpandedContextRows key={row.id} rows={row.rows} />
          ) : (
            <CollapsedContextRow
              key={row.id}
              row={row}
              onExpand={() => setExpandedRows((current) => ({ ...current, [row.id]: true }))}
            />
          )
        ) : (
          <SplitDiffLine key={row.id} row={row} />
        ),
      )}
    </div>
  );
}

function ExpandedContextRows({ rows }: { rows: SplitDiffLineRow[] }) {
  return (
    <>
      {rows.map((row) => (
        <SplitDiffLine key={row.id} row={row} />
      ))}
    </>
  );
}

function CollapsedContextRow({
  row,
  onExpand,
}: {
  row: SplitDiffCollapsedRow;
  onExpand: () => void;
}) {
  return (
    <button type="button" className="git-diff-collapsed" onClick={onExpand}>
      Show {row.hiddenCount} unchanged line{row.hiddenCount === 1 ? "" : "s"}
    </button>
  );
}

function SplitDiffLine({ row }: { row: SplitDiffLineRow }) {
  const leftKind = row.left?.kind ?? "empty";
  const rightKind = row.right?.kind ?? "empty";

  return (
    <div className="git-diff-row" role="row">
      <div className={`git-diff-gutter git-diff-gutter-${leftKind}`}>{row.left?.lineNumber ?? ""}</div>
      <div className={`git-diff-code git-diff-code-left git-diff-code-${leftKind}`}>
        <code>{row.left?.text ?? ""}</code>
      </div>
      <div className={`git-diff-gutter git-diff-gutter-${rightKind}`}>{row.right?.lineNumber ?? ""}</div>
      <div className={`git-diff-code git-diff-code-right git-diff-code-${rightKind}`}>
        <code>{row.right?.text ?? ""}</code>
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

function formatFileKind(kind: string) {
  switch (kind) {
    case "added":
      return "A";
    case "deleted":
      return "D";
    case "renamed":
      return "R";
    case "copied":
      return "C";
    case "untracked":
      return "??";
    default:
      return "M";
  }
}
