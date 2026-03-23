export type UnifiedDiffLineKind = "context" | "delete" | "add";

export type UnifiedDiffLineRow = {
  type: "line";
  id: string;
  kind: UnifiedDiffLineKind;
  oldLineNumber?: number;
  newLineNumber?: number;
  text: string;
};

export type UnifiedDiffCollapsedRow = {
  type: "collapsed";
  id: string;
  hiddenCount: number;
  rows: UnifiedDiffLineRow[];
};

export type UnifiedDiffRow = UnifiedDiffLineRow | UnifiedDiffCollapsedRow;

export type ParsedGitDiff = {
  rows: UnifiedDiffRow[];
};

type HunkHeader = {
  leftStart: number;
  rightStart: number;
};

const visibleContextLines = 3;

export function parseUnifiedDiff(patch: string): ParsedGitDiff {
  const lines = patch.split("\n").map((line) => line.replace(/\r$/, ""));
  const rows: UnifiedDiffLineRow[] = [];
  let rowID = 0;

  for (let index = 0; index < lines.length; ) {
    const hunk = parseHunkHeader(lines[index]);
    if (!hunk) {
      index += 1;
      continue;
    }

    index += 1;
    let leftLine = hunk.leftStart;
    let rightLine = hunk.rightStart;

    while (index < lines.length && !parseHunkHeader(lines[index])) {
      const line = lines[index];

      if (line.startsWith("\\ No newline")) {
        index += 1;
        continue;
      }

      if (line.startsWith(" ")) {
        rows.push({
          type: "line",
          id: `line-${rowID++}`,
          kind: "context",
          oldLineNumber: leftLine++,
          newLineNumber: rightLine++,
          text: line.slice(1),
        });
        index += 1;
        continue;
      }

      if (line.startsWith("-")) {
        while (index < lines.length && lines[index].startsWith("-")) {
          rows.push({
            type: "line",
            id: `line-${rowID++}`,
            kind: "delete",
            oldLineNumber: leftLine++,
            text: lines[index].slice(1),
          });
          index += 1;
        }
        while (index < lines.length && lines[index].startsWith("+")) {
          rows.push({
            type: "line",
            id: `line-${rowID++}`,
            kind: "add",
            newLineNumber: rightLine++,
            text: lines[index].slice(1),
          });
          index += 1;
        }
        continue;
      }

      if (line.startsWith("+")) {
        while (index < lines.length && lines[index].startsWith("+")) {
          rows.push({
            type: "line",
            id: `line-${rowID++}`,
            kind: "add",
            newLineNumber: rightLine++,
            text: lines[index].slice(1),
          });
          index += 1;
        }
        continue;
      }

      index += 1;
    }
  }

  return {
    rows: collapseContextRows(rows),
  };
}

function parseHunkHeader(line: string): HunkHeader | null {
  const match = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(line);
  if (!match) {
    return null;
  }
  return {
    leftStart: Number.parseInt(match[1], 10),
    rightStart: Number.parseInt(match[2], 10),
  };
}

function collapseContextRows(rows: UnifiedDiffLineRow[]): UnifiedDiffRow[] {
  const collapsed: UnifiedDiffRow[] = [];
  const contextBuffer: UnifiedDiffLineRow[] = [];
  let collapsedID = 0;

  const flushContextBuffer = () => {
    if (contextBuffer.length <= visibleContextLines * 2) {
      collapsed.push(...contextBuffer);
      contextBuffer.length = 0;
      return;
    }

    collapsed.push(...contextBuffer.slice(0, visibleContextLines));
    collapsed.push({
      type: "collapsed",
      id: `collapsed-${collapsedID++}`,
      hiddenCount: contextBuffer.length - visibleContextLines * 2,
      rows: contextBuffer.slice(visibleContextLines, -visibleContextLines),
    });
    collapsed.push(...contextBuffer.slice(-visibleContextLines));
    contextBuffer.length = 0;
  };

  for (const row of rows) {
    if (isContextRow(row)) {
      contextBuffer.push(row);
      continue;
    }
    flushContextBuffer();
    collapsed.push(row);
  }

  flushContextBuffer();
  return collapsed;
}

function isContextRow(row: UnifiedDiffLineRow) {
  return row.kind === "context";
}
