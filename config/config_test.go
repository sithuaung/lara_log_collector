package config

import "testing"

func TestStacktraceDisabledByDefault(t *testing.T) {
	if DefaultConfig().IncludeStacktrace {
		t.Fatal("IncludeStacktrace = true, want false by default")
	}
}
