package deferclient

import (
	"testing"
)

func TestNewTrace(t *testing.T) {
	tr := NewTrace([]byte("Out"), []byte("Pkg"), 1, true)

	if string(tr.Out) != "Out" {
		t.Error("not creating Out field")
	}
	if string(tr.Pkg) != "Pkg" {
		t.Error("not creating Pkg field")
	}
	if tr.CommandId != 1 {
		t.Error("not creating CommandId field")
	}
	if tr.Ignored != true {
		t.Error("not creating Ignored field")
	}
}
