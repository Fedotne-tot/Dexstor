package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateHTMLPassesValidHTML(t *testing.T) {
	root := t.TempDir()

	jar := filepath.Join(root, ".tools", "vnu", "vnu.jar")
	if err := os.MkdirAll(filepath.Dir(jar), 0755); err != nil {
		t.Fatal(err)
	}

	realJar := filepath.Join(
		os.Getenv("HOME"),
		"DEXSTER",
		"destor-merge",
		".tools",
		"vnu",
		"vnu.jar",
	)

	data, err := os.ReadFile(realJar)
	if err != nil {
		t.Skipf("local vnu.jar unavailable: %v", err)
	}

	if err := os.WriteFile(jar, data, 0644); err != nil {
		t.Fatal(err)
	}

	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>DEXSTER</title>
</head>
<body>
  <main>
    <h1>Hello DEXSTER</h1>
  </main>
</body>
</html>
`

	if err := os.WriteFile(
		filepath.Join(root, "index.html"),
		[]byte(html),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	router := New(root)

	result := router.ValidateHTML("index.html")

	if !result.OK {
		t.Fatalf(
			"expected valid HTML to pass: status=%s exit=%d error=%q output=%q",
			result.Status,
			result.Exit,
			result.Error,
			result.Output,
		)
	}

	if result.Exit != 0 {
		t.Fatalf("expected exit 0, got %d", result.Exit)
	}

	if result.Status != "PASS" {
		t.Fatalf("expected PASS, got %q", result.Status)
	}
}

func TestValidateHTMLRejectsInvalidHTML(t *testing.T) {
	root := t.TempDir()

	jar := filepath.Join(root, ".tools", "vnu", "vnu.jar")
	if err := os.MkdirAll(filepath.Dir(jar), 0755); err != nil {
		t.Fatal(err)
	}

	realJar := filepath.Join(
		os.Getenv("HOME"),
		"DEXSTER",
		"destor-merge",
		".tools",
		"vnu",
		"vnu.jar",
	)

	data, err := os.ReadFile(realJar)
	if err != nil {
		t.Skipf("local vnu.jar unavailable: %v", err)
	}

	if err := os.WriteFile(jar, data, 0644); err != nil {
		t.Fatal(err)
	}

	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Broken</title>
</head>
<body>
  <div class="one" class="two">Broken</div>
</body>
</html>
`

	if err := os.WriteFile(
		filepath.Join(root, "index.html"),
		[]byte(html),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	router := New(root)

	result := router.ValidateHTML("index.html")

	if result.OK {
		t.Fatal("expected invalid HTML to fail")
	}

	if result.Exit == 0 {
		t.Fatal("expected non-zero exit")
	}

	if result.Status != "FAIL" {
		t.Fatalf("expected FAIL, got %q", result.Status)
	}

	if !strings.Contains(result.Output, "Duplicate attribute") {
		t.Fatalf(
			"expected VNU duplicate attribute error, got %q",
			result.Output,
		)
	}
}

func TestValidateHTMLRejectsNonHTML(t *testing.T) {
	root := t.TempDir()

	if err := os.WriteFile(
		filepath.Join(root, "test.pl"),
		[]byte(`print "ok\n";`),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	router := New(root)

	result := router.ValidateHTML("test.pl")

	if result.OK {
		t.Fatal("expected non-HTML file to be rejected")
	}

	if result.Exit == 0 {
		t.Fatal("expected non-zero exit")
	}

	if !strings.Contains(
		result.Error,
		"HTML validation requires a .html file",
	) {
		t.Fatalf("unexpected error: %q", result.Error)
	}
}

func TestValidateHTMLRejectsPathEscape(t *testing.T) {
	root := t.TempDir()
	router := New(root)

	result := router.ValidateHTML("../outside.html")

	if result.OK {
		t.Fatal("expected path escape to fail")
	}

	if result.Exit == 0 {
		t.Fatal("expected non-zero exit")
	}

	if !strings.Contains(
		result.Error,
		"path escapes project root",
	) {
		t.Fatalf("unexpected error: %q", result.Error)
	}
}
