package projectindex

import (
	"reflect"
	"testing"
)

func TestDiffUnchanged(t *testing.T) {
	baseline := &Index{
		Files: []File{
			{Path: "index.html", Hash: "aaa"},
			{Path: "style.css", Hash: "bbb"},
		},
	}

	current := &Index{
		Files: []File{
			{Path: "index.html", Hash: "aaa"},
			{Path: "style.css", Hash: "bbb"},
		},
	}

	got := Diff(baseline, current)

	want := DiffResult{
		Added:    []string{},
		Modified: []string{},
		Deleted:  []string{},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Diff() = %#v, want %#v", got, want)
	}

	if got.Changed() {
		t.Fatal("unchanged project reported as changed")
	}
}

func TestDiffModified(t *testing.T) {
	baseline := &Index{
		Files: []File{
			{Path: "index.html", Hash: "aaa"},
			{Path: "style.css", Hash: "bbb"},
		},
	}

	current := &Index{
		Files: []File{
			{Path: "index.html", Hash: "ccc"},
			{Path: "style.css", Hash: "bbb"},
		},
	}

	got := Diff(baseline, current)

	want := DiffResult{
		Added:    []string{},
		Modified: []string{"index.html"},
		Deleted:  []string{},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Diff() = %#v, want %#v", got, want)
	}

	if !got.Changed() {
		t.Fatal("modified project reported as unchanged")
	}
}

func TestDiffAdded(t *testing.T) {
	baseline := &Index{
		Files: []File{
			{Path: "index.html", Hash: "aaa"},
		},
	}

	current := &Index{
		Files: []File{
			{Path: "index.html", Hash: "aaa"},
			{Path: "app.js", Hash: "bbb"},
		},
	}

	got := Diff(baseline, current)

	want := DiffResult{
		Added:    []string{"app.js"},
		Modified: []string{},
		Deleted:  []string{},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Diff() = %#v, want %#v", got, want)
	}
}

func TestDiffDeleted(t *testing.T) {
	baseline := &Index{
		Files: []File{
			{Path: "index.html", Hash: "aaa"},
			{Path: "old.css", Hash: "bbb"},
		},
	}

	current := &Index{
		Files: []File{
			{Path: "index.html", Hash: "aaa"},
		},
	}

	got := Diff(baseline, current)

	want := DiffResult{
		Added:    []string{},
		Modified: []string{},
		Deleted:  []string{"old.css"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Diff() = %#v, want %#v", got, want)
	}
}

func TestDiffCombined(t *testing.T) {
	baseline := &Index{
		Files: []File{
			{Path: "index.html", Hash: "aaa"},
			{Path: "style.css", Hash: "bbb"},
			{Path: "old.js", Hash: "ccc"},
		},
	}

	current := &Index{
		Files: []File{
			{Path: "index.html", Hash: "ddd"},
			{Path: "style.css", Hash: "bbb"},
			{Path: "app.js", Hash: "eee"},
		},
	}

	got := Diff(baseline, current)

	want := DiffResult{
		Added:    []string{"app.js"},
		Modified: []string{"index.html"},
		Deleted:  []string{"old.js"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Diff() = %#v, want %#v", got, want)
	}
}

func TestDiffNilBaseline(t *testing.T) {
	current := &Index{
		Files: []File{
			{Path: "index.html", Hash: "aaa"},
			{Path: "app.js", Hash: "bbb"},
		},
	}

	got := Diff(nil, current)

	want := DiffResult{
		Added:    []string{"app.js", "index.html"},
		Modified: []string{},
		Deleted:  []string{},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Diff() = %#v, want %#v", got, want)
	}
}

func TestDiffNilCurrent(t *testing.T) {
	baseline := &Index{
		Files: []File{
			{Path: "index.html", Hash: "aaa"},
			{Path: "style.css", Hash: "bbb"},
		},
	}

	got := Diff(baseline, nil)

	want := DiffResult{
		Added:    []string{},
		Modified: []string{},
		Deleted:  []string{"index.html", "style.css"},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Diff() = %#v, want %#v", got, want)
	}
}
