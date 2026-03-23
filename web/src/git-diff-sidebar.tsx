import { useEffect, useMemo, useState } from "react";
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

export function GitDiffSidebar() {
  const windows = useWorkspaceStore((state) => state.windows);
  const windowCwds = useWorkspaceStore((state) => state.windowCwds);
  const gitDiffSidebar = useWorkspaceStore((state) => state.gitDiffSidebar);
  const closeGitDiffSidebar = useWorkspaceStore((state) => state.closeGitDiffSidebar);
  const selectGitDiffSidebarFile = useWorkspaceStore((state) => state.selectGitDiffSidebarFile);
  const clearGitDiffSidebarFile = useWorkspaceStore((state) => state.clearGitDiffSidebarFile);

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
  const selectedFile = useMemo(
    () =>
      files.find(
        (file) =>
          file.path === gitDiffSidebar.selectedPath &&
          file.previous_path === gitDiffSidebar.selectedPreviousPath,
      ) ?? null,
    [files, gitDiffSidebar.selectedPath, gitDiffSidebar.selectedPreviousPath],
  );

  useEffect(() => {
    if (statusQuery.data?.state !== "ready") {
      clearGitDiffSidebarFile();
      return;
    }

    if (files.length === 0) {
      clearGitDiffSidebarFile();
      return;
    }

    const nextSelection = selectedFile ?? files[0];
    if (
      nextSelection.path !== gitDiffSidebar.selectedPath ||
      nextSelection.previous_path !== gitDiffSidebar.selectedPreviousPath
    ) {
      selectGitDiffSidebarFile(nextSelection.path, nextSelection.previous_path);
    }
  }, [
    clearGitDiffSidebarFile,
    files,
    gitDiffSidebar.selectedPath,
    gitDiffSidebar.selectedPreviousPath,
    selectGitDiffSidebarFile,
    selectedFile,
    statusQuery.data?.state,
  ]);

  const diffQuery = useQuery({
    queryKey: [
      "terminal-git-diff",
      sessionId,
      cwd,
      statusQuery.data?.repo_root,
      selectedFile?.path,
      selectedFile?.previous_path,
      selectedFile?.kind,
      selectedFile?.index_status,
      selectedFile?.worktree_status,
    ],
    queryFn: () =>
      getTerminalGitDiff(sessionId, {
        cwd,
        repo_root: statusQuery.data?.repo_root ?? "",
        path: selectedFile?.path ?? "",
        previous_path: selectedFile?.previous_path,
        kind: selectedFile?.kind,
        index_status: selectedFile?.index_status,
        worktree_status: selectedFile?.worktree_status,
      }),
    enabled: Boolean(
      gitDiffSidebar.windowID &&
        sessionId &&
        cwd &&
        statusQuery.data?.state === "ready" &&
        statusQuery.data.repo_root &&
        selectedFile,
    ),
    refetchOnWindowFocus: false,
    retry: false,
  });

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
          <button type="button" onClick={() => void statusQuery.refetch()} disabled={!sessionId || !cwd || statusQuery.isFetching}>
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
          <SidebarStateCard title="Loading repository changes" description="Inspecting the active shell working tree." />
        ) : statusQuery.error ? (
          <SidebarStateCard
            title="Unable to load git status"
            description={statusQuery.error instanceof Error ? statusQuery.error.message : "Git inspection failed for this shell."}
            actionLabel="Retry"
            onAction={() => void statusQuery.refetch()}
          />
        ) : statusQuery.data?.state === "not_repo" ? (
          <SidebarStateCard
            title="This shell is not in a git repository"
            description="Move the shell into a repository directory to inspect file changes here."
          />
        ) : statusQuery.data?.state !== "ready" ? (
          <SidebarStateCard title="Git inspection unavailable" description="Fascinate could not resolve repository state for this shell." />
        ) : (
          <div className="git-diff-layout">
            <section className="git-diff-file-list-pane" aria-label="Changed files">
              <div className="git-diff-file-list-header">
                <div>
                  <strong>{files.length} changed file{files.length === 1 ? "" : "s"}</strong>
                  <span>{statusQuery.data.branch ? `Branch ${statusQuery.data.branch}` : "Working tree"}</span>
                </div>
              </div>
              {files.length === 0 ? (
                <SidebarStateCard
                  title="Working tree is clean"
                  description="This repository has no changed files right now."
                  compact
                />
              ) : (
                <div className="git-diff-file-list">
                  {files.map((file) => {
                    const isSelected =
                      file.path === selectedFile?.path && file.previous_path === selectedFile?.previous_path;
                    return (
                      <button
                        key={`${file.previous_path ?? ""}:${file.path}`}
                        type="button"
                        className="git-diff-file-row"
                        data-selected={isSelected ? "true" : "false"}
                        onClick={() => selectGitDiffSidebarFile(file.path, file.previous_path)}
                      >
                        <span className={`git-diff-file-kind git-diff-file-kind-${file.kind}`}>{formatFileKind(file.kind)}</span>
                        <span className="git-diff-file-copy">
                          <strong>{file.path}</strong>
                          {file.previous_path ? <span>{file.previous_path} -&gt; {file.path}</span> : <span>{file.kind}</span>}
                        </span>
                      </button>
                    );
                  })}
                </div>
              )}
            </section>

            <section className="git-diff-view-pane" aria-label="Selected file diff">
              {selectedFile ? (
                <SelectedFileDiff
                  branch={statusQuery.data.branch}
                  diff={diffQuery.data}
                  diffError={diffQuery.error}
                  diffPending={diffQuery.isPending}
                  onRetry={() => void diffQuery.refetch()}
                  repoRoot={statusQuery.data.repo_root ?? ""}
                  selectedFile={selectedFile}
                />
              ) : (
                <SidebarStateCard
                  title="Select a changed file"
                  description="Choose a file from the list to inspect its split diff."
                />
              )}
            </section>
          </div>
        )}
      </div>
    </aside>
  );
}

