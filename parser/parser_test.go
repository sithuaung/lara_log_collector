package parser

import "testing"

func TestParserOmitsOnlyStacktraceByDefault(t *testing.T) {
	p := NewParser()
	p.ParseLine(`[2026-08-21 03:29:07] uat.ERROR: Document generation failed: cannot apply operation`)
	p.ParseLine(`while rendering template`)
	p.ParseLine(`{"exception":"[object] (RuntimeException: Document generation failed`)
	p.ParseLine(`[stacktrace]`)
	p.ParseLine(`#0 /app/Utils/Generator.php(435): generate()`)

	entry := p.Flush()
	want := "Document generation failed: cannot apply operation\nwhile rendering template\n{\"exception\":\"[object] (RuntimeException: Document generation failed"
	if entry == nil || entry.Message != want {
		t.Fatalf("message = %q, want %q", entry.Message, want)
	}
}

func TestParserKeepsLaravelContextWithoutStacktrace(t *testing.T) {
	p := NewParser()
	p.ParseLine(`[2026-08-21 03:29:07] uat.ERROR: Document generation failed! {"template_file":"template.typ","error":"failed"}`)
	p.ParseLine(`debug context continued on another line`)

	entry := p.Flush()
	want := "Document generation failed! {\"template_file\":\"template.typ\",\"error\":\"failed\"}\ndebug context continued on another line"
	if entry == nil || entry.Message != want {
		t.Fatalf("message = %q, want %q", entry.Message, want)
	}
}

func TestParserIncludesStacktraceWhenEnabled(t *testing.T) {
	p := NewParser(true)
	p.ParseLine(`[2026-08-21 03:29:07] uat.ERROR: Database unavailable {"exception":"RuntimeException`)
	p.ParseLine(`[stacktrace]`)
	p.ParseLine(`#0 /app/Service.php(10): connect()`)

	entry := p.Flush()
	want := "Database unavailable {\"exception\":\"RuntimeException\n[stacktrace]\n#0 /app/Service.php(10): connect()"
	if entry == nil || entry.Message != want {
		t.Fatalf("message = %q, want %q", entry.Message, want)
	}
}
