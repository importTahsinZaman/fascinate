export type UnifiedDiffLineKind = "context" | "delete" | "add";
export type InlineDiffRange = {
  start: number;
  end: number;
};

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

export function computeInlineDiffRanges(rows: UnifiedDiffLineRow[]) {
  const ranges: Record<string, InlineDiffRange[]> = {};

  for (let index = 0; index < rows.length; ) {
    if (rows[index].kind !== "delete") {
      index += 1;
      continue;
    }

    const deleteStart = index;
    while (index < rows.length && rows[index].kind === "delete") {
      index += 1;
    }
    const addStart = index;
    while (index < rows.length && rows[index].kind === "add") {
      index += 1;
    }

    if (addStart === index) {
      continue;
    }

    const deletedRows = rows.slice(deleteStart, addStart);
    const addedRows = rows.slice(addStart, index);
    const pairCount = Math.min(deletedRows.length, addedRows.length);

    for (let pairIndex = 0; pairIndex < pairCount; pairIndex += 1) {
      const deletedRow = deletedRows[pairIndex];
      const addedRow = addedRows[pairIndex];
      const pairRanges = computeInlineDiffPair(deletedRow.text, addedRow.text);
      if (pairRanges.added) {
        ranges[addedRow.id] = pairRanges.added;
      }
    }
  }

  return ranges;
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

function computeInlineDiffPair(
  deletedText: string,
  addedText: string,
): {
  added?: InlineDiffRange[];
} {
  if (deletedText === addedText) {
    return {};
  }

  const maxPrefix = Math.min(deletedText.length, addedText.length);
  let prefixLength = 0;
  while (prefixLength < maxPrefix && deletedText[prefixLength] === addedText[prefixLength]) {
    prefixLength += 1;
  }

  const maxSuffix = Math.min(deletedText.length - prefixLength, addedText.length - prefixLength);
  let suffixLength = 0;
  while (
    suffixLength < maxSuffix &&
    deletedText[deletedText.length - 1 - suffixLength] === addedText[addedText.length - 1 - suffixLength]
  ) {
    suffixLength += 1;
  }

  const addedRange =
    addedText.length - suffixLength > prefixLength
      ? [{ start: prefixLength, end: addedText.length - suffixLength }]
      : undefined;

  const hasDeletedSegment = deletedText.length - suffixLength > prefixLength;
  if (hasDeletedSegment) {
    return {};
  }

  return {
    added: addedRange,
  };
}
