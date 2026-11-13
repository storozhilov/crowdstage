package gstutil

import "testing"

func TestStringEscapesValues(t *testing.T) {
	if got, want := String(`a"b\c`), `"a\"b\\c"`; got != want {
		t.Errorf("String() = %s, want %s", got, want)
	}
}
