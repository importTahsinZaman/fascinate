export type SplitDiffCellKind = "context" | "delete" | "add";

export type SplitDiffCell = {
  lineNumber: number;
  text: string;
  kind: SplitDiffCellKind;
};

export type SplitDiffLineRow = {
  type: "line";
  id: string;
  left?: SplitDiffCell;
  right?: SplitDiffCell;
};

export type SplitDiffCollapsedRow = {
  type: "collapsed";
  id: string;
  hiddenCount: number;
  rows: SplitDiffLineRow[];
};

export type SplitDiffRow = SplitDiffLineRow | SplitDiffCollapsedRow;

export type ParsedGitDiff = {
  rows: SplitDiffRow[];
};

type HunkHeader = {
  leftStart: number;
  rightStart: number;
};

const visibleContextLines = 3;

export function parseUnifiedDiff(patch: string): ParsedGitDiff {
  const lines = patch.split("\n").map((line) => line.replace(/\r$/, ""));
  const rows: SplitDiffLineRow[] = [];
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
          left: { lineNumber: leftLine++, text: line.slice(1), kind: "context" },
          right: { lineNumber: rightLine++, text: line.slice(1), kind: "context" },
        });
        index += 1;
        continue;
      }

      if (line.startsWith("-")) {
        const deleted: SplitDiffCell[] = [];
        while (index < lines.length && lines[index].startsWith("-")) {
          deleted.push({
            lineNumber: leftLine++,
            text: lines[index].slice(1),
            kind: "delete",
          });
          index += 1;
        }

        const added: SplitDiffCell[] = [];
        while (index < lines.length && lines[index].startsWith("+")) {
          added.push({
            lineNumber: rightLine++,
            text: lines[index].slice(1),
            kind: "add",
          });
          index += 1;
        }

        const pairCount = Math.max(deleted.length, added.length);
        for (let pairIndex = 0; pairIndex < pairCount; pairIndex += 1) {
          rows.push({
            type: "line",
            id: `line-${rowID++}`,
            left: deleted[pairIndex],
            right: added[pairIndex],
          });
        }
        continue;
      }

      if (line.startsWith("+")) {
        const added: SplitDiffCell[] = [];
        while (index < lines.length && lines[index].startsWith("+")) {
          added.push({
            lineNumber: rightLine++,
            text: lines[index].slice(1),
            kind: "add",
          });
          index += 1;
        }

        for (const entry of added) {
          rows.push({
            type: "line",
            id: `line-${rowID++}`,
            right: entry,
          });
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

function collapseContextRows(rows: SplitDiffLineRow[]): SplitDiffRow[] {
  const collapsed: SplitDiffRow[] = [];
  const contextBuffer: SplitDiffLineRow[] = [];
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

function isContextRow(row: SplitDiffLineRow) {
  return row.left?.kind === "context" && row.right?.kind === "context";
}
