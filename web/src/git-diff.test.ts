import { describe, expect, it } from "vitest";
import { parseUnifiedDiff } from "./git-diff";

describe("parseUnifiedDiff", () => {
  it("pairs changed lines and collapses long unchanged regions", () => {
    const parsed = parseUnifiedDiff(`diff --git a/web/src/app.tsx b/web/src/app.tsx
index 1111111..2222222 100644
--- a/web/src/app.tsx
+++ b/web/src/app.tsx
@@ -1,20 +1,20 @@
 line 1
 line 2
 line 3
 line 4
 line 5
 line 6
-old alpha
+new alpha
 line 8
 line 9
 line 10
 line 11
 line 12
 line 13
 line 14
 line 15
-old omega
+new omega
 line 17
 line 18
 line 19
 line 20
`);

    expect(parsed.rows[0]).toMatchObject({
      type: "line",
      left: { lineNumber: 1, text: "line 1", kind: "context" },
      right: { lineNumber: 1, text: "line 1", kind: "context" },
    });
    expect(parsed.rows[6]).toMatchObject({
      type: "line",
      left: { lineNumber: 7, text: "old alpha", kind: "delete" },
      right: { lineNumber: 7, text: "new alpha", kind: "add" },
    });

    const collapsedRow = parsed.rows.find((row) => row.type === "collapsed");
    expect(collapsedRow).toMatchObject({
      type: "collapsed",
      hiddenCount: 2,
    });
  });

  it("renders pure additions without synthetic left-hand line numbers", () => {
    const parsed = parseUnifiedDiff(`diff --git a/web/src/git-diff.ts b/web/src/git-diff.ts
new file mode 100644
--- /dev/null
+++ b/web/src/git-diff.ts
@@ -0,0 +1,2 @@
+first line
+second line
`);

    expect(parsed.rows).toHaveLength(2);
    expect(parsed.rows[0]).toMatchObject({
      type: "line",
      right: { lineNumber: 1, text: "first line", kind: "add" },
    });
    expect(parsed.rows[0]).not.toHaveProperty("left");
    expect(parsed.rows[1]).toMatchObject({
      type: "line",
      right: { lineNumber: 2, text: "second line", kind: "add" },
    });
    expect(parsed.rows[1]).not.toHaveProperty("left");
  });
});