function SelectedFileDiff({
  branch,
  diff,
  diffError,
  diffPending,
  onRetry,
  repoRoot,
  selectedFile,
}: {
  branch?: string;
  diff?: GitFileDiff;
  diffError: unknown;
  diffPending: boolean;
  onRetry: () => void;
  repoRoot: string;
  selectedFile: GitChangedFile;
}) {
  const parsedDiff = useMemo<ParsedGitDiff | null>(() => {
    if (!diff?.patch || diff.state !== "ready") {
      return null;
    }
    return parseUnifiedDiff(diff.patch);
  }, [diff]);

  if (diffPending) {
    return <SidebarStateCard title="Loading file diff" description="Fetching the selected file patch." />;
  }

  if (diffError) {
    return (
      <SidebarStateCard
        title="Unable to load this file diff"
        description={diffError instanceof Error ? diffError.message : "The selected file patch could not be loaded."}
        actionLabel="Retry"
        onAction={onRetry}
      />
    );
  }

  if (!diff) {
    return <SidebarStateCard title="Select a changed file" description="Choose a file from the list to inspect its split diff." />;
  }

  if (diff.state !== "ready") {
    return (
      <div className="git-diff-file-panel">
        <FilePanelHeader
          branch={branch}
          diff={diff}
          repoRoot={repoRoot}
          selectedFile={selectedFile}
        />
        <div className="git-diff-nonrenderable">
          <strong>Inline diff unavailable</strong>
          <p>{diff.message ?? "Fascinate cannot render this file as inline text."}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="git-diff-file-panel">
      <FilePanelHeader branch={branch} diff={diff} repoRoot={repoRoot} selectedFile={selectedFile} />
      {parsedDiff && parsedDiff.rows.length > 0 ? (
        <SplitDiffRows rows={parsedDiff.rows} />
      ) : (
        <div className="git-diff-nonrenderable">
          <strong>No inline hunks available</strong>
          <p>The selected file has no textual hunks to render in the split diff view.</p>
        </div>
      )}
    </div>
  );
}

function FilePanelHeader({
  branch,
  diff,
  repoRoot,
  selectedFile,
}: {
  branch?: string;
  diff: GitFileDiff;
  repoRoot: string;
  selectedFile: GitChangedFile;
}) {
  return (
    <div className="git-diff-file-header">
      <div className="git-diff-file-header-copy">
        <strong>{selectedFile.path}</strong>
        <span title={repoRoot}>{selectedFile.previous_path ? `${selectedFile.previous_path} -> ${selectedFile.path}` : repoRoot}</span>
      </div>
      <div className="git-diff-file-header-meta">
        {branch ? <span className="inline-chip">{branch}</span> : null}
        {typeof diff.additions === "number" ? <span className="git-diff-stat git-diff-stat-added">+{diff.additions}</span> : null}
        {typeof diff.deletions === "number" ? <span className="git-diff-stat git-diff-stat-deleted">-{diff.deletions}</span> : null}
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
  return <>{rows.map((row) => <SplitDiffLine key={row.id} row={row} />)}</>;
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
