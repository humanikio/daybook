package vcs

import "testing"

// git's numstat writes a moved file with the common prefix and suffix factored
// out. That notation is readable and is not a path: it reached the report
// telling a teammate to open a file that does not exist, and it reached the
// narration facts, where the model resolved it correctly and the verification
// gate then threw away a whole day's capability list for "inventing" the right
// answer.
func TestRenamedTo(t *testing.T) {
	for in, want := range map[string]string{
		// The real case, from hos-frontend@71c0ede9.
		"src/{app/(workspace)/w/[workspaceId]/automation/workflows/editor/[id]/components => components/canvas}/CanvasZoomControls.tsx": "src/components/canvas/CanvasZoomControls.tsx",

		// Whole path replaced — git omits the braces entirely.
		"old/path.ts => new/path.ts": "new/path.ts",

		// Moved into a directory: the left half is empty.
		"src/{ => nested}/f.ts": "src/nested/f.ts",
		// Moved out of one: the right half is empty.
		"src/{nested => }/f.ts": "src/f.ts",

		// Ordinary paths are untouched, including ones with braces in the name —
		// Next.js route groups and dynamic segments are full of them.
		"src/app/(workspace)/w/[workspaceId]/page.tsx": "src/app/(workspace)/w/[workspaceId]/page.tsx",
		"internal/vcs/vcs.go":                          "internal/vcs/vcs.go",
		"a.txt":                                        "a.txt",
	} {
		if got := renamedTo(in); got != want {
			t.Errorf("renamedTo(%q)\n  = %q\n want %q", in, got, want)
		}
	}
}

// A shape this does not recognise must be passed through, not guessed at. A
// wrong path is worse than an ugly one.
func TestRenamedToLeavesUnknownShapesAlone(t *testing.T) {
	weird := "src/{unclosed => brace/f.ts"
	if got := renamedTo(weird); got != weird {
		t.Errorf("mangled an unrecognised shape: %q", got)
	}
}
